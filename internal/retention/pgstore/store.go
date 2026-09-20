package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/retention"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("retention store requires a database pool")
	}
	if pool.Config().MaxConns < 2 {
		return nil, errors.New("raw retention source guard requires at least two database connections")
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Start(
	ctx context.Context,
	idempotencyKey string,
	policy retention.Policy,
	now time.Time,
) (string, retention.Counts, bool, error) {
	encodedPolicy, err := json.Marshal(map[string]int64{
		"batchSize":               int64(policy.BatchSize),
		"mutationSnapshotSeconds": int64(policy.MutationSnapshots.Seconds()),
		"normalizedSeconds":       int64(policy.NormalizedRevision.Seconds()),
		"operationalSeconds":      int64(policy.OperationalRows.Seconds()),
		"outboxSeconds":           int64(policy.OutboxReplay.Seconds()),
		"rawSeconds":              int64(policy.RawSnapshots.Seconds()),
	})
	if err != nil {
		return "", retention.Counts{}, false, fmt.Errorf("encode retention policy: %w", err)
	}
	var runID string
	err = store.pool.QueryRow(ctx, `
		insert into app.retention_runs (idempotency_key, state, policy, started_at)
		values ($1, 'running', $2::jsonb, $3)
		on conflict (idempotency_key) do nothing
		returning id::text`, idempotencyKey, encodedPolicy, now).Scan(&runID)
	if err == nil {
		return runID, retention.Counts{}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", retention.Counts{}, false, fmt.Errorf("start retention run: %w", err)
	}
	var state string
	var encodedCounts []byte
	if err := store.pool.QueryRow(ctx, `
		select id::text, state, counts
		from app.retention_runs
		where idempotency_key = $1`, idempotencyKey).Scan(&runID, &state, &encodedCounts); err != nil {
		return "", retention.Counts{}, false, fmt.Errorf("select retention run: %w", err)
	}
	counts := retention.Counts{}
	if err := json.Unmarshal(encodedCounts, &counts); err != nil {
		return "", retention.Counts{}, false, fmt.Errorf("decode retention counts: %w", err)
	}
	switch state {
	case "completed":
		return runID, counts, false, nil
	case "running":
		return runID, counts, false, retention.ErrBusy
	case "failed":
		command, err := store.pool.Exec(ctx, `
			update app.retention_runs
			set state = 'running', policy = $2::jsonb,
				started_at = $3, completed_at = null, error_code = null
			where id = $1::uuid and state = 'failed'`, runID, encodedPolicy, now)
		if err != nil {
			return "", retention.Counts{}, false, fmt.Errorf("restart retention run: %w", err)
		}
		if command.RowsAffected() != 1 {
			return runID, counts, false, retention.ErrBusy
		}
		return runID, counts, true, nil
	default:
		return "", retention.Counts{}, false, fmt.Errorf("unknown retention state %q", state)
	}
}

func (store *Store) RawCandidates(ctx context.Context, cutoff time.Time, limit int) ([]retention.ObjectCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		select document.id::text, document.object_key
		from app.raw_documents document
		where document.object_key is not null
			and document.first_seen_at < $1
			and document.ingestion_error_code is null
			and not exists (
				select 1 from app.advisory_collection_observations observation
				where observation.parent_raw_document_id = document.id
					and observation.state = 'pending'
			)
			and not exists (
				select 1 from app.advisory_collection_observations observation
				where document.source_entry_id is not null
					and observation.source_id = document.source_id
					and observation.state = 'pending'
			)
			and not exists (
				select 1
				from app.content_revisions revision
				join app.claims claim on claim.revision_id = revision.id
				where revision.raw_document_id = document.id
			)
			and not exists (
				select 1
				from app.content_revisions revision
				join app.item_sources source on source.revision_id = revision.id
				join app.items item on item.id = source.item_id
				where revision.raw_document_id = document.id and (
					item.lifecycle_state = 'published'
					or exists (
						select 1 from app.cluster_members member
						join app.research_briefs brief on brief.cluster_id = member.cluster_id
						where member.item_id = item.id
					)
				)
			)
		order by document.first_seen_at, document.id
		limit $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("select raw retention candidates: %w", err)
	}
	return collectCandidates(rows, "raw")
}

