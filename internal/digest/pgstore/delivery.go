package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
)

func (store *Store) BeginDelivery(ctx context.Context, digestID string, now time.Time) (*digest.Delivery, error) {
	return retrySerializableValue(ctx, func() (*digest.Delivery, error) {
		return store.beginDelivery(ctx, digestID, now)
	})
}

func (store *Store) beginDelivery(ctx context.Context, digestID string, now time.Time) (*digest.Delivery, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin digest delivery: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var request digest.Delivery
	var state string
	var encoded []byte
	err = transaction.QueryRow(ctx, `
		select current_digest.id::text, current_digest.schedule_occurrence_id::text,
			current_digest.channel, current_digest.state, current_digest.provider_idempotency_key,
			current_digest.local_digest_date::text, occurrence.scheduled_for,
			current_digest.rendered_payload, current_digest.payload_sha256
		from app.digests current_digest
		join app.schedule_occurrences occurrence on occurrence.id = current_digest.schedule_occurrence_id
		where current_digest.id = $1::uuid
		for update of current_digest`, digestID).Scan(
		&request.DigestID, &request.OccurrenceID, &request.Channel, &state,
		&request.IdempotencyKey, &request.LocalDate, &request.ScheduledFor,
		&encoded, &request.PayloadSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, digest.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select immutable digest delivery: %w", err)
	}
	if request.Channel == digest.ChannelDashboard {
		return nil, fmt.Errorf("%w: dashboard digests are not externally delivered", digest.ErrInvalid)
	}
	if state == "delivered" || state == "skipped" {
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit terminal digest delivery: %w", err)
		}
		return nil, nil
	}
	if err := json.Unmarshal(encoded, &request.Payload); err != nil {
		return nil, fmt.Errorf("decode immutable digest delivery: %w", err)
	}
	if err := transaction.QueryRow(ctx, `
		update app.delivery_attempts
		set state = 'delivering', attempt_count = attempt_count + 1,
			attempted_at = $2, completed_at = null, error_code = null, updated_at = $2
		where digest_id = $1::uuid and channel = $3
		returning attempt_count`, digestID, now.UTC(), request.Channel).Scan(&request.Attempt); err != nil {
		return nil, fmt.Errorf("claim digest delivery attempt: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.digests set state = 'delivering', completed_at = null, error_code = null
		where id = $1::uuid`, digestID); err != nil {
		return nil, fmt.Errorf("mark digest delivering: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences set state = 'delivering', completed_at = null, error_code = null
		where id = $1::uuid`, request.OccurrenceID); err != nil {
		return nil, fmt.Errorf("mark digest occurrence delivering: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit digest delivery claim: %w", err)
	}
	return &request, nil
}

func (store *Store) CompleteDelivery(
	ctx context.Context,
	digestID string,
	receipt digest.Receipt,
	now time.Time,
) error {
	providerID := strings.TrimSpace(receipt.ProviderID)
	if providerID == "" || len(providerID) > 255 {
		return fmt.Errorf("%w: provider receipt ID is invalid", digest.ErrInvalid)
	}
	return retrySerializable(ctx, func() error {
		return store.completeDelivery(ctx, digestID, providerID, now)
	})
}

func (store *Store) completeDelivery(
	ctx context.Context,
	digestID string,
	providerID string,
	now time.Time,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin digest delivery completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var occurrenceID, channel, state string
	if err := transaction.QueryRow(ctx, `
		select schedule_occurrence_id::text, channel, state
		from app.digests where id = $1::uuid for update`, digestID).Scan(&occurrenceID, &channel, &state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digest.ErrNotFound
		}
		return fmt.Errorf("select completing digest: %w", err)
	}
	if state == "delivered" {
		return transaction.Commit(ctx)
	}
	if _, err := transaction.Exec(ctx, `
		update app.delivery_attempts
		set provider_id = $2, state = 'delivered', completed_at = $3,
			error_code = null, updated_at = $3
		where digest_id = $1::uuid and channel = $4`, digestID, providerID, now.UTC(), channel); err != nil {
		return fmt.Errorf("complete digest delivery attempt: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.digests
		set state = 'delivered', completed_at = $2, error_code = null
		where id = $1::uuid`, digestID, now.UTC()); err != nil {
		return fmt.Errorf("complete digest: %w", err)
	}
	var incomplete int
	if err := transaction.QueryRow(ctx, `
		select count(*)::integer from app.digests
		where schedule_occurrence_id = $1::uuid and channel <> 'dashboard'
			and state <> 'delivered'`, occurrenceID).Scan(&incomplete); err != nil {
		return fmt.Errorf("count incomplete digest channels: %w", err)
	}
	if incomplete == 0 {
		if _, err := transaction.Exec(ctx, `
			update app.schedule_occurrences
			set state = 'delivered', completed_at = $2, error_code = null
			where id = $1::uuid`, occurrenceID, now.UTC()); err != nil {
			return fmt.Errorf("complete digest occurrence: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit digest delivery completion: %w", err)
	}
	return nil
}

func (store *Store) FailDelivery(
	ctx context.Context,
	digestID string,
	errorCode string,
	now time.Time,
) error {
	errorCode = boundedErrorCode(errorCode)
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin digest delivery failure: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var occurrenceID, channel string
	if err := transaction.QueryRow(ctx, `
		select schedule_occurrence_id::text, channel
		from app.digests where id = $1::uuid for update`, digestID).Scan(&occurrenceID, &channel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digest.ErrNotFound
		}
		return fmt.Errorf("select failed digest: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.delivery_attempts
		set state = 'failed', completed_at = null, error_code = $2, updated_at = $3
		where digest_id = $1::uuid and channel = $4`, digestID, errorCode, now.UTC(), channel); err != nil {
		return fmt.Errorf("record failed digest delivery attempt: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.digests set state = 'failed', completed_at = null, error_code = $2
		where id = $1::uuid`, digestID, errorCode); err != nil {
		return fmt.Errorf("record failed digest: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences set state = 'failed', completed_at = null, error_code = $2
		where id = $1::uuid`, occurrenceID, errorCode); err != nil {
		return fmt.Errorf("record failed digest occurrence: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit digest delivery failure: %w", err)
	}
	return nil
}

func boundedErrorCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "delivery_failed"
	}
	if len(value) > 120 {
		return value[:120]
	}
	return value
}
