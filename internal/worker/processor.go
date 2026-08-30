package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/radar"
)

var errOccurrenceNotFound = errors.New("schedule occurrence not found")

type Processor struct {
	pool   *pgxpool.Pool
	digest scheduledDigest
	radar  scheduledRadar
}

type scheduledDigest interface {
	QueueOccurrence(context.Context, string) error
}

type scheduledRadar interface {
	QueueDiscovery(context.Context, radar.QueueDiscoveryRequest, time.Time) (radar.DiscoveryRun, error)
}

type ProcessorOption func(*Processor)

func WithRadarScheduleHandoff(store scheduledRadar) ProcessorOption {
	return func(processor *Processor) {
		processor.radar = store
	}
}

func WithDigestScheduleHandoff(store scheduledDigest) ProcessorOption {
	return func(processor *Processor) {
		processor.digest = store
	}
}

type occurrence struct {
	id             string
	idempotencyKey string
	localDate      time.Time
	scheduledFor   time.Time
	scheduleType   string
	userID         string
}

func NewProcessor(
	pool *pgxpool.Pool,
	_ string,
	_ time.Duration,
	options ...ProcessorOption,
) *Processor {
	processor := &Processor{pool: pool}
	for _, option := range options {
		option(processor)
	}
	return processor
}

func (processor *Processor) Process(ctx context.Context, occurrenceID string) error {
	if _, err := uuid.Parse(occurrenceID); err != nil {
		return jobqueue.Permanent(fmt.Errorf("invalid occurrence ID: %w", err))
	}

	selected, err := processor.lookup(ctx, occurrenceID)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}
	if selected.scheduleType == "daily_digest" {
		if processor.digest == nil {
			configurationErr := jobqueue.Permanent(errors.New("daily digest schedule handoff is not configured"))
			if updateErr := processor.recordFailure(ctx, selected.id, "digest_handoff_unconfigured"); updateErr != nil {
				return fmt.Errorf("daily digest handoff: %w; record failure: %v", configurationErr, updateErr)
			}
			return configurationErr
		}
		if queueErr := processor.digest.QueueOccurrence(ctx, selected.id); queueErr != nil {
			if errors.Is(queueErr, digest.ErrInvalid) || errors.Is(queueErr, digest.ErrNotFound) {
				queueErr = jobqueue.Permanent(queueErr)
			}
			if updateErr := processor.recordFailure(ctx, selected.id, "digest_handoff_unavailable"); updateErr != nil {
				return fmt.Errorf("queue daily digest: %w; record failure: %v", queueErr, updateErr)
			}
			return queueErr
		}
		return nil
	}
	selected, err = processor.claim(ctx, occurrenceID)
	if err != nil || selected == nil {
		return err
	}
	if selected.scheduleType == "weekly_radar" {
		if processor.radar == nil {
			configurationErr := jobqueue.Permanent(errors.New("weekly Radar schedule handoff is not configured"))
			if updateErr := processor.recordFailure(ctx, selected.id, "radar_handoff_unconfigured"); updateErr != nil {
				return fmt.Errorf("weekly Radar handoff: %w; record failure: %v", configurationErr, updateErr)
			}
			return configurationErr
		}
		run, queueErr := processor.radar.QueueDiscovery(ctx, radar.QueueDiscoveryRequest{
			UserID: selected.userID, TriggerType: "scheduled",
			IdempotencyKey: "weekly-radar:" + selected.idempotencyKey,
		}, selected.scheduledFor.UTC())
		if queueErr != nil {
			if updateErr := processor.recordFailure(ctx, selected.id, "radar_discovery_unavailable"); updateErr != nil {
				return fmt.Errorf("queue Radar discovery: %w; record failure: %v", queueErr, updateErr)
			}
			return queueErr
		}
		return processor.recordRadarHandoff(ctx, selected.id, run.ID)
	}
	if selected.scheduleType == "maintenance" {
		return processor.recordDelivery(ctx, selected.id)
	}
	unsupportedErr := jobqueue.Permanent(fmt.Errorf("unsupported schedule type %q", selected.scheduleType))
	if updateErr := processor.recordFailure(ctx, selected.id, "schedule_type_unsupported"); updateErr != nil {
		return fmt.Errorf("unsupported schedule: %w; record failure: %v", unsupportedErr, updateErr)
	}
	return unsupportedErr
}

func (processor *Processor) lookup(ctx context.Context, occurrenceID string) (*occurrence, error) {
	var selected occurrence
	var state string
	err := processor.pool.QueryRow(ctx, `
		select occurrence.id::text, occurrence.idempotency_key, occurrence.local_date,
			occurrence.scheduled_for, occurrence.state, schedule.schedule_type,
			schedule.user_id::text
		from app.schedule_occurrences occurrence
		join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
		where occurrence.id = $1::uuid`, occurrenceID).Scan(
		&selected.id, &selected.idempotencyKey, &selected.localDate, &selected.scheduledFor,
		&state, &selected.scheduleType, &selected.userID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, jobqueue.Permanent(errOccurrenceNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("select occurrence: %w", err)
	}
	if state == "delivered" || state == "skipped" || state == "missed" {
		return nil, nil
	}
	return &selected, nil
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
		select occurrence.id::text, occurrence.idempotency_key, occurrence.local_date,
			occurrence.scheduled_for, occurrence.state, schedule.schedule_type,
			schedule.user_id::text
		from app.schedule_occurrences occurrence
		join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
		where occurrence.id = $1::uuid
		for update`, occurrenceID).Scan(
		&selected.id,
		&selected.idempotencyKey,
		&selected.localDate,
		&selected.scheduledFor,
		&state,
		&selected.scheduleType,
		&selected.userID,
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

func (processor *Processor) recordRadarHandoff(ctx context.Context, id string, runID string) error {
	result, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'delivered', completed_at = now(), error_code = null,
			metadata = jsonb_set(metadata, '{radarDiscoveryRunId}', to_jsonb($2::text), true)
		where id = $1::uuid and state = 'delivering'`, id, runID)
	if err != nil {
		return fmt.Errorf("record occurrence %s Radar handoff: %w", id, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("record occurrence %s Radar handoff: state changed", id)
	}
	return nil
}

func (processor *Processor) recordFailure(ctx context.Context, id string, errorCode string) error {
	_, err := processor.pool.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'failed', error_code = $2
		where id = $1::uuid and state not in ('delivered', 'skipped', 'missed')`, id, errorCode)
	if err != nil {
		return fmt.Errorf("record occurrence %s failure: %w", id, err)
	}
	return nil
}