func (store *Store) PruneRaw(
	ctx context.Context,
	candidate retention.ObjectCandidate,
	cutoff time.Time,
	now time.Time,
	objects retention.ObjectStore,
) (pruned bool, pruneErr error) {
	// A new advisory observation takes FOR UPDATE on the source before it can
	// reuse a raw object. Hold the conflicting source lock until deletion ends;
	// the raw reference itself must still commit before object deletion.
	guard, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, fmt.Errorf("begin raw retention source guard: %w", err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := guard.Rollback(releaseCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			pruneErr = errors.Join(pruneErr, fmt.Errorf("release raw retention source guard: %w", err))
		}
	}()
	var sourceID string
	err = guard.QueryRow(ctx, `
		select source.id from app.sources source
		join app.raw_documents document on document.source_id = source.id
		where document.id = $1::uuid
		for share of source`, candidate.ID).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock raw retention source: %w", err)
	}
	command, err := store.pool.Exec(ctx, `
		update app.raw_documents document
		set object_key = null, raw_pruned_at = $4
		where document.id = $1::uuid and document.object_key = $2
			and document.raw_pruned_at is null and document.first_seen_at < $3
			and document.ingestion_error_code is null
			and not exists (
				select 1 from app.advisory_collection_observations observation
				where observation.parent_raw_document_id = document.id
					and observation.state = 'pending'
			)
			and not exists (
				select 1 from app.advisory_collection_observations observation
				where document.source_entry_id is not null
					and observation.source_id = document.source_id
					and observation.state = 'pending'
			)
			and not exists (
				select 1 from app.content_revisions revision
				join app.claims claim on claim.revision_id = revision.id
				where revision.raw_document_id = document.id
			)
			and not exists (
				select 1 from app.content_revisions revision
				join app.item_sources source on source.revision_id = revision.id
				join app.items item on item.id = source.item_id
				where revision.raw_document_id = document.id and (
					item.lifecycle_state = 'published'
					or exists (
						select 1 from app.cluster_members member
						join app.research_briefs brief on brief.cluster_id = member.cluster_id
						where member.item_id = item.id
					)
				)
			)`, candidate.ID, candidate.Key, cutoff, now)
	if err != nil {
		return false, fmt.Errorf("mark eligible raw object pruned: %w", err)
	}
	if command.RowsAffected() == 0 {
		return false, nil
	}
	var protected, reused bool
	err = store.pool.QueryRow(ctx, `
		select
			exists (
				select 1 from app.content_revisions revision
				where revision.raw_document_id = $1::uuid and (
					exists (select 1 from app.claims claim where claim.revision_id = revision.id)
					or exists (
						select 1 from app.item_sources source
						join app.items item on item.id = source.item_id
						where source.revision_id = revision.id and (
							item.lifecycle_state = 'published'
							or exists (
								select 1 from app.cluster_members member
								join app.research_briefs brief on brief.cluster_id = member.cluster_id
								where member.item_id = item.id
							)
						)
					)
				)
			)
			or exists (
				select 1 from app.advisory_collection_observations observation
				where observation.parent_raw_document_id = $1::uuid
					and observation.state = 'pending'
			)
			or exists (
				select 1 from app.raw_documents document
				join app.advisory_collection_observations observation
					on observation.source_id = document.source_id
					and observation.state = 'pending'
				where document.id = $1::uuid and document.source_entry_id is not null
			),
			exists (select 1 from app.raw_documents where object_key = $2)`,
		candidate.ID, candidate.Key).Scan(&protected, &reused)
	if err != nil {
		return false, errors.Join(fmt.Errorf("recheck raw object protection: %w", err), store.restoreRaw(ctx, candidate, now))
	}
	if reused {
		// A newer row now owns the same key. The old reference was pruned, but
		// deleting the shared object would erase the newer row's evidence.
		return false, nil
	}
	if protected {
		return false, store.restoreRaw(ctx, candidate, now)
	}
	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := objects.Delete(deleteCtx, candidate.Key); err != nil {
		// Delete may have reached object storage before its response failed.
		// Keep the database reference pruned rather than point at a missing object.
		return false, fmt.Errorf("delete pruned raw object %q; inspect for an orphan: %w", candidate.Key, err)
	}
	return true, nil
}

func (store *Store) NormalizedCandidates(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]retention.ObjectCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		select min(revision.id::text), revision.normalized_text_object_key
		from app.content_revisions revision
		where revision.normalized_text_object_key is not null
		group by revision.normalized_text_object_key
		having bool_and(revision.observed_at < $1)
			and bool_and(not exists (
				select 1 from app.raw_documents raw
				where raw.id = revision.raw_document_id and raw.ingestion_error_code is not null
			))
			and bool_and(not exists (
				select 1 from app.claims claim where claim.revision_id = revision.id
			))
			and bool_and(not exists (
				select 1 from app.item_sources source
				join app.items item on item.id = source.item_id
				where source.revision_id = revision.id and (
					item.lifecycle_state = 'published'
					or exists (
						select 1 from app.cluster_members member
						join app.research_briefs brief on brief.cluster_id = member.cluster_id
						where member.item_id = item.id
					)
				)
			))
		order by min(revision.observed_at), revision.normalized_text_object_key
		limit $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("select normalized retention candidates: %w", err)
	}
	return collectCandidates(rows, "normalized")
}

