package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/sources"
)

const advisoryCatchUpPageSize = 16

// River snoozes catch-up work while a newer collection may supersede its
// evidence. A normal retry would eventually exhaust its attempt limit.
var ErrAdvisoryObservationBackfillPending = errors.New("advisory observation backfill is waiting for source assessment")

type advisoryRevision struct {
	rawDocumentID string
	revisionID    string
	endpointURL   string
	parentURL     string
	pendingSource bool
}

// CatchUp pages through current official advisory revisions. For observed
// entries the latest ordered event, including a repeat that reuses an older
// raw child, establishes currentness instead of the child's creation time.
// Each page and its successor commit together, so retries cannot silently
// leave a partially enqueued page behind.
func (store *Store) CatchUp(ctx context.Context, args jobqueue.ReassessCurrentAdvisoriesArgs) error {
	if _, err := uuid.Parse(args.UserID); err != nil || args.SettingsVersion < 1 {
		return errors.New("advisory catch-up requires an owner and settings version")
	}
	if args.Cursor != "" {
		if _, err := uuid.Parse(args.Cursor); err != nil {
			return errors.New("advisory catch-up cursor is invalid")
		}
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin advisory catch-up page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var hasActiveWatch bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from app.watched_technologies
			where user_id = $1::uuid and ecosystem is not null and status = 'active'
		)`, args.UserID).Scan(&hasActiveWatch); err != nil {
		return fmt.Errorf("check active advisory watches: %w", err)
	}
	if !hasActiveWatch {
		return tx.Commit(ctx)
	}

	rows, err := tx.Query(ctx, `
		select distinct revision.id, child.id, endpoint.url, parent.canonical_url,
			exists (
				select 1 from app.advisory_collection_observations pending
				where pending.source_id = child.source_id and pending.state = 'pending'
			)
		from app.content_revisions revision
		join app.raw_documents child on child.id = revision.raw_document_id
		join app.raw_documents parent on parent.id = child.parent_raw_document_id
		join app.source_entries entry on entry.id = child.source_entry_id
		join app.sources source on source.id = child.source_id
		join app.source_endpoints endpoint on endpoint.registry_id = child.source_registry_id
		join app.item_sources item_source on item_source.revision_id = revision.id
		join app.items item on item.id = item_source.item_id
		left join lateral (
			select event.id, event.child_raw_document_id, event.state,
				collection.state as collection_state
			from app.advisory_entry_observations event
			join app.advisory_collection_observations collection
				on collection.id = event.collection_observation_id
			where event.source_entry_id = entry.id
			order by collection.id desc, event.entry_ordinal desc
			limit 1
		) latest on true
		where revision.id > coalesce(nullif($1::text, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
			and child.source_id = parent.source_id and child.source_id = entry.source_id
			and parent.source_registry_id = child.source_registry_id
			and parent.source_connector = 'github_advisories'
			and child.source_connector = 'source_entry'
			and endpoint.source_id = child.source_id and endpoint.connector = 'github_advisories'
			and (parent.canonical_url = endpoint.url or (
				endpoint.url = '`+sources.GlobalReviewedAdvisoriesURL+`' and (latest.id is not null or exists (
					select 1 from app.source_fetches source_fetch
					where source_fetch.endpoint_id = endpoint.id
						and source_fetch.outcome = 'stored'
						and source_fetch.final_url = parent.canonical_url
						and source_fetch.object_key = parent.object_key
						and source_fetch.raw_sha256 = parent.raw_sha256
						and source_fetch.attempted_at = parent.first_seen_at
						and source_fetch.completed_at = parent.first_fetched_at
				))
			))
			and child.content_policy = 'link-and-excerpt'
			and child.object_key is not null and child.raw_pruned_at is null
			and child.ingestion_error_code is null and parent.ingestion_error_code is null
			and source.enabled and source.validation_state = 'active'
			and source.trust_tier in ('T0', 'T1')
			and item.status in ('active', 'updated')
			and item_source.source_tier = source.trust_tier
			and item_source.canonical_url = child.canonical_url
			and (
				(latest.id is not null
					and latest.child_raw_document_id = child.id
					and latest.state = 'processed'
					and latest.collection_state = 'processed')
				or (latest.id is null
					and not exists (
						select 1 from app.advisory_collection_observations collection
						where collection.parent_raw_document_id = child.parent_raw_document_id
					)
					and not exists (
						select 1 from app.raw_documents newer
						where newer.source_entry_id = child.source_entry_id
							and (newer.first_seen_at, newer.id) > (child.first_seen_at, child.id)
					))
			)
		order by revision.id
		limit $2`, args.Cursor, advisoryCatchUpPageSize)
	if err != nil {
		return fmt.Errorf("select current advisory catch-up page: %w", err)
	}
	page := make([]advisoryRevision, 0, advisoryCatchUpPageSize)
	for rows.Next() {
		var candidate advisoryRevision
		if err := rows.Scan(&candidate.revisionID, &candidate.rawDocumentID,
			&candidate.endpointURL, &candidate.parentURL, &candidate.pendingSource); err != nil {
			rows.Close()
			return fmt.Errorf("scan advisory catch-up candidate: %w", err)
		}
		page = append(page, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate advisory catch-up page: %w", err)
	}
	rows.Close()

	for _, candidate := range page {
		if !officialAdvisoryParent(candidate.endpointURL, candidate.parentURL) {
			continue
		}
		if candidate.pendingSource {
			return ErrAdvisoryObservationBackfillPending
		}
		if _, _, err := store.jobs.EnqueueBackfillCriticalAdvisory(ctx, tx,
			jobqueue.AssessCriticalAdvisoryArgs{
				RawDocumentID: candidate.rawDocumentID, RevisionID: candidate.revisionID,
			}); err != nil {
			return err
		}
	}
	if len(page) == advisoryCatchUpPageSize {
		args.Cursor = page[len(page)-1].revisionID
		if _, _, err := store.jobs.EnqueueReassessCurrentAdvisories(ctx, tx, args); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit advisory catch-up page: %w", err)
	}
	return nil
}

// assessObservedBackfill replays a completed observation only to fill gaps
// created by later watch edits. It never changes event state or reapplies a
// correction. The source lock serializes currentness with new collection
// capture and ordered assessment.
func (store *Store) assessObservedBackfill(ctx context.Context, rawDocumentID, revisionID string) error {
	var eventID int64
	var sourceID string
	err := store.pool.QueryRow(ctx, `
		select latest.id, target.source_id
		from app.raw_documents target
		join lateral (
			select event.id, event.child_raw_document_id
			from app.advisory_entry_observations event
			join app.advisory_collection_observations collection
				on collection.id = event.collection_observation_id
			where event.source_entry_id = target.source_entry_id
			order by collection.id desc, event.entry_ordinal desc
			limit 1
		) latest on latest.child_raw_document_id = target.id
		where target.id = $1::uuid`, rawDocumentID).Scan(&eventID, &sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find current advisory observation for backfill: %w", err)
	}
	preflight, ok, err := loadObservationEvidenceState(ctx, store.pool, eventID,
		revisionID, "processed", false)
	if err != nil {
		return err
	}
	if !ok {
		return store.checkUnavailableObservedBackfillEvidence(ctx, sourceID, eventID,
			rawDocumentID, revisionID)
	}
	advisory, sourceURL, err := store.readAdvisory(ctx, preflight.evidence)
	if err != nil {
		return err
	}
	now := store.clock.Now().UTC()
	if now.IsZero() {
		return errors.New("critical advisory backfill clock is zero")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin observed advisory backfill: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedSourceID string
	if err := tx.QueryRow(ctx, `select id from app.sources where id = $1 for update`,
		preflight.sourceID).Scan(&lockedSourceID); err != nil {
		return fmt.Errorf("lock advisory backfill source: %w", err)
	}
	var latestEventID int64
	err = tx.QueryRow(ctx, `
		select event.id
		from app.advisory_entry_observations event
		join app.advisory_collection_observations collection
			on collection.id = event.collection_observation_id
		where event.source_entry_id = $1::uuid
		order by collection.id desc, event.entry_ordinal desc
		limit 1`, preflight.entryRowID).Scan(&latestEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck current advisory observation: %w", err)
	}
	if latestEventID != eventID {
		return nil
	}
	var pending bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from app.advisory_collection_observations collection
			where collection.source_id = $1 and collection.state = 'pending'
		)`, preflight.sourceID).Scan(&pending); err != nil {
		return fmt.Errorf("check pending advisory collections before backfill: %w", err)
	}
	if pending {
		return ErrAdvisoryObservationBackfillPending
	}
	current, ok, err := loadObservationEvidenceState(ctx, tx, eventID,
		revisionID, "processed", true)
	if err != nil {
		return err
	}
	if !ok {
		expected, err := currentObservedBackfillEvidenceExpected(ctx, tx, eventID,
			rawDocumentID, revisionID)
		if err != nil {
			return err
		}
		if expected {
			return errors.New("current advisory backfill evidence is unavailable or invalid")
		}
		return nil
	}
	if !sameObservationEvidence(preflight, current) ||
		lockedSourceID != current.sourceID || current.rawDocumentID != rawDocumentID {
		return errors.New("advisory backfill evidence changed during assessment")
	}
	if current.title == "" || len(current.title) > 500 ||
		sourceURL == "" || len(sourceURL) > 4096 {
		return errors.New("advisory title or source URL exceeds alert bounds")
	}
	if advisoryCorrectionReason(advisory) != "" ||
		(current.itemStatus != "active" && current.itemStatus != "updated") {
		return nil
	}
	if err := store.admitObservedAlerts(ctx, tx, current, revisionID,
		advisory, sourceURL, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit observed advisory backfill: %w", err)
	}
	return nil
}

// A replay can lose its raw object or another evidence invariant between
// catch-up enqueue and assessment. Retention takes the source lock, so this
// check distinguishes current eligible evidence from stale or disabled work.
func (store *Store) checkUnavailableObservedBackfillEvidence(
	ctx context.Context, sourceID string, eventID int64, rawDocumentID, revisionID string,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin missing advisory backfill check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedSourceID string
	err = tx.QueryRow(ctx, `select id from app.sources where id = $1 for update`,
		sourceID).Scan(&lockedSourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock advisory backfill source for evidence check: %w", err)
	}
	var pending bool
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from app.advisory_collection_observations
		where source_id = $1 and state = 'pending'
	)`, lockedSourceID).Scan(&pending); err != nil {
		return fmt.Errorf("check pending advisory collections for evidence check: %w", err)
	}
	expected, err := currentObservedBackfillEvidenceExpected(ctx, tx, eventID,
		rawDocumentID, revisionID)
	if err != nil {
		return err
	}
	if pending {
		if expected {
			return ErrAdvisoryObservationBackfillPending
		}
		return nil // A pending event is owned by ordered assessment.
	}
	if expected {
		return errors.New("current advisory backfill evidence is unavailable or invalid")
	}
	return nil
}

func currentObservedBackfillEvidenceExpected(
	ctx context.Context, tx pgx.Tx, eventID int64, rawDocumentID, revisionID string,
) (bool, error) {
	var expected bool
	err := tx.QueryRow(ctx, `select exists (
		select 1
		from app.advisory_entry_observations event
		join app.advisory_collection_observations collection
			on collection.id = event.collection_observation_id
		join app.raw_documents child on child.id = event.child_raw_document_id
		join app.content_revisions revision on revision.raw_document_id = child.id
		join app.item_sources item_source on item_source.revision_id = revision.id
		join app.items item on item.id = item_source.item_id
		join app.sources source on source.id = collection.source_id
		where event.id = $1 and child.id = $2::uuid and revision.id = $3::uuid
			and event.state = 'processed' and collection.state = 'processed'
			and source.id = child.source_id and source.enabled
			and source.validation_state = 'active'
			and source.trust_tier in ('T0', 'T1')
			and item.status in ('active', 'updated')
			and item_source.source_tier = source.trust_tier
			and item_source.canonical_url = child.canonical_url
			and not exists (
				select 1 from app.advisory_entry_observations later
				join app.advisory_collection_observations later_collection
					on later_collection.id = later.collection_observation_id
				where later.source_entry_id = event.source_entry_id
					and (later_collection.id, later.entry_ordinal)
						> (collection.id, event.entry_ordinal)
			)
	)`, eventID, rawDocumentID, revisionID).Scan(&expected)
	if err != nil {
		return false, fmt.Errorf("inspect current advisory backfill evidence: %w", err)
	}
	return expected, nil
}
