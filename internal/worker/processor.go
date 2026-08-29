package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/fakeprovider"
	"github.com/traweezy/relantern/internal/jobqueue"
)

var errOccurrenceNotFound = errors.New("schedule occurrence not found")

type Processor struct {
	pool        *pgxpool.Pool
	deliveryURL string
	client      *http.Client
}

type occurrence struct {
	id             string
	idempotencyKey string
	localDate      time.Time
	scheduledFor   time.Time
}

func NewProcessor(pool *pgxpool.Pool, deliveryURL string, requestTimeout time.Duration) *Processor {
	return &Processor{
		pool:        pool,
		deliveryURL: deliveryURL,
		client:      &http.Client{Timeout: requestTimeout},
	}
}

func (processor *Processor) Process(ctx context.Context, occurrenceID string) error {
	if _, err := uuid.Parse(occurrenceID); err != nil {
		return jobqueue.Permanent(fmt.Errorf("invalid occurrence ID: %w", err))
	}

	selected, err := processor.claim(ctx, occurrenceID)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	if err := processor.deliver(ctx, *selected); err != nil {
		if updateErr := processor.recordFailure(ctx, selected.id, err); updateErr != nil {
			return fmt.Errorf("deliver occurrence: %w; record failure: %v", err, updateErr)
		}
		return err
	}
	if err := processor.recordDelivery(ctx, selected.id); err != nil {
		return err
	}
	return nil
}

func (processor *Processor) claim(ctx context.Context, occurrenceID string) (*occurrence, error) {
	tx, err := processor.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin occurrence claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var selected occurrence
	var state string
	err = tx.QueryRow(ctx, `
		select id::text, idempotency_key, local_date, scheduled_for, state
		from app.schedule_occurrences
		where id = $1::uuid
		for update`, occurrenceID).Scan(
		&selected.id,
		&selected.idempotencyKey,
		&selected.localDate,
		&selected.scheduledFor,
		&state,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, jobqueue.Permanent(errOccurrenceNotFound)
		}
		return nil, fmt.Errorf("select occurrence: %w", err)
	}
	if state == "delivered" || state == "skipped" || state == "missed" {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit terminal occurrence check: %w", err)
		}
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'delivering', started_at = coalesce(started_at, now()),
			completed_at = null, error_code = null
		where id = $1::uuid`, selected.id); err != nil {
		return nil, fmt.Errorf("claim occurrence %s: %w", selected.id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit occurrence claim: %w", err)
	}
	return &selected, nil
}

func (processor *Processor) deliver(ctx context.Context, selected occurrence) error {
	payload := fakeprovider.Capture{
		IdempotencyKey: selected.idempotencyKey,
		LocalDate:      selected.localDate.Format(time.DateOnly),
		Message:        "Relantern PR 0 deterministic digest: no eligible stories; live providers are disabled.",
		ScheduledFor:   selected.scheduledFor.UTC(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode occurrence %s: %w", selected.id, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, processor.deliveryURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create delivery request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", selected.idempotencyKey)
	response, err := processor.client.Do(request)
	if err != nil {
		return fmt.Errorf("send occurrence %s: %w", selected.id, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		deliveryError := fmt.Errorf("fake delivery returned status %d", response.StatusCode)
		if response.StatusCode >= http.StatusBadRequest &&
			response.StatusCode < http.StatusInternalServerError &&
			response.StatusCode != http.StatusRequestTimeout &&
			response.StatusCode != http.StatusTooEarly &&
			response.StatusCode != http.StatusTooManyRequests {
			return jobqueue.Permanent(deliveryError)
		}
		return deliveryError
	}
	return nil
}

func (processor *Processor) recordDelivery(ctx context.Context, id string) error {
	result, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'delivered', completed_at = now(), error_code = null
		where id = $1::uuid and state = 'delivering'`, id)
	if err != nil {
		return fmt.Errorf("record occurrence %s delivery: %w", id, err)
	}
	if result.RowsAffected() != 1 {
		var state string
		if queryErr := processor.pool.QueryRow(ctx, `
			select state from app.schedule_occurrences where id = $1::uuid`, id).Scan(&state); queryErr != nil {
			return fmt.Errorf("record occurrence %s delivery state: %w", id, queryErr)
		}
		if state != "delivered" {
			return fmt.Errorf("record occurrence %s delivery: unexpected state %q", id, state)
		}
	}
	return nil
}

func (processor *Processor) recordFailure(ctx context.Context, id string, cause error) error {
	errorCode := "fake_delivery_unavailable"
	if jobqueue.IsPermanent(cause) {
		errorCode = "fake_delivery_rejected"
	}
	_, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'failed', error_code = $2
		where id = $1::uuid and state = 'delivering'`, id, errorCode)
	if err != nil {
		return fmt.Errorf("record occurrence %s failure: %w", id, err)
	}
	return nil
}
