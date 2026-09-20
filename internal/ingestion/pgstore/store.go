package pgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/fetcher"
	fetchstore "github.com/traweezy/relantern/internal/fetcher/pgstore"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

type Store struct {
	pool           *pgxpool.Pool
	jobs           *jobqueue.Inserter
	fetches        fetchstore.Store
	objects        entryObjectStore
	clock          clock.Clock
	researchMu     sync.Mutex
	researchCursor string
}

type entryObjectStore interface {
	storage.RawStore
	Delete(context.Context, string) error
}

func NewWithEntries(pool *pgxpool.Pool, jobs *jobqueue.Inserter, objects entryObjectStore, configuredClock clock.Clock) (*Store, error) {
	if objects == nil || configuredClock == nil {
		return nil, errors.New("source entry store requires object storage and clock")
	}
	store, err := New(pool, jobs)
	if err != nil {
		return nil, err
	}
	store.objects = objects
	store.clock = configuredClock
	return store, nil
}

func New(pool *pgxpool.Pool, jobs *jobqueue.Inserter) (*Store, error) {
	if pool == nil || jobs == nil {
		return nil, errors.New("source ingestion requires database and transactional job inserter")
	}
	return &Store{pool: pool, jobs: jobs}, nil
}

func (store *Store) ScheduleDue(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, errors.New("source poll batch size must be between 1 and 500")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin source scheduling: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select endpoint.registry_id,
			extract(epoch from coalesce(runtime.poll_interval, endpoint.poll_interval))::bigint
		from app.source_endpoints endpoint
		join app.sources source on source.id = endpoint.source_id
		left join app.source_runtime_overrides runtime on runtime.source_id = source.id
		where source.enabled and source.validation_state = 'active'
			and coalesce(runtime.polling_enabled, true)
			and endpoint.health_state not in ('paused', 'failed')
			and (endpoint.next_poll_at is null or endpoint.next_poll_at <= $1)
		order by case endpoint.priority when 'critical' then 0 when 'high' then 1 else 2 end,
			endpoint.next_poll_at nulls first, endpoint.registry_id
		limit $2
		for update of endpoint skip locked`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("select due source endpoints: %w", err)
	}
	type dueEndpoint struct {
		registryID      string
		intervalSeconds int64
	}
	due := make([]dueEndpoint, 0, limit)
	for rows.Next() {
		var selected dueEndpoint
		if err := rows.Scan(&selected.registryID, &selected.intervalSeconds); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due source endpoint: %w", err)
		}
		due = append(due, selected)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due source endpoints: %w", err)
	}
	rows.Close()
	inserted := 0
	// A parser defect leaves durable raw evidence. Operator resume re-arms the
	// endpoint, after which this query replays the failed raw document once.
	replayRows, err := tx.Query(ctx, `
		select raw.id::text, raw.source_registry_id
		from app.raw_documents raw
		join app.source_endpoints endpoint on endpoint.registry_id = raw.source_registry_id
		join app.sources source on source.id = raw.source_id
		left join app.source_runtime_overrides runtime on runtime.source_id = source.id
		where raw.ingestion_error_code is not null
			and raw.object_key is not null
			and source.enabled and source.validation_state = 'active'
			and coalesce(runtime.polling_enabled, true)
			and endpoint.health_state not in ('paused', 'failed')
			and not exists (
				select 1 from app.advisory_collection_observations observation
				where observation.parent_raw_document_id = raw.id
			)
			and not exists (
				select 1 from river.river_job job
				where job.kind = $2 and job.args ->> 'rawDocumentId' = raw.id::text
					and job.state in ('available', 'pending', 'retryable', 'running', 'scheduled')
			)
		order by raw.ingestion_failed_at, raw.id
		limit $1
		for update of raw skip locked`, limit, jobqueue.ParseRawDocumentKind)
	if err != nil {
		return 0, fmt.Errorf("select failed source entry splits: %w", err)
	}
	type replayDocument struct{ id, registryID string }
	replays := make([]replayDocument, 0)
	for replayRows.Next() {
		var replay replayDocument
		if err := replayRows.Scan(&replay.id, &replay.registryID); err != nil {
			replayRows.Close()
			return 0, err
		}
		replays = append(replays, replay)
	}
	if err := replayRows.Err(); err != nil {
		replayRows.Close()
		return 0, err
	}
	replayRows.Close()
	for _, replay := range replays {
		_, newJob, err := store.jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
			RawDocumentID: replay.id, RegistryID: replay.registryID,
		})
		if err != nil {
			return 0, err
		}
		if newJob {
			inserted++
		}
	}
	for _, endpoint := range due {
		if endpoint.intervalSeconds < 300 || endpoint.intervalSeconds > 7*24*3600 {
			return 0, fmt.Errorf("source endpoint %s has an invalid poll interval", endpoint.registryID)
		}
		_, newJob, err := store.jobs.EnqueuePollSourceEndpoint(ctx, tx, endpoint.registryID)
		if err != nil {
			return 0, err
		}
		if newJob {
			inserted++
		}
		_, err = tx.Exec(ctx, `
			update app.source_endpoints
			set next_poll_at = $2, updated_at = $3
			where registry_id = $1`,
			endpoint.registryID,
			nextPollAt(now.UTC(), time.Duration(endpoint.intervalSeconds)*time.Second, endpoint.registryID),
			now.UTC(),
		)
		if err != nil {
			return 0, fmt.Errorf("advance source endpoint %s: %w", endpoint.registryID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit source poll jobs: %w", err)
	}
	return inserted, nil
}

func (store *Store) RecordIngestionFailure(ctx context.Context, document ingestion.RawDocument, registryID string, code parsing.ErrorCode) error {
	if document.ID == "" || registryID == "" || code == "" {
		return errors.New("source entry failure requires raw document and error code")
	}
	now := store.clock.Now().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin source ingestion failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `
		update app.raw_documents
		set ingestion_error_code = $3, ingestion_failed_at = $4,
			source_connector = coalesce(source_connector, $5),
			source_content_type = coalesce(source_content_type, $6)
		where id = $1::uuid and source_registry_id = $2`,
		document.ID, registryID, string(code), now, string(document.Connector), document.ContentType)
	if err != nil {
		return fmt.Errorf("mark failed source ingestion: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("failed source entry raw document was not found")
	}
	_, err = tx.Exec(ctx, `
		insert into app.source_parse_attempts (
			raw_document_id, outcome, parser_name, parser_version,
			attempted_at, completed_at, duration_ms, error_code, warnings
		) values ($1::uuid, 'failed', 'source-ingestion-replay', $2, $3, $3, 0, $4, '[]'::jsonb)`,
		document.ID, parsing.ParserVersion, now, string(code))
	if err != nil {
		return fmt.Errorf("record failed source ingestion attempt: %w", err)
	}
	_, err = tx.Exec(ctx, `
		update app.source_endpoints
		set health_state = case when health_state = 'paused' then 'paused' else 'failed' end,
			next_poll_at = null, updated_at = $2
		where registry_id = $1`, registryID, now)
	if err != nil {
		return fmt.Errorf("stop source endpoint after ingestion failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit source ingestion failure: %w", err)
	}
	return nil
}

func (store *Store) ClearIngestionFailure(ctx context.Context, document ingestion.RawDocument, registryID string) error {
	if document.ID == "" || registryID == "" {
		return errors.New("source entry success requires raw document and registry ID")
	}
	result, err := store.pool.Exec(ctx, `
		update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null,
			source_connector = coalesce(source_connector, $3),
			source_content_type = coalesce(source_content_type, $4)
		where id = $1::uuid and source_registry_id = $2`,
		document.ID, registryID, string(document.Connector), document.ContentType)
	if err != nil {
		return fmt.Errorf("clear source entry failure: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("source entry raw document was not found")
	}
	return nil
}

// CompleteParsedRevision commits the parser replay acknowledgement together
// with downstream jobs. A failed enqueue leaves the raw marker available for
// reconciliation, even though normalization and deduplication already commit
// their own idempotent records.
func (store *Store) CompleteParsedRevision(
	ctx context.Context,
	document ingestion.RawDocument,
	registryID string,
	decision dedupe.ProcessResult,
	revisionID string,
	capabilities ingestion.HandoffCapabilities,
) error {
	if store.clock == nil {
		return errors.New("source revision handoff requires a clock")
	}
	for _, identifier := range []string{document.ID, decision.ItemID, decision.ClusterID, revisionID} {
		if _, err := uuid.Parse(identifier); err != nil {
			return errors.New("source revision handoff requires UUID identities")
		}
	}
	if document.SourceID == "" || registryID == "" {
		return errors.New("source revision handoff requires source and registry identities")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin source revision handoff: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentRevisionID string
	var lifecycleState string
	var sourceRole string
	var sourceTier string
	err = tx.QueryRow(ctx, `
		select item.current_revision_id::text, item.lifecycle_state,
			item_source.source_role, item_source.source_tier
		from app.raw_documents raw
		join app.content_revisions revision on revision.raw_document_id = raw.id
		join app.dedupe_decisions dedupe on dedupe.revision_id = revision.id
		join app.items item on item.id = dedupe.item_id
		join app.item_sources item_source on item_source.revision_id = revision.id
			and item_source.item_id = item.id
		where raw.id = $1::uuid and raw.source_id = $2
			and raw.source_registry_id = $3 and revision.id = $4::uuid
			and dedupe.item_id = $5::uuid and dedupe.cluster_id = $6::uuid
		for update of raw, item`,
		document.ID, document.SourceID, registryID, revisionID,
		decision.ItemID, decision.ClusterID,
	).Scan(&currentRevisionID, &lifecycleState, &sourceRole, &sourceTier)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("source revision handoff does not match proven raw and dedupe identities")
	}
	if err != nil {
		return fmt.Errorf("lock source revision handoff: %w", err)
	}
	if currentRevisionID == revisionID && sourceRole == "primary" {
		if capabilities.EmbeddingModelID != "" {
			var indexReady bool
			if err := tx.QueryRow(ctx, `
				select exists (
					select 1 from app.search_documents search
					join app.embeddings embedding on embedding.id = search.embedding_id
					where search.item_id = $1::uuid and search.revision_id = $2::uuid
						and embedding.entity_type = 'item' and embedding.entity_id = $1::uuid
						and embedding.model_id = $3
				)`, decision.ItemID, revisionID, capabilities.EmbeddingModelID).Scan(&indexReady); err != nil {
				return fmt.Errorf("inspect source revision index state: %w", err)
			}
			if !indexReady {
				if _, _, err := store.jobs.EnqueueReembedEntity(ctx, tx, jobqueue.ReembedEntityArgs{
					EntityType: "item", EntityID: decision.ItemID,
					RevisionID: revisionID, ModelID: capabilities.EmbeddingModelID,
				}); err != nil {
					return err
				}
			}
		}
		if capabilities.ExtractEnabled && lifecycleState == "clustered" &&
			(sourceTier == "T0" || sourceTier == "T1") {
			if _, _, err := store.jobs.EnqueueExtractItem(ctx, tx, jobqueue.ExtractItemArgs{
				ItemID: decision.ItemID, RevisionID: revisionID,
			}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				update app.items set lifecycle_state = 'awaiting_ai', updated_at = $3
				where id = $1::uuid and current_revision_id = $2::uuid
					and lifecycle_state = 'clustered'`,
				decision.ItemID, revisionID, store.clock.Now().UTC()); err != nil {
				return fmt.Errorf("mark source item awaiting extraction: %w", err)
			}
		}
	}
	managedAdvisoryParent := false
	if document.SourceEntryID != "" && (sourceTier == "T0" || sourceTier == "T1") {
		var officialAdvisory bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from app.raw_documents child
				join app.raw_documents parent on parent.id = child.parent_raw_document_id
				join app.source_endpoints endpoint
					on endpoint.registry_id = child.source_registry_id
					and endpoint.registry_id = parent.source_registry_id
				where child.id = $1::uuid and child.source_entry_id = $2::uuid
					and child.source_id = parent.source_id
					and endpoint.connector = 'github_advisories'
			)`, document.ID, document.SourceEntryID).Scan(&officialAdvisory); err != nil {
			return fmt.Errorf("verify source advisory parent: %w", err)
		}
		if officialAdvisory {
			if err := tx.QueryRow(ctx, `select exists (
				select 1 from app.advisory_collection_observations observation
				join app.raw_documents child
					on child.parent_raw_document_id = observation.parent_raw_document_id
				where child.id = $1::uuid
			)`, document.ID).Scan(&managedAdvisoryParent); err != nil {
				return fmt.Errorf("check managed advisory parent: %w", err)
			}
			if !managedAdvisoryParent {
				if _, _, err := store.jobs.EnqueueAssessCriticalAdvisory(ctx, tx,
					jobqueue.AssessCriticalAdvisoryArgs{
						RawDocumentID: document.ID, RevisionID: revisionID,
					}); err != nil {
					return err
				}
			}
		}
	}
	result, err := tx.Exec(ctx, `
		update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null,
			source_connector = coalesce(source_connector, $3),
			source_content_type = coalesce(source_content_type, $4)
		where id = $1::uuid and source_registry_id = $2`,
		document.ID, registryID, string(document.Connector), document.ContentType)
	if err != nil {
		return fmt.Errorf("acknowledge parsed source revision: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("parsed source revision lost its raw provenance")
	}
	if managedAdvisoryParent {
		if _, err := store.enqueueFirstReadyAdvisoryEvent(ctx, tx, document.SourceID, document.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit source revision handoff: %w", err)
	}
	return nil
}

func nextPollAt(now time.Time, interval time.Duration, registryID string) time.Time {
	// Stable per-endpoint jitter spreads provider requests without random state.
	window := interval / 10
	if window > 30*time.Second {
		window = 30 * time.Second
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(registryID))
	jitter := time.Duration(hash.Sum64() % uint64(window+1))
	return now.Add(interval + jitter)
}

func (store *Store) LoadEndpoint(ctx context.Context, registryID string) (*ingestion.Endpoint, error) {
	var endpoint ingestion.Endpoint
	var cursor, etag, lastModified *string
	var state []byte
	err := store.pool.QueryRow(ctx, `
		select endpoint.registry_id, endpoint.source_id, endpoint.connector, endpoint.url,
			case
				when endpoint.connector = 'github_advisories'
					and endpoint.config->>'event' = 'security_advisories'
					and endpoint.config->>'contentPolicy' = 'link-and-excerpt'
					then 'link-and-excerpt'
				else source.content_policy
			end, endpoint.expected_content_types,
			endpoint.max_response_bytes, checkpoint.cursor, checkpoint.etag,
			checkpoint.last_modified, coalesce(checkpoint.provider_state, '{}'::jsonb)
		from app.source_endpoints endpoint
		join app.sources source on source.id = endpoint.source_id
		left join app.source_runtime_overrides runtime on runtime.source_id = source.id
		left join app.source_checkpoints checkpoint on checkpoint.endpoint_id = endpoint.id
		where endpoint.registry_id = $1 and source.enabled
			and source.validation_state = 'active'
			and coalesce(runtime.polling_enabled, true)
			and endpoint.health_state not in ('paused', 'failed')`, registryID).Scan(
		&endpoint.RegistryID, &endpoint.SourceID, &endpoint.Connector, &endpoint.URL,
		&endpoint.ContentPolicy, &endpoint.ExpectedContentTypes,
		&endpoint.MaxResponseBytes, &cursor, &etag, &lastModified, &state,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load source endpoint %s: %w", registryID, err)
	}
	if cursor != nil {
		endpoint.Checkpoint.Cursor = *cursor
	}
	if etag != nil {
		endpoint.Checkpoint.ETag = *etag
	}
	if lastModified != nil {
		endpoint.Checkpoint.LastModified = *lastModified
	}
	if err := json.Unmarshal(state, &endpoint.Checkpoint.ProviderState); err != nil {
		return nil, fmt.Errorf("decode source checkpoint %s: %w", registryID, err)
	}
	return &endpoint, nil
}

func (store *Store) RecordFetch(ctx context.Context, endpoint ingestion.Endpoint, result fetcher.Result, fetchErr error) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin source fetch record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	officialAdvisoryCollection := result.Outcome == fetcher.OutcomeStored &&
		endpoint.Connector == sources.ConnectorGitHubAdvisories
	if officialAdvisoryCollection {
		// Source-wide locking orders observations across distinct advisory
		// endpoints before either transaction allocates its fetch/observation IDs.
		var lockedSourceID string
		if err := tx.QueryRow(ctx, `
			select source.id from app.sources source
			join app.source_endpoints source_endpoint on source_endpoint.source_id = source.id
			where source.id = $1 and source_endpoint.registry_id = $2
				and source_endpoint.connector = $3
			for update of source`, endpoint.SourceID, endpoint.RegistryID,
			string(sources.ConnectorGitHubAdvisories)).Scan(&lockedSourceID); err != nil {
			return fmt.Errorf("lock official advisory source %q: %w", endpoint.SourceID, err)
		}
	}
	finalFetchID, err := store.fetches.RecordWithFinalAttemptID(ctx, tx, endpoint.RegistryID, endpoint.SourceID, endpoint.ContentPolicy, result)
	if err != nil {
		return err
	}
	last := result.Attempts[len(result.Attempts)-1]
	if result.Outcome == fetcher.OutcomeStored {
		var rawDocumentID string
		var retainedObjectKey string
		var rawRegistryID *string
		var rawConnector string
		var rawContentPolicy string
		err := tx.QueryRow(ctx, `
			select raw.id::text, raw.object_key, raw.source_registry_id,
				coalesce(raw.source_connector, original_endpoint.connector, $4),
				raw.content_policy
			from app.raw_documents raw
			left join app.source_endpoints original_endpoint
				on original_endpoint.registry_id = raw.source_registry_id
			where raw.source_id = $1 and raw.canonical_url = $2
				and raw.raw_sha256 = $3 and raw.source_entry_id is null`,
			endpoint.SourceID, last.FinalURL, result.SHA256[:],
			string(endpoint.Connector),
		).Scan(&rawDocumentID, &retainedObjectKey, &rawRegistryID, &rawConnector, &rawContentPolicy)
		if err != nil {
			return fmt.Errorf("find stored source document: %w", err)
		}
		if result.ObjectKey != retainedObjectKey {
			// The same revision may be fetched again on another day. Its raw row
			// retains the first key, so point this fetch at that evidence and
			// remove the newly committed duplicate before advancing checkpoint.
			updated, err := tx.Exec(ctx, `
				update app.source_fetches set object_key = $2
				where id = (
					select source_fetch.id from app.source_fetches source_fetch
					join app.source_endpoints endpoint on endpoint.id = source_fetch.endpoint_id
					where endpoint.registry_id = $1 and source_fetch.object_key = $3
						and source_fetch.completed_at = $4
					order by source_fetch.id desc limit 1
				)`, endpoint.RegistryID, retainedObjectKey, result.ObjectKey, last.CompletedAt)
			if err != nil {
				return fmt.Errorf("reconcile duplicate source fetch object: %w", err)
			}
			if updated.RowsAffected() != 1 {
				return errors.New("duplicate source fetch attempt was not found")
			}
			if store.objects == nil {
				return errors.New("source fetch object cleanup is not configured")
			}
			var referenced bool
			if err := tx.QueryRow(ctx, `select exists(
				select 1 from app.raw_documents where object_key = $1
			)`, result.ObjectKey).Scan(&referenced); err != nil {
				return fmt.Errorf("check duplicate source object references: %w", err)
			}
			if !referenced {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				if err := store.objects.Delete(cleanupCtx, result.ObjectKey); err != nil {
					return fmt.Errorf("delete duplicate source fetch object: %w", err)
				}
			}
		}
		if rawRegistryID == nil {
			// Preserve the fetched bytes for review, but do not schedule a parser
			// under the later endpoint when the original creator is unknown.
			_, err := tx.Exec(ctx, `
				update app.raw_documents
				set ingestion_error_code = 'provenance_unresolved', ingestion_failed_at = $2
				where id = $1::uuid`, rawDocumentID, last.CompletedAt)
			if err != nil {
				return fmt.Errorf("mark unresolved source provenance: %w", err)
			}
			_, err = tx.Exec(ctx, `
				update app.source_endpoints
				set health_state = 'failed', next_poll_at = null, updated_at = $2
				where registry_id = $1`, endpoint.RegistryID, last.CompletedAt)
			if err != nil {
				return fmt.Errorf("stop source endpoint with unresolved provenance: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit unresolved source provenance: %w", err)
			}
			return fmt.Errorf("source endpoint %s cannot prove raw document %s provenance", endpoint.RegistryID, rawDocumentID)
		}
		if rawConnector != string(endpoint.Connector) || rawContentPolicy != endpoint.ContentPolicy {
			// Redirected endpoints can converge on one canonical raw identity.
			// A different parser or retention policy cannot borrow its provenance.
			_, err := tx.Exec(ctx, `
				update app.source_endpoints
				set health_state = 'failed', next_poll_at = null, updated_at = $2
				where registry_id = $1`, endpoint.RegistryID, last.CompletedAt)
			if err != nil {
				return fmt.Errorf("stop conflicting source endpoint: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit conflicting source endpoint state: %w", err)
			}
			return fmt.Errorf("source endpoint %s conflicts with raw document %s provenance", endpoint.RegistryID, rawDocumentID)
		}
		if officialAdvisoryCollection {
			var observationID int64
			if err := tx.QueryRow(ctx, `
				insert into app.advisory_collection_observations (
					source_id, source_registry_id, source_fetch_id,
					parent_raw_document_id, observed_at
				) values ($1, $2, $3, $4::uuid, $5) returning id`,
				endpoint.SourceID, endpoint.RegistryID, finalFetchID,
				rawDocumentID, last.CompletedAt).Scan(&observationID); err != nil {
				return fmt.Errorf("record official advisory collection observation: %w", err)
			}
			if _, _, err := store.jobs.EnqueueSplitAdvisoryObservation(ctx, tx,
				jobqueue.SplitAdvisoryObservationArgs{ObservationID: observationID}); err != nil {
				return err
			}
		}
		pendingCode := "pending_parse"
		if parsing.IsCollectionConnector(endpoint.Connector) {
			pendingCode = "pending_entries"
		}
		// Keep a durable replay target before the fetch checkpoint advances.
		// This also prevents retention from pruning old, freshly restored
		// evidence while its parse job is still pending.
		_, err = tx.Exec(ctx, `
			update app.raw_documents
			set ingestion_error_code = $2, ingestion_failed_at = $3
			where id = $1::uuid and source_entry_id is null`,
			rawDocumentID, pendingCode, last.CompletedAt)
		if err != nil {
			return fmt.Errorf("mark stored source document pending: %w", err)
		}
		if !officialAdvisoryCollection {
			if _, _, err := store.jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
				RawDocumentID: rawDocumentID, RegistryID: *rawRegistryID,
			}); err != nil {
				return err
			}
		}
	}
	var typed *fetcher.FetchError
	if fetchErr != nil && errors.As(fetchErr, &typed) && !typed.Retryable {
		_, err = tx.Exec(ctx, `
			update app.source_endpoints
			set health_state = 'failed', next_poll_at = null, updated_at = $2
			where registry_id = $1`, endpoint.RegistryID, last.CompletedAt)
		if err != nil {
			return fmt.Errorf("stop permanently failing source endpoint: %w", err)
		}
	} else if last.RetryAfter.After(last.CompletedAt) {
		_, err = tx.Exec(ctx, `
			update app.source_endpoints
			set next_poll_at = greatest(next_poll_at, $2), updated_at = $3
			where registry_id = $1`, endpoint.RegistryID, last.RetryAfter, last.CompletedAt)
		if err != nil {
			return fmt.Errorf("defer source endpoint after rate limit: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (store *Store) LoadRawDocument(ctx context.Context, registryID string, rawDocumentID string) (ingestion.RawDocument, error) {
	var document ingestion.RawDocument
	var rawDigest []byte
	var parentID, entryID *string
	err := store.pool.QueryRow(ctx, `
		select raw.id::text, raw.source_id,
			coalesce(raw.source_connector, legacy.connector),
			raw.canonical_url,
			coalesce(raw.source_content_type, legacy.content_type, ''),
			raw.content_policy, raw.object_key,
			case when raw.source_entry_id is null then endpoint.max_response_bytes
				else greatest(endpoint.max_response_bytes, 1048576) end,
			raw.raw_sha256,
			raw.parent_raw_document_id::text, raw.source_entry_id::text
		from app.raw_documents raw
		left join lateral (
			select min(candidate.registry_id) as registry_id,
				min(candidate.connector) as connector,
				min(candidate.content_type) as content_type
			from (
				select source_endpoint.registry_id, source_endpoint.connector,
					source_fetch.content_type
				from app.source_fetches source_fetch
				join app.source_endpoints source_endpoint on source_endpoint.id = source_fetch.endpoint_id
				where raw.source_registry_id is null and raw.source_entry_id is null
					and source_fetch.object_key = raw.object_key
					and source_fetch.outcome = 'stored'
					and source_fetch.final_url = raw.canonical_url
					and source_fetch.raw_sha256 = raw.raw_sha256
					and source_fetch.attempted_at = raw.first_seen_at
					and source_fetch.completed_at = raw.first_fetched_at
					and source_endpoint.source_id = raw.source_id
				limit 2
			) candidate
			having count(*) = 1
		) legacy on true
		join app.source_endpoints endpoint
			on endpoint.registry_id = coalesce(raw.source_registry_id, legacy.registry_id)
				and endpoint.source_id = raw.source_id
		where coalesce(raw.source_registry_id, legacy.registry_id) = $1
			and raw.id = $2::uuid
		limit 1`, registryID, rawDocumentID).Scan(
		&document.ID, &document.SourceID, &document.Connector,
		&document.URL, &document.ContentType, &document.ContentPolicy, &document.ObjectKey,
		&document.MaxResponseBytes, &rawDigest, &parentID, &entryID,
	)
	if err != nil {
		return ingestion.RawDocument{}, fmt.Errorf("load raw source document: %w", err)
	}
	if len(rawDigest) != sha256.Size {
		return ingestion.RawDocument{}, errors.New("raw source document has invalid SHA-256")
	}
	copy(document.RawSHA256[:], rawDigest)
	if parentID != nil {
		document.ParentRawID = *parentID
	}
	if entryID != nil {
		document.SourceEntryID = *entryID
	}
	return document, nil
}

func (store *Store) RecordEntries(ctx context.Context, parent ingestion.RawDocument, registryID string, entries []parsing.Entry) (int, error) {
	if store.objects == nil || store.clock == nil {
		return 0, errors.New("source entry storage is not configured")
	}
	if parent.ID == "" || parent.SourceID == "" || parent.ParentRawID != "" || parent.ContentPolicy != "link-and-excerpt" || registryID == "" || entries == nil || len(entries) > 500 {
		return 0, errors.New("source entry batch or parent document is invalid")
	}
	var managedParent bool
	if err := store.pool.QueryRow(ctx, `select exists (
		select 1 from app.advisory_collection_observations
		where parent_raw_document_id = $1::uuid
	)`, parent.ID).Scan(&managedParent); err != nil {
		return 0, fmt.Errorf("check source collection observation: %w", err)
	}
	if managedParent {
		return 0, errors.New("managed advisory collection requires observation splitting")
	}
	seen := make(map[string][sha256.Size]byte, len(entries))
	for _, entry := range entries {
		if err := parsing.ValidateEntry(parent.URL, entry); err != nil {
			return 0, fmt.Errorf("validate source entry: %w", err)
		}
		digest := sha256.Sum256(entry.Payload)
		if previous, exists := seen[entry.ExternalID]; exists && previous != digest {
			return 0, errors.New("source entry batch repeats an external ID with conflicting content")
		}
		seen[entry.ExternalID] = digest
	}
	created := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		inserted, err := store.recordEntry(ctx, parent, registryID, entry, 0, 0)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return created, fmt.Errorf("begin source entry completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var priorCode *string
	var failedAt *time.Time
	err = tx.QueryRow(ctx, `
		select ingestion_error_code, ingestion_failed_at
		from app.raw_documents
		where id = $1::uuid and source_registry_id = $2 and source_entry_id is null
		for update`, parent.ID, registryID).Scan(&priorCode, &failedAt)
	if err != nil {
		return created, fmt.Errorf("load source entry completion marker: %w", err)
	}
	_, err = tx.Exec(ctx, `
		update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null
		where id = $1::uuid and source_registry_id = $2
			and source_entry_id is null and ingestion_error_code is not null`,
		parent.ID, registryID)
	if err != nil {
		return created, fmt.Errorf("clear source entry failure: %w", err)
	}
	if priorCode != nil && *priorCode == "source_entry_record_failed" && failedAt != nil {
		// Only this transient recording failure can self-heal. A later fetch
		// failure, operator pause, or another unresolved raw keeps polling off.
		_, err = tx.Exec(ctx, `
			update app.source_endpoints endpoint
			set health_state = 'unverified', next_poll_at = null, updated_at = $3
			where endpoint.registry_id = $1 and endpoint.health_state = 'failed'
				and (endpoint.last_attempt_at is null or endpoint.last_attempt_at <= $2)
				and exists (
					select 1 from app.sources source
					left join app.source_runtime_overrides runtime on runtime.source_id = source.id
					where source.id = endpoint.source_id and source.enabled
						and source.validation_state = 'active'
						and coalesce(runtime.polling_enabled, true)
				)
				and not exists (
					select 1 from app.raw_documents unresolved
					where unresolved.source_registry_id = $1
						and unresolved.ingestion_error_code is not null
						and unresolved.ingestion_error_code <> 'pending_parse'
				)`, registryID, *failedAt, store.clock.Now().UTC())
		if err != nil {
			return created, fmt.Errorf("re-arm recovered source endpoint: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return created, fmt.Errorf("commit source entry completion: %w", err)
	}
	return created, nil
}

func (store *Store) recordEntry(ctx context.Context, parent ingestion.RawDocument, registryID string, entry parsing.Entry, observationID int64, ordinal int) (created bool, recordErr error) {
	observedAt := store.clock.Now().UTC()
	digest := sha256.Sum256(entry.Payload)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, fmt.Errorf("begin source entry record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sourceEntryID string
	var firstSeenAt time.Time
	err = tx.QueryRow(ctx, `
		insert into app.source_entries (source_id, external_id, first_seen_at, last_seen_at)
		values ($1, $2, $3, $3)
		on conflict (source_id, external_id) do update
		set last_seen_at = greatest(app.source_entries.last_seen_at, excluded.last_seen_at)
		returning id::text, first_seen_at`, parent.SourceID, entry.ExternalID, observedAt).Scan(&sourceEntryID, &firstSeenAt)
	if err != nil {
		return false, fmt.Errorf("record source entry identity: %w", err)
	}
	var existingRawID string
	var existingObjectKey *string
	var prunedAt *time.Time
	var pendingCode *string
	err = tx.QueryRow(ctx, `
		select id::text, object_key, raw_pruned_at, ingestion_error_code from app.raw_documents
		where source_entry_id = $1::uuid and raw_sha256 = $2
		for update`, sourceEntryID, digest[:]).Scan(&existingRawID, &existingObjectKey, &prunedAt, &pendingCode)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("find source entry revision: %w", err)
	}
	if err == nil {
		if existingObjectKey != nil {
			if err := recordAdvisoryEntryObservation(ctx, tx, observationID, ordinal, sourceEntryID, existingRawID); err != nil {
				return false, err
			}
			if pendingCode != nil {
				if _, _, err := store.jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
					RawDocumentID: existingRawID, RegistryID: registryID,
				}); err != nil {
					return false, err
				}
			}
			if err := tx.Commit(ctx); err != nil {
				return false, fmt.Errorf("commit existing source entry: %w", err)
			}
			return false, nil
		}
		if prunedAt == nil {
			return false, errors.New("source entry object is missing without a pruning marker")
		}
	}
	identityHash := sha256.Sum256([]byte(entry.ExternalID))
	keySourceID := parent.SourceID + "-entry-" + hex.EncodeToString(identityHash[:])
	objectKey, err := storage.RawObjectKey(keySourceID, firstSeenAt, digest, "application/json")
	if err != nil {
		return false, err
	}
	staged, err := store.objects.Stage(ctx, "application/json", bytes.NewReader(entry.Payload))
	if err != nil {
		return false, fmt.Errorf("stage source entry: %w", err)
	}
	if staged.SHA256 != digest || staged.Bytes != int64(len(entry.Payload)) {
		_ = store.objects.Abort(ctx, staged)
		return false, errors.New("staged source entry digest or length differs from payload")
	}
	if err := store.objects.Commit(ctx, staged, objectKey); err != nil {
		_ = store.objects.Abort(ctx, staged)
		return false, fmt.Errorf("commit source entry object: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A failed transaction may leave an object, but retries reuse this exact key.
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if cleanupErr := store.objects.Delete(cleanupCtx, objectKey); cleanupErr != nil {
				recordErr = errors.Join(recordErr, fmt.Errorf("delete uncommitted source entry object: %w", cleanupErr))
			}
		}
	}()
	var rawID string
	if existingRawID != "" {
		err = tx.QueryRow(ctx, `
			update app.raw_documents
			set object_key = $2, raw_pruned_at = null,
				ingestion_error_code = 'pending_parse', ingestion_failed_at = $3
			where id = $1::uuid and object_key is null and raw_pruned_at is not null
			returning id::text`, existingRawID, objectKey, observedAt).Scan(&rawID)
	} else {
		err = tx.QueryRow(ctx, `
			insert into app.raw_documents (
				source_id, canonical_url, object_key, raw_sha256,
				first_seen_at, first_fetched_at, content_policy,
				parent_raw_document_id, source_entry_id,
				source_registry_id, source_connector, source_content_type,
				ingestion_error_code, ingestion_failed_at
			) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6::uuid, $7::uuid,
				$8, 'source_entry', 'application/json', 'pending_parse', $5)
			returning id::text`, parent.SourceID, entry.URL, objectKey, digest[:],
			observedAt, parent.ID, sourceEntryID, registryID).Scan(&rawID)
	}
	if err != nil {
		return false, fmt.Errorf("record source entry raw document: %w", err)
	}
	if err := recordAdvisoryEntryObservation(ctx, tx, observationID, ordinal, sourceEntryID, rawID); err != nil {
		return false, err
	}
	if _, _, err := store.jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
		RawDocumentID: rawID, RegistryID: registryID,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		// Commit errors can be ambiguous. Keep the object if the durable row exists.
		var persisted bool
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		checkErr := store.pool.QueryRow(checkCtx, `
			select exists(select 1 from app.raw_documents where id = $1::uuid and object_key = $2)`, rawID, objectKey).Scan(&persisted)
		if checkErr != nil || persisted {
			committed = true
		}
		return false, fmt.Errorf("commit source entry: %w", err)
	}
	committed = true
	return existingRawID == "", nil
}