func (store *Store) PruneNormalized(
	ctx context.Context,
	candidate retention.ObjectCandidate,
	cutoff time.Time,
	now time.Time,
	objects retention.ObjectStore,
) (pruned bool, resultError error) {
	connection, err := store.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire normalized retention connection: %w", err)
	}
	if _, err := connection.Exec(ctx, `
		select pg_advisory_lock(hashtextextended('relantern:normalized:' || $1, 0))`, candidate.Key); err != nil {
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = connection.Conn().Close(closeContext)
		connection.Release()
		return false, fmt.Errorf("lock normalized object key: %w", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockError := connection.QueryRow(unlockContext, `
			select pg_advisory_unlock(hashtextextended('relantern:normalized:' || $1, 0))`, candidate.Key).Scan(&unlocked)
		if unlockError != nil || !unlocked {
			closeContext, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			resultError = errors.Join(resultError, fmt.Errorf("release normalized object key lock: %v, unlocked=%t", unlockError, unlocked), connection.Conn().Close(closeContext))
		}
		connection.Release()
	}()
	return store.pruneNormalizedLocked(ctx, connection, candidate, cutoff, now, objects)
}

func (store *Store) pruneNormalizedLocked(
	ctx context.Context,
	connection *pgxpool.Conn,
	candidate retention.ObjectCandidate,
	cutoff time.Time,
	now time.Time,
	objects retention.ObjectStore,
) (bool, error) {
	rows, err := connection.Query(ctx, `
		update app.content_revisions revision
		set normalized_text_object_key = null, normalized_text_pruned_at = $3
		where revision.normalized_text_object_key = $2
			and exists (select 1 from app.content_revisions representative
				where representative.id = $1::uuid and representative.normalized_text_object_key = $2)
			and not exists (
				select 1 from app.content_revisions other
				join app.raw_documents raw on raw.id = other.raw_document_id
				where other.normalized_text_object_key = $2 and (
					other.observed_at >= $4 or other.normalized_text_pruned_at is not null
					or raw.ingestion_error_code is not null
					or exists (select 1 from app.claims claim where claim.revision_id = other.id)
					or exists (
						select 1 from app.item_sources source
						join app.items item on item.id = source.item_id
						where source.revision_id = other.id and (
							item.lifecycle_state = 'published'
							or exists (
								select 1 from app.cluster_members member
								join app.research_briefs brief on brief.cluster_id = member.cluster_id
								where member.item_id = item.id
							)
						)
					)
				)
			)
		returning revision.id::text`, candidate.ID, candidate.Key, now, cutoff)
	if err != nil {
		return false, fmt.Errorf("mark eligible normalized object pruned: %w", err)
	}
	ids := make([]string, 0, 1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan pruned normalized revision: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("iterate pruned normalized revisions: %w", err)
	}
	rows.Close()
	if len(ids) == 0 {
		return false, nil
	}
	var protected, reused bool
	err = connection.QueryRow(ctx, `
		select
			exists (
				select 1 from app.content_revisions revision
				where revision.id = any($1::uuid[]) and (
					exists (select 1 from app.claims claim where claim.revision_id = revision.id)
					or exists (
						select 1 from app.item_sources source
						join app.items item on item.id = source.item_id
						where source.revision_id = revision.id and (
							item.lifecycle_state = 'published'
							or exists (
								select 1 from app.cluster_members member
								join app.research_briefs brief on brief.cluster_id = member.cluster_id
								where member.item_id = item.id
							)
						)
					)
				)
			),
			exists (select 1 from app.content_revisions where normalized_text_object_key = $2)`,
		ids, candidate.Key).Scan(&protected, &reused)
	if err != nil {
		return false, errors.Join(fmt.Errorf("recheck normalized object protection: %w", err), store.restoreNormalized(ctx, connection, candidate, now, ids))
	}
	if protected || reused {
		return false, store.restoreNormalized(ctx, connection, candidate, now, ids)
	}
	if err := objects.Delete(ctx, candidate.Key); err != nil {
		// A transport error cannot distinguish a failed delete from a lost
		// success response, so references stay pruned in both cases.
		return false, fmt.Errorf("delete pruned normalized object %q; inspect for an orphan: %w", candidate.Key, err)
	}
	return true, nil
}

