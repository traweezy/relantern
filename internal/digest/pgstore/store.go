package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/jobqueue"
)

type Store struct {
	pool *pgxpool.Pool
	jobs *jobqueue.Inserter
}

func New(pool *pgxpool.Pool, jobs *jobqueue.Inserter) (*Store, error) {
	if pool == nil || jobs == nil {
		return nil, errors.New("digest PostgreSQL store requires database and queue dependencies")
	}
	return &Store{pool: pool, jobs: jobs}, nil
}

func (store *Store) QueueOccurrence(ctx context.Context, occurrenceID string) error {
	return retrySerializable(ctx, func() error {
		return store.queueOccurrence(ctx, occurrenceID)
	})
}

func (store *Store) queueOccurrence(ctx context.Context, occurrenceID string) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin digest occurrence handoff: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var scheduleType, state string
	if err := transaction.QueryRow(ctx, `
		select schedule.schedule_type, occurrence.state
		from app.schedule_occurrences occurrence
		join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
		where occurrence.id = $1::uuid
		for update`, occurrenceID).Scan(&scheduleType, &state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digest.ErrNotFound
		}
		return fmt.Errorf("select digest occurrence: %w", err)
	}
	if scheduleType != "daily_digest" {
		return fmt.Errorf("%w: occurrence is not a daily digest", digest.ErrInvalid)
	}
	if terminalOccurrence(state) {
		return transaction.Commit(ctx)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'preparing', started_at = coalesce(started_at, now()),
			completed_at = null, error_code = null,
			metadata = metadata || '{"digestStage":"queued"}'::jsonb
		where id = $1::uuid`, occurrenceID); err != nil {
		return fmt.Errorf("mark digest occurrence preparing: %w", err)
	}
	if _, _, err := store.jobs.EnqueuePreflightDigestSources(ctx, transaction, jobqueue.PreflightDigestSourcesArgs{
		OccurrenceID: occurrenceID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit digest occurrence handoff: %w", err)
	}
	return nil
}

const (
	serializableAttempts = 5
	serializableBackoff  = 5 * time.Millisecond
)

func retrySerializable(ctx context.Context, operation func() error) error {
	_, err := retrySerializableValue(ctx, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

func retrySerializableValue[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var value T
	var err error
	for attempt := 0; attempt < serializableAttempts; attempt++ {
		value, err = operation()
		if !retryableTransactionError(err) {
			return value, err
		}
		if attempt == serializableAttempts-1 {
			break
		}
		timer := time.NewTimer(serializableBackoff << attempt)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return value, fmt.Errorf("wait to retry digest transaction: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return value, err
}

func retryableTransactionError(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		(postgresError.Code == "40001" || postgresError.Code == "40P01")
}

func (store *Store) Preflight(ctx context.Context, occurrenceID string, now time.Time) error {
	return retrySerializable(ctx, func() error {
		return store.preflight(ctx, occurrenceID, now)
	})
}

func (store *Store) preflight(ctx context.Context, occurrenceID string, now time.Time) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin digest preflight: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var state string
	if err := transaction.QueryRow(ctx, `
		select state from app.schedule_occurrences where id = $1::uuid for update`, occurrenceID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digest.ErrNotFound
		}
		return fmt.Errorf("select digest preflight occurrence: %w", err)
	}
	if terminalOccurrence(state) {
		return transaction.Commit(ctx)
	}
	var stalePrioritySources int
	var duePrioritySources int
	if err := transaction.QueryRow(ctx, `
		select
			count(*) filter (where endpoint.last_success_at is null or endpoint.last_success_at < $1::timestamptz - interval '15 minutes')::integer,
			count(*) filter (where endpoint.next_poll_at is not null and endpoint.next_poll_at <= $1::timestamptz)::integer
		from app.source_endpoints endpoint
		join app.sources source on source.id = endpoint.source_id
		left join app.source_runtime_overrides override on override.source_id = source.id
		where source.enabled and source.trust_tier = 'T0'
			and source.validation_state = 'active'
			and endpoint.health_state <> 'paused'
			and coalesce(override.polling_enabled, true)`, now.UTC()).Scan(&stalePrioritySources, &duePrioritySources); err != nil {
		return fmt.Errorf("inspect priority source freshness: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"digestStage":          "preflighted",
		"preflightAt":          now.UTC(),
		"stalePrioritySources": stalePrioritySources,
		"duePrioritySources":   duePrioritySources,
	})
	if err != nil {
		return fmt.Errorf("encode digest preflight metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences
		set metadata = metadata || $2::jsonb
		where id = $1::uuid`, occurrenceID, metadata); err != nil {
		return fmt.Errorf("record digest preflight: %w", err)
	}
	if _, _, err := store.jobs.EnqueuePrepareDailyDigest(ctx, transaction, jobqueue.PrepareDailyDigestArgs{
		OccurrenceID: occurrenceID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit digest preflight: %w", err)
	}
	return nil
}

func terminalOccurrence(state string) bool {
	return state == "delivered" || state == "skipped" || state == "missed"
}
