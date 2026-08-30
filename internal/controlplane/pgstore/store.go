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
	"github.com/traweezy/relantern/internal/jobqueue"
)

type Store struct {
	pool *pgxpool.Pool
	jobs *jobqueue.Inserter
}

const (
	serializableAttempts = 5
	serializableBackoff  = 5 * time.Millisecond
)

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
			return value, fmt.Errorf("wait to retry control-plane transaction: %w", ctx.Err())
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

func New(pool *pgxpool.Pool, configuredJobs ...*jobqueue.Inserter) (*Store, error) {
	if pool == nil {
		return nil, errors.New("control-plane database pool is required")
	}
	var jobs *jobqueue.Inserter
	if len(configuredJobs) > 0 {
		jobs = configuredJobs[0]
	}
	return &Store{pool: pool, jobs: jobs}, nil
}

func recordMutation(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	action string,
	targetType string,
	targetID string,
	metadata map[string]any,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode control-plane mutation metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.audit_events (
			actor_type, actor_id, action, target_type, target_id, metadata
		) values ('owner', $1, $2, $3, $4, $5::jsonb)`,
		userID, action, targetType, targetID, encoded); err != nil {
		return fmt.Errorf("record control-plane audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.outbox_events (
			event_type, aggregate_type, aggregate_id, payload
		) values ($1, $2, $3::uuid, $4::jsonb)`,
		action, targetType, userID, encoded); err != nil {
		return fmt.Errorf("record control-plane outbox event: %w", err)
	}
	return nil
}