func (store *Store) restoreRaw(ctx context.Context, candidate retention.ObjectCandidate, now time.Time) error {
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	command, err := store.pool.Exec(restoreCtx, `
		update app.raw_documents
		set object_key = $2, raw_pruned_at = null
		where id = $1::uuid and object_key is null and raw_pruned_at = $3`, candidate.ID, candidate.Key, now)
	if err != nil {
		return fmt.Errorf("restore raw object reference: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("raw object reference changed before restoration")
	}
	return nil
}

func (store *Store) restoreNormalized(ctx context.Context, connection *pgxpool.Conn, candidate retention.ObjectCandidate, now time.Time, ids []string) error {
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	command, err := connection.Exec(restoreCtx, `
		update app.content_revisions
		set normalized_text_object_key = $2, normalized_text_pruned_at = null
		where id = any($1::uuid[]) and normalized_text_object_key is null
			and normalized_text_pruned_at = $3`, ids, candidate.Key, now)
	if err != nil {
		return fmt.Errorf("restore normalized object references: %w", err)
	}
	if command.RowsAffected() != int64(len(ids)) {
		return errors.New("normalized object references changed before restoration")
	}
	return nil
}

func (store *Store) PruneOperational(
	ctx context.Context,
	policy retention.Policy,
	now time.Time,
) (retention.Counts, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return retention.Counts{}, fmt.Errorf("begin operational retention: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	counts := retention.Counts{}
	if counts.ParseAttemptsPruned, err = deleteBefore(ctx, transaction,
		"delete from app.source_parse_attempts where attempted_at < $1", now.Add(-policy.OperationalRows)); err != nil {
		return retention.Counts{}, err
	}
	if counts.FetchAttemptsPruned, err = deleteBefore(ctx, transaction,
		"delete from app.source_fetches where attempted_at < $1", now.Add(-policy.OperationalRows)); err != nil {
		return retention.Counts{}, err
	}
	if counts.WebhookEventsPruned, err = deleteBefore(ctx, transaction,
		"delete from app.openai_webhook_events where processed_at is not null and processed_at < $1", now.Add(-policy.MutationSnapshots)); err != nil {
		return retention.Counts{}, err
	}
	if counts.OutboxEventsPruned, err = deleteBefore(ctx, transaction,
		"delete from app.outbox_events where created_at < $1", now.Add(-policy.OutboxReplay)); err != nil {
		return retention.Counts{}, err
	}
	command, err := transaction.Exec(ctx, `
		update app.item_state_mutations
		set before_state = null, after_state = null, compacted_at = $2
		where before_state is not null and created_at < $1`, now.Add(-policy.MutationSnapshots), now)
	if err != nil {
		return retention.Counts{}, fmt.Errorf("compact mutation snapshots: %w", err)
	}
	counts.MutationStatesCompacted = command.RowsAffected()
	if err := transaction.Commit(ctx); err != nil {
		return retention.Counts{}, fmt.Errorf("commit operational retention: %w", err)
	}
	return counts, nil
}

func (store *Store) Complete(ctx context.Context, runID string, counts retention.Counts, now time.Time) error {
	encoded, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("encode retention counts: %w", err)
	}
	command, err := store.pool.Exec(ctx, `
		update app.retention_runs
		set state = 'completed', counts = $2::jsonb, completed_at = $3
		where id = $1::uuid and state = 'running'`, runID, encoded, now)
	if err != nil {
		return fmt.Errorf("complete retention run: %w", err)
	}
	if command.RowsAffected() != 1 {
		return retention.ErrBusy
	}
	return nil
}

func (store *Store) Fail(
	ctx context.Context,
	runID string,
	errorCode string,
	counts retention.Counts,
	now time.Time,
) error {
	encoded, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("encode failed retention counts: %w", err)
	}
	_, err = store.pool.Exec(ctx, `
		update app.retention_runs
		set state = 'failed', counts = $3::jsonb, completed_at = $4, error_code = $2
		where id = $1::uuid and state = 'running'`, runID, errorCode, encoded, now)
	if err != nil {
		return fmt.Errorf("fail retention run: %w", err)
	}
	return nil
}

type candidateRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

func collectCandidates(rows candidateRows, kind string) ([]retention.ObjectCandidate, error) {
	defer rows.Close()
	candidates := make([]retention.ObjectCandidate, 0)
	for rows.Next() {
		var candidate retention.ObjectCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Key); err != nil {
			return nil, fmt.Errorf("scan %s retention candidate: %w", kind, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s retention candidates: %w", kind, err)
	}
	return candidates, nil
}

func deleteBefore(ctx context.Context, transaction pgx.Tx, statement string, cutoff time.Time) (int64, error) {
	command, err := transaction.Exec(ctx, statement, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune operational rows: %w", err)
	}
	return command.RowsAffected(), nil
}
