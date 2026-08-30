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
				where revision.raw_document_id = document.id and item.lifecycle_state = 'published'
			)
		order by document.first_seen_at, document.id
		limit $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("select raw retention candidates: %w", err)
	}
	return collectCandidates(rows, "raw")
}

func (store *Store) MarkRawPruned(
	ctx context.Context,
	candidate retention.ObjectCandidate,
	now time.Time,
) (bool, error) {
	command, err := store.pool.Exec(ctx, `
		update app.raw_documents
		set object_key = null, raw_pruned_at = $3
		where id = $1::uuid and object_key = $2 and raw_pruned_at is null`, candidate.ID, candidate.Key, now)
	if err != nil {
		return false, fmt.Errorf("mark raw object pruned: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (store *Store) NormalizedCandidates(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]retention.ObjectCandidate, error) {
	rows, err := store.pool.Query(ctx, `
		select revision.id::text, revision.normalized_text_object_key
		from app.content_revisions revision
		where revision.normalized_text_object_key is not null
			and revision.observed_at < $1
			and not exists (select 1 from app.claims claim where claim.revision_id = revision.id)
			and not exists (
				select 1 from app.item_sources source
				join app.items item on item.id = source.item_id
				where source.revision_id = revision.id and item.lifecycle_state = 'published'
			)
		order by revision.observed_at, revision.id
		limit $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("select normalized retention candidates: %w", err)
	}
	return collectCandidates(rows, "normalized")
}

func (store *Store) MarkNormalizedPruned(
	ctx context.Context,
	candidate retention.ObjectCandidate,
	now time.Time,
) (bool, error) {
	command, err := store.pool.Exec(ctx, `
		update app.content_revisions
		set normalized_text_object_key = null, normalized_text_pruned_at = $3
		where id = $1::uuid and normalized_text_object_key = $2
			and normalized_text_pruned_at is null`, candidate.ID, candidate.Key, now)
	if err != nil {
		return false, fmt.Errorf("mark normalized object pruned: %w", err)
	}
	return command.RowsAffected() == 1, nil
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
