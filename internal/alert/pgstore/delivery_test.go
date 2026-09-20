package pgstore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/operability"
)

func TestCriticalAlertDeliveryLifecycle(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	var scheduledAt time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts a on a.id = delivery.alert_id
		where a.user_id = $1::uuid and delivery.channel = 'discord'`,
		fixture.userID).Scan(&deliveryID, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	if request, err := store.BeginDelivery(ctx, deliveryID, scheduledAt.Add(-time.Minute)); err != nil || request != nil {
		t.Fatalf("BeginDelivery() before quiet-hour end = %#v, %v", request, err)
	}
	first, err := store.BeginDelivery(ctx, deliveryID, scheduledAt)
	if err != nil || first == nil || first.Attempt != 1 {
		t.Fatalf("first BeginDelivery() = %#v, %v", first, err)
	}
	if _, err := store.BeginDelivery(ctx, deliveryID, scheduledAt.Add(time.Second)); err == nil {
		t.Fatal("BeginDelivery() allowed a concurrent send")
	}
	if err := store.FailDelivery(ctx, deliveryID, "provider_retryable", false, scheduledAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginDelivery(ctx, deliveryID, scheduledAt.Add(3*time.Second))
	if err != nil || second == nil || second.Attempt != 2 || second.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("retry BeginDelivery() = %#v, %v", second, err)
	}
	if err := store.CompleteDelivery(ctx, deliveryID, alert.Receipt{ProviderID: "capture:one"}, scheduledAt.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if request, err := store.BeginDelivery(ctx, deliveryID, scheduledAt.Add(5*time.Second)); err != nil || request != nil {
		t.Fatalf("BeginDelivery() after receipt = %#v, %v", request, err)
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, `select state, attempt_count from app.critical_alert_deliveries where id = $1::uuid`, deliveryID).
		Scan(&state, &attempts); err != nil || state != "sent" || attempts != 2 {
		t.Fatalf("delivery state = %q, attempts = %d, error = %v", state, attempts, err)
	}
	var recorded int
	if err := pool.QueryRow(ctx, `select count(*) from app.critical_alert_attempts where delivery_id = $1::uuid`, deliveryID).
		Scan(&recorded); err != nil || recorded != 2 {
		t.Fatalf("recorded attempts = %d, error = %v", recorded, err)
	}
}

func TestCriticalAlertDeliveryRejectsPayloadTampering(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	var scheduledAt time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts a on a.id = delivery.alert_id
		where a.user_id = $1::uuid and delivery.channel = 'discord'`,
		fixture.userID).Scan(&deliveryID, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `update app.critical_alerts set title = 'Altered after admission'
		where user_id = $1::uuid`, fixture.userID)
	_, err = store.BeginDelivery(ctx, deliveryID, scheduledAt)
	if !errors.Is(err, alert.ErrPayloadIntegrity) {
		t.Fatalf("BeginDelivery() error = %v, want payload integrity failure", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `select state from app.critical_alert_deliveries where id = $1::uuid`, deliveryID).Scan(&state); err != nil || state != "permanent" {
		t.Fatalf("tampered delivery state = %q, %v; want permanent", state, err)
	}
}

