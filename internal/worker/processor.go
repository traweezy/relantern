package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/fakeprovider"
)

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

func (processor *Processor) DeliverDue(ctx context.Context) (int64, error) {
	var delivered int64
	for {
		selected, err := processor.claimNext(ctx)
		if err != nil {
			return delivered, err
		}
		if selected == nil {
			return delivered, nil
		}
		if err := processor.deliver(ctx, *selected); err != nil {
			if updateErr := processor.recordFailure(ctx, selected.id); updateErr != nil {
				return delivered, fmt.Errorf("deliver occurrence: %w; record failure: %v", err, updateErr)
			}
			return delivered, err
		}
		if err := processor.recordDelivery(ctx, selected.id); err != nil {
			return delivered, err
		}
		delivered++
	}
}

func (processor *Processor) claimNext(ctx context.Context) (*occurrence, error) {
	tx, err := processor.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin occurrence claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var selected occurrence
	err = tx.QueryRow(ctx, `
		select id::text, idempotency_key, local_date, scheduled_for
		from app.schedule_occurrences
		where state in ('due', 'failed')
		order by scheduled_for, id
		for update skip locked
		limit 1`).Scan(
		&selected.id,
		&selected.idempotencyKey,
		&selected.localDate,
		&selected.scheduledFor,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("select occurrence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'preparing', started_at = coalesce(started_at, now()), error_code = null
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
		return fmt.Errorf("fake delivery returned status %d", response.StatusCode)
	}
	return nil
}

func (processor *Processor) recordDelivery(ctx context.Context, id string) error {
	result, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'delivered', completed_at = now(), error_code = null
		where id = $1::uuid and state = 'preparing'`, id)
	if err != nil {
		return fmt.Errorf("record occurrence %s delivery: %w", id, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record occurrence %s delivery: unexpected state", id)
	}
	return nil
}

func (processor *Processor) recordFailure(ctx context.Context, id string) error {
	_, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'failed', error_code = 'fake_delivery_unavailable'
		where id = $1::uuid and state = 'preparing'`, id)
	if err != nil {
		return fmt.Errorf("record occurrence %s failure: %w", id, err)
	}
	return nil
}
