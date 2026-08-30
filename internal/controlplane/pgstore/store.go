package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/jobqueue"
)

type Store struct {
	pool *pgxpool.Pool
	jobs *jobqueue.Inserter
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