func TestExhaustedCriticalDeliveryRecoversAfterCooldown(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	var scheduledAt time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts a on a.id = delivery.alert_id
		where a.user_id = $1::uuid and delivery.channel = 'discord'`, fixture.userID).
		Scan(&deliveryID, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	first, err := store.BeginDelivery(ctx, deliveryID, scheduledAt)
	if err != nil || first == nil {
		t.Fatalf("BeginDelivery() = %+v, %v", first, err)
	}
	failedAt := scheduledAt.Add(time.Second)
	if err := store.FailDelivery(ctx, deliveryID, "provider_retry_exhausted", false, failedAt); err != nil {
		t.Fatal(err)
	}
	var state string
	var nextAttemptAt time.Time
	if err := pool.QueryRow(ctx, `
		select state, next_attempt_at from app.critical_alert_deliveries where id = $1::uuid`,
		deliveryID).Scan(&state, &nextAttemptAt); err != nil || state != "failed" ||
		!nextAttemptAt.Equal(failedAt.Add(exhaustedRetryCooldown)) {
		t.Fatalf("exhausted retry state = %q, %s, %v", state, nextAttemptAt, err)
	}
	setAlertJobInactive(t, ctx, pool, deliveryID, failedAt)
	before, err := store.ReconcileDeliveries(ctx, nextAttemptAt.Add(-time.Second))
	if err != nil || before.Requeued != 0 {
		t.Fatalf("ReconcileDeliveries() before cooldown = %+v, %v", before, err)
	}
	result, err := store.ReconcileDeliveries(ctx, nextAttemptAt)
	if err != nil || result.Requeued != 1 {
		t.Fatalf("ReconcileDeliveries() after cooldown = %+v, %v", result, err)
	}
	second, err := store.BeginDelivery(ctx, deliveryID, nextAttemptAt)
	if err != nil || second == nil || second.Attempt != 2 || second.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("recovered BeginDelivery() = %+v, %v", second, err)
	}
	if err := store.CompleteDelivery(ctx, deliveryID, alert.Receipt{ProviderID: "capture:recovered"}, nextAttemptAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestStrandedCriticalSendRecoversAndSignalsOperator(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	var scheduledAt time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts a on a.id = delivery.alert_id
		where a.user_id = $1::uuid and delivery.channel = 'discord'`, fixture.userID).
		Scan(&deliveryID, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginDelivery(ctx, deliveryID, scheduledAt); err != nil {
		t.Fatal(err)
	}
	setAlertJobInactive(t, ctx, pool, deliveryID, scheduledAt.Add(time.Minute))
	result, err := store.ReconcileDeliveries(ctx, scheduledAt.Add(3*time.Minute))
	if err != nil || result.Requeued != 1 {
		t.Fatalf("ReconcileDeliveries() stranded send = %+v, %v", result, err)
	}
	var outcome, code string
	if err := pool.QueryRow(ctx, `
		select outcome, error_code from app.critical_alert_attempts
		where delivery_id = $1::uuid and attempt_number = 1`, deliveryID).
		Scan(&outcome, &code); err != nil || outcome != "retryable" || code != "worker_interrupted" {
		t.Fatalf("interrupted attempt = %q, %q, %v", outcome, code, err)
	}
	collector, err := operability.NewCollector(pool)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := collector.Collect(ctx, scheduledAt.Add(11*time.Minute))
	if err != nil || snapshot.UndeliveredCritical < 1 {
		t.Fatalf("undelivered critical snapshot = %+v, %v", snapshot, err)
	}
	alerted := false
	for _, signal := range operability.Evaluate(snapshot, scheduledAt.Add(11*time.Minute)) {
		if signal.Name == "critical_advisory_undelivered" && signal.Severity == "critical" {
			alerted = true
		}
	}
	if !alerted {
		t.Fatal("overdue critical delivery has no operator signal")
	}
}

func TestMissingPendingCriticalJobIsReenqueuedOnlyAfterDue(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	var deliveryID string
	var scheduledAt time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts a on a.id = delivery.alert_id
		where a.user_id = $1::uuid and delivery.channel = 'discord'`, fixture.userID).
		Scan(&deliveryID, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	active, err := store.ReconcileDeliveries(ctx, scheduledAt)
	if err != nil || active.Requeued != 0 {
		t.Fatalf("active pending job was duplicated: %+v, %v", active, err)
	}
	setAlertJobInactive(t, ctx, pool, deliveryID, scheduledAt.Add(-time.Minute))
	before, err := store.ReconcileDeliveries(ctx, scheduledAt.Add(-time.Second))
	if err != nil || before.Requeued != 0 {
		t.Fatalf("quiet-hour pending job was sent early: %+v, %v", before, err)
	}
	after, err := store.ReconcileDeliveries(ctx, scheduledAt)
	if err != nil || after.Requeued != 1 {
		t.Fatalf("due pending job was not recovered: %+v, %v", after, err)
	}
}

func setAlertJobInactive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deliveryID string, now time.Time) {
	t.Helper()
	result, err := pool.Exec(ctx, `
		update river.river_job set state = 'cancelled', finalized_at = $2
		where kind = 'deliver_critical_alert' and queue = 'test_alert_assessment'
			and args->>'deliveryId' = $1 and state in ('available', 'pending', 'retryable', 'running', 'scheduled')`,
		deliveryID, now)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("cancel simulated exhausted River job: rows=%d error=%v", result.RowsAffected(), err)
	}
}
