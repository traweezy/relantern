package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

var (
	ErrJobNotRetryable = errors.New("job is not in a manually retryable state")
	ErrRetryAuditInput = errors.New("manual retry audit fields are invalid")
)

type Operations struct {
	client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
}

type DeadLetter struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"maxAttempts"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinalizedAt *time.Time `json:"finalizedAt"`
}

type RetryRequest struct {
	JobID     int64
	ActorType string
	ActorID   string
	RequestID string
	Reason    string
}

func NewOperations(pool *pgxpool.Pool, client *river.Client[pgx.Tx]) *Operations {
	return &Operations{client: client, pool: pool}
}

func (operations *Operations) ListDeadLetters(ctx context.Context, limit int) ([]DeadLetter, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("dead-letter limit must be from 1 through 200")
	}
	rows, err := operations.pool.Query(ctx, `
		select id, kind, queue, attempt, max_attempts, created_at, finalized_at
		from river.river_job
		where state = 'discarded'
		order by finalized_at desc nulls last, id desc
		limit $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list discarded River jobs: %w", err)
	}
	defer rows.Close()

	deadLetters := make([]DeadLetter, 0, limit)
	for rows.Next() {
		var deadLetter DeadLetter
		if err := rows.Scan(
			&deadLetter.ID,
			&deadLetter.Kind,
			&deadLetter.Queue,
			&deadLetter.Attempt,
			&deadLetter.MaxAttempts,
			&deadLetter.CreatedAt,
			&deadLetter.FinalizedAt,
		); err != nil {
			return nil, fmt.Errorf("scan discarded River job: %w", err)
		}
		deadLetters = append(deadLetters, deadLetter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate discarded River jobs: %w", err)
	}
	return deadLetters, nil
}

func (operations *Operations) Retry(
	ctx context.Context,
	request RetryRequest,
) (*rivertype.JobRow, error) {
	if err := validateRetryRequest(request); err != nil {
		return nil, err
	}
	request.ActorType = strings.TrimSpace(request.ActorType)
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.Reason = strings.TrimSpace(request.Reason)
	tx, err := operations.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin manual River retry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var previousState string
	var kind string
	var queue string
	if err := tx.QueryRow(ctx, `
		select state::text, kind, queue
		from river.river_job
		where id = $1
		for update`, request.JobID).Scan(&previousState, &kind, &queue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rivertype.ErrNotFound
		}
		return nil, fmt.Errorf("lock River job %d for retry: %w", request.JobID, err)
	}
	if previousState != string(rivertype.JobStateCancelled) &&
		previousState != string(rivertype.JobStateDiscarded) {
		return nil, ErrJobNotRetryable
	}

	retriedJob, err := operations.client.JobRetryTx(ctx, tx, request.JobID)
	if err != nil {
		return nil, fmt.Errorf("retry River job %d: %w", request.JobID, err)
	}
	metadata, err := json.Marshal(map[string]string{
		"kind":          kind,
		"previousState": previousState,
		"queue":         queue,
		"reason":        request.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("encode manual retry audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into app.audit_events (
			actor_type,
			actor_id,
			action,
			target_type,
			target_id,
			request_id,
			metadata
		) values ($1, $2, 'job.manual_retry', 'river_job', $3, nullif($4, ''), $5::jsonb)`,
		request.ActorType,
		request.ActorID,
		fmt.Sprintf("%d", request.JobID),
		request.RequestID,
		metadata,
	); err != nil {
		return nil, fmt.Errorf("audit manual River retry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit manual River retry: %w", err)
	}
	return retriedJob, nil
}

func validateRetryRequest(request RetryRequest) error {
	actorType := strings.TrimSpace(request.ActorType)
	actorID := strings.TrimSpace(request.ActorID)
	reason := strings.TrimSpace(request.Reason)
	requestID := strings.TrimSpace(request.RequestID)
	if request.JobID < 1 ||
		len(actorType) < 1 || len(actorType) > 80 ||
		len(actorID) < 1 || len(actorID) > 255 ||
		len(reason) < 1 || len(reason) > 500 ||
		len(requestID) > 255 {
		return ErrRetryAuditInput
	}
	return nil
}
