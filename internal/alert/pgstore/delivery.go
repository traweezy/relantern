package pgstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/jobqueue"
)

const (
	criticalDeliveryRecoveryPageSize = 128
	exhaustedRetryCooldown           = 5 * time.Minute
)

// BeginDelivery claims one durable channel delivery. Retried River jobs keep
// the same provider idempotency key and cannot send after a completed receipt.
func (store *Store) BeginDelivery(ctx context.Context, deliveryID string, now time.Time) (*alert.Delivery, error) {
	if _, err := uuid.Parse(deliveryID); err != nil || now.IsZero() {
		return nil, errors.New("critical alert delivery requires an ID and time")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin critical alert delivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var request alert.Delivery
	var state string
	var nextAttemptAt time.Time
	var lastAttemptAt *time.Time
	var attemptCount int
	var expectedSHA []byte
	err = tx.QueryRow(ctx, `
		select alert.id::text, delivery.channel, delivery.idempotency_key,
			alert.title, alert.source_url, alert.package_name,
			alert.ecosystem, alert.current_version, alert.vulnerable_range,
			alert.patched_version, delivery.state, delivery.next_attempt_at,
			delivery.attempt_count, delivery.payload_sha256,
			delivery.last_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts alert on alert.id = delivery.alert_id
		where delivery.id = $1::uuid
		for update of delivery`, deliveryID).Scan(
		&request.AlertID, &request.Channel, &request.IdempotencyKey,
		&request.Title, &request.SourceURL, &request.PackageName,
		&request.Ecosystem, &request.CurrentVersion, &request.VersionRange,
		&request.PatchedVersion, &state, &nextAttemptAt, &attemptCount, &expectedSHA,
		&lastAttemptAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock critical alert delivery: %w", err)
	}
	if state == "sent" || state == "permanent" || now.Before(nextAttemptAt) {
		return nil, nil
	}
	if state == "sending" && lastAttemptAt != nil && now.Sub(*lastAttemptAt) < 2*time.Minute {
		return nil, errors.New("critical alert delivery is already in flight")
	}
	hash := alert.DeliverySHA256(request)
	if !bytes.Equal(hash[:], expectedSHA) {
		if _, err := tx.Exec(ctx, `
			update app.critical_alert_deliveries
			set state = 'permanent', updated_at = $2
			where id = $1::uuid`, deliveryID, now.UTC()); err != nil {
			return nil, fmt.Errorf("quarantine changed critical alert payload: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit critical alert payload quarantine: %w", err)
		}
		return nil, alert.ErrPayloadIntegrity
	}
	request.Attempt = attemptCount + 1
	if _, err := tx.Exec(ctx, `
		update app.critical_alert_deliveries
		set state = 'sending', attempt_count = $2, last_attempt_at = $3,
			updated_at = $3
		where id = $1::uuid`, deliveryID, request.Attempt, now.UTC()); err != nil {
		return nil, fmt.Errorf("claim critical alert delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit critical alert claim: %w", err)
	}
	return &request, nil
}

func (store *Store) CompleteDelivery(ctx context.Context, deliveryID string, receipt alert.Receipt, now time.Time) error {
	if _, err := uuid.Parse(deliveryID); err != nil || receipt.ProviderID == "" || len(receipt.ProviderID) > 255 || now.IsZero() {
		return errors.New("critical alert receipt is invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin critical alert completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	var attemptCount int
	var attemptedAt time.Time
	err = tx.QueryRow(ctx, `
		select state, attempt_count, last_attempt_at
		from app.critical_alert_deliveries where id = $1::uuid for update`, deliveryID).
		Scan(&state, &attemptCount, &attemptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock completed critical alert: %w", err)
	}
	if state == "sent" {
		return nil
	}
	if state != "sending" || now.Before(attemptedAt) {
		return errors.New("critical alert delivery is not sending")
	}
	if _, err := tx.Exec(ctx, `
		insert into app.critical_alert_attempts
			(delivery_id, attempt_number, outcome, provider_id, attempted_at, completed_at)
		values ($1::uuid, $2, 'sent', $3, $4, $5)
		on conflict (delivery_id, attempt_number) do nothing`,
		deliveryID, attemptCount, receipt.ProviderID, attemptedAt, now.UTC()); err != nil {
		return fmt.Errorf("record critical alert success: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update app.critical_alert_deliveries
		set state = 'sent', provider_id = $2, delivered_at = $3, updated_at = $3
		where id = $1::uuid`, deliveryID, receipt.ProviderID, now.UTC()); err != nil {
		return fmt.Errorf("complete critical alert delivery: %w", err)
	}
	return tx.Commit(ctx)
}

func (store *Store) FailDelivery(ctx context.Context, deliveryID string, code string, permanent bool, now time.Time) error {
	if _, err := uuid.Parse(deliveryID); err != nil || code == "" || len(code) > 100 || now.IsZero() {
		return errors.New("critical alert failure is invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin critical alert failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	var attemptCount int
	var attemptedAt time.Time
	err = tx.QueryRow(ctx, `
		select state, attempt_count, last_attempt_at
		from app.critical_alert_deliveries where id = $1::uuid for update`, deliveryID).
		Scan(&state, &attemptCount, &attemptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock failed critical alert: %w", err)
	}
	if state != "sending" || now.Before(attemptedAt) {
		return errors.New("critical alert delivery is not sending")
	}
	state = "failed"
	outcome := "retryable"
	if permanent {
		state = "permanent"
		outcome = "permanent"
	}
	nextAttemptAt := now.UTC()
	if !permanent && code == "provider_retry_exhausted" {
		nextAttemptAt = nextAttemptAt.Add(exhaustedRetryCooldown)
	}
	if _, err := tx.Exec(ctx, `
		insert into app.critical_alert_attempts
			(delivery_id, attempt_number, outcome, error_code, attempted_at, completed_at)
		values ($1::uuid, $2, $3, $4, $5, $6)
		on conflict (delivery_id, attempt_number) do nothing`,
		deliveryID, attemptCount, outcome, code, attemptedAt, now.UTC()); err != nil {
		return fmt.Errorf("record critical alert failure: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update app.critical_alert_deliveries
		set state = $2, next_attempt_at = $3, updated_at = $4
		where id = $1::uuid`, deliveryID, state, nextAttemptAt, now.UTC()); err != nil {
		return fmt.Errorf("mark critical alert delivery failed: %w", err)
	}
	return tx.Commit(ctx)
}

// ReconcileDeliveries restores River jobs that exhausted their retry budget or
// disappeared while a send was in flight. The ledger stays the source of truth;
// active River jobs are excluded and the page is locked before any enqueue.
func (store *Store) ReconcileDeliveries(ctx context.Context, now time.Time) (alert.ReconcileResult, error) {
	if now.IsZero() {
		return alert.ReconcileResult{}, errors.New("critical alert reconciliation requires a time")
	}
	now = now.UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return alert.ReconcileResult{}, fmt.Errorf("begin critical alert reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select delivery.id::text, delivery.state, delivery.attempt_count,
			delivery.last_attempt_at
		from app.critical_alert_deliveries delivery
		where ((delivery.state in ('pending', 'failed') and delivery.next_attempt_at <= $1)
			or (delivery.state = 'sending'
				and delivery.last_attempt_at <= $1 - interval '2 minutes'))
			and not exists (
				select 1 from river.river_job job
				where job.kind = 'deliver_critical_alert'
					and job.args->>'deliveryId' = delivery.id::text
					and job.state in ('available', 'pending', 'retryable', 'running', 'scheduled')
			)
		order by delivery.next_attempt_at, delivery.id
		limit $2
		for update of delivery skip locked`, now, criticalDeliveryRecoveryPageSize)
	if err != nil {
		return alert.ReconcileResult{}, fmt.Errorf("select stranded critical deliveries: %w", err)
	}
	type strandedDelivery struct {
		id            string
		state         string
		attemptCount  int
		lastAttemptAt *time.Time
	}
	stranded := make([]strandedDelivery, 0, criticalDeliveryRecoveryPageSize)
	for rows.Next() {
		var candidate strandedDelivery
		if err := rows.Scan(&candidate.id, &candidate.state, &candidate.attemptCount, &candidate.lastAttemptAt); err != nil {
			rows.Close()
			return alert.ReconcileResult{}, fmt.Errorf("scan stranded critical delivery: %w", err)
		}
		stranded = append(stranded, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return alert.ReconcileResult{}, fmt.Errorf("iterate stranded critical deliveries: %w", err)
	}
	rows.Close()
	result := alert.ReconcileResult{}
	for _, candidate := range stranded {
		if candidate.state == "sending" {
			if candidate.lastAttemptAt == nil {
				return alert.ReconcileResult{}, errors.New("stranded critical delivery lacks attempt time")
			}
			if _, err := tx.Exec(ctx, `
				insert into app.critical_alert_attempts
					(delivery_id, attempt_number, outcome, error_code, attempted_at, completed_at)
				values ($1::uuid, $2, 'retryable', 'worker_interrupted', $3, $4)
				on conflict (delivery_id, attempt_number) do nothing`,
				candidate.id, candidate.attemptCount, *candidate.lastAttemptAt, now); err != nil {
				return alert.ReconcileResult{}, fmt.Errorf("record interrupted critical send: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				update app.critical_alert_deliveries
				set state = 'failed', next_attempt_at = $2, updated_at = $2
				where id = $1::uuid`, candidate.id, now); err != nil {
				return alert.ReconcileResult{}, fmt.Errorf("release stranded critical send: %w", err)
			}
		}
		if _, inserted, err := store.jobs.EnqueueDeliverCriticalAlert(ctx, tx,
			jobqueue.DeliverCriticalAlertArgs{DeliveryID: candidate.id}, now); err != nil {
			return alert.ReconcileResult{}, err
		} else if inserted {
			result.Requeued++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return alert.ReconcileResult{}, fmt.Errorf("commit critical alert reconciliation: %w", err)
	}
	if err := store.pool.QueryRow(ctx, `
		select count(*)::bigint
		from app.critical_alert_deliveries delivery
		where delivery.state <> 'sent' and (
			delivery.state = 'permanent' or
			coalesce((select min(attempted_at) from app.critical_alert_attempts attempt
				where attempt.delivery_id = delivery.id),
				delivery.last_attempt_at, delivery.next_attempt_at) <= $1::timestamptz - interval '10 minutes'
		)`, now).Scan(&result.Overdue); err != nil {
		return result, fmt.Errorf("count undelivered critical alerts: %w", err)
	}
	return result, nil
}
