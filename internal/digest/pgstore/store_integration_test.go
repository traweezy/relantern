package pgstore_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/delivery"
	"github.com/traweezy/relantern/internal/digest"
	digeststore "github.com/traweezy/relantern/internal/digest/pgstore"
	"github.com/traweezy/relantern/internal/fakeprovider"
	intelligencestore "github.com/traweezy/relantern/internal/intelligence/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestDigestWorkflowFreezesAndRetriesOneIdempotentPayload(t *testing.T) {
	pool := openDigestDatabase(t)
	now := time.Date(2099, time.August, 29, 12, 0, 0, 0, time.UTC)
	userID, occurrenceID := insertDigestOccurrence(t, pool, now)
	cleanupDigestFixture(t, pool, userID, occurrenceID)
	jobs, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatal(err)
	}
	store, err := digeststore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.QueueOccurrence(context.Background(), occurrenceID); err != nil {
		t.Fatalf("QueueOccurrence() error = %v", err)
	}
	if err := store.QueueOccurrence(context.Background(), occurrenceID); err != nil {
		t.Fatalf("replayed QueueOccurrence() error = %v", err)
	}
	if err := store.Preflight(context.Background(), occurrenceID, now); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if err := store.Prepare(context.Background(), occurrenceID, now.Add(time.Minute)); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := store.Finalize(context.Background(), occurrenceID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := store.Finalize(context.Background(), occurrenceID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("replayed Finalize() error = %v", err)
	}

	snapshot, err := store.Snapshot(context.Background(), userID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Digests) != 2 {
		t.Fatalf("digest count = %d, want dashboard and Discord", len(snapshot.Digests))
	}
	var external digest.DigestRecord
	for _, current := range snapshot.Digests {
		if current.Channel == digest.ChannelDiscord {
			external = current
		}
	}
	if external.ID == "" || external.State != "ready" || len(external.Items) != 0 ||
		external.ExecutiveSummary != "No material, evidence-backed changes met this digest's reviewed threshold." {
		t.Fatalf("external digest = %+v", external)
	}

	handler, err := fakeprovider.New(fakeprovider.KindDelivery, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	sender, err := delivery.New(delivery.Config{
		Mode: "log", CaptureURL: server.URL + "/capture", RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.BeginDelivery(context.Background(), external.ID, now.Add(4*time.Minute))
	if err != nil || first == nil {
		t.Fatalf("BeginDelivery() = %+v, %v", first, err)
	}
	receipt, err := sender.Send(context.Background(), *first)
	if err != nil {
		t.Fatalf("first Send() error = %v", err)
	}
	if err := store.FailDelivery(context.Background(), external.ID, "fixture_timeout", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("FailDelivery() error = %v", err)
	}
	retried, err := store.Retry(context.Background(), userID, external.ID, now.Add(6*time.Minute))
	if err != nil || retried.State != "ready" {
		t.Fatalf("Retry() = %+v, %v", retried, err)
	}
	second, err := store.BeginDelivery(context.Background(), external.ID, now.Add(7*time.Minute))
	if err != nil || second == nil {
		t.Fatalf("second BeginDelivery() = %+v, %v", second, err)
	}
	if second.Attempt != 2 || second.IdempotencyKey != first.IdempotencyKey ||
		hex.EncodeToString(second.PayloadSHA256) != hex.EncodeToString(first.PayloadSHA256) {
		t.Fatalf("immutable retry changed: first=%+v second=%+v", first, second)
	}
	secondReceipt, err := sender.Send(context.Background(), *second)
	if err != nil {
		t.Fatalf("second Send() error = %v", err)
	}
	if secondReceipt.ProviderID != receipt.ProviderID {
		t.Fatalf("provider IDs changed: %q != %q", secondReceipt.ProviderID, receipt.ProviderID)
	}
	if err := store.CompleteDelivery(context.Background(), external.ID, secondReceipt, now.Add(8*time.Minute)); err != nil {
		t.Fatalf("CompleteDelivery() error = %v", err)
	}
	if terminal, err := store.BeginDelivery(context.Background(), external.ID, now.Add(9*time.Minute)); err != nil || terminal != nil {
		t.Fatalf("terminal BeginDelivery() = %+v, %v", terminal, err)
	}

	response, err := http.Get(server.URL + "/captures")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var captures struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&captures); err != nil {
		t.Fatal(err)
	}
	if captures.Count != 1 {
		t.Fatalf("provider captures = %d, want one idempotent delivery", captures.Count)
	}
	completed, err := store.Get(context.Background(), userID, external.ID)
	if err != nil || completed.State != "delivered" || completed.AttemptCount != 2 {
		t.Fatalf("completed digest = %+v, %v", completed, err)
	}
	var occurrenceState string
	if err := pool.QueryRow(context.Background(), `
		select state from app.schedule_occurrences where id = $1::uuid`, occurrenceID).Scan(&occurrenceState); err != nil {
		t.Fatal(err)
	}
	if occurrenceState != "delivered" {
		t.Fatalf("occurrence state = %q, want delivered", occurrenceState)
	}
	intelligence, err := intelligencestore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	today, err := intelligence.Today(context.Background(), userID, now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Today() error = %v", err)
	}
	if today.DeliveryState != "delivered" || !today.CoverageStartAt.Equal(external.WindowStart) ||
		!today.CoverageEndAt.Equal(external.WindowEnd) || len(today.Stories) != 0 {
		t.Fatalf("persisted Today snapshot = %+v", today)
	}
}

func TestDigestPreviewDoesNotPersistAndRunNowDefaultsToDashboardOnly(t *testing.T) {
	pool := openDigestDatabase(t)
	now := time.Date(2098, time.August, 29, 12, 0, 0, 0, time.UTC)
	userID, occurrenceID := insertDigestOccurrence(t, pool, now)
	cleanupDigestFixture(t, pool, userID, occurrenceID)
	jobs, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatal(err)
	}
	store, err := digeststore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}
	var scheduleID string
	if err := pool.QueryRow(context.Background(), `
		update app.schedule_occurrences
		set trigger_type = 'run_now', metadata = '{"externalDelivery":false}'::jsonb
		where id = $1::uuid
		returning schedule_id::text`, occurrenceID).Scan(&scheduleID); err != nil {
		t.Fatal(err)
	}
	preview, err := store.Preview(context.Background(), userID, scheduleID, now)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if preview.ScheduleID != scheduleID || preview.Rendered.Channel != digest.ChannelDashboard ||
		len(preview.Channels) != 2 {
		t.Fatalf("Preview() = %+v", preview)
	}
	var before int
	if err := pool.QueryRow(context.Background(), `
		select count(*)::integer from app.digests where user_id = $1::uuid`, userID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("preview persisted %d digests", before)
	}
	if err := store.QueueOccurrence(context.Background(), occurrenceID); err != nil {
		t.Fatal(err)
	}
	if err := store.Preflight(context.Background(), occurrenceID, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Prepare(context.Background(), occurrenceID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Finalize(context.Background(), occurrenceID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(context.Background(), userID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Digests) != 1 || snapshot.Digests[0].Channel != digest.ChannelDashboard ||
		snapshot.Digests[0].State != "delivered" {
		t.Fatalf("run-now preview digests = %+v", snapshot.Digests)
	}
}

func openDigestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for digest integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertDigestOccurrence(t *testing.T, pool *pgxpool.Pool, now time.Time) (string, string) {
	t.Helper()
	suffix := time.Now().UnixNano()
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		) values ($1, $2, 'Digest integration', 'America/New_York', $3, true)
		returning id::text`, suffix, fmt.Sprintf("digest-%d", suffix),
		fmt.Sprintf("digest-%d@tests.relantern.local", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var scheduleID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.schedule_definitions (
			user_id, schedule_type, timezone, local_time, days_of_week, enabled,
			catchup_policy, catchup_grace, next_due_at, empty_behavior, channels
		) values ($1::uuid, 'daily_digest', 'America/New_York', '08:00',
			array[1,2,3,4,5,6,7]::smallint[], true, 'catch_up', interval '6 hours',
			$2::timestamptz + interval '1 day', 'all_clear', array['dashboard','discord']::text[])
		returning id::text`, userID, now.UTC()).Scan(&scheduleID); err != nil {
		t.Fatal(err)
	}
	var occurrenceID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.schedule_occurrences (
			schedule_id, scheduled_for, local_date, local_offset_seconds,
			state, trigger_type, idempotency_key
		) values ($1::uuid, $2::timestamptz, $2::timestamptz::date, 0, 'enqueued', 'scheduled', $3)
		returning id::text`, scheduleID, now.UTC(), "digest-integration:"+scheduleID).Scan(&occurrenceID); err != nil {
		t.Fatal(err)
	}
	return userID, occurrenceID
}

func cleanupDigestFixture(t *testing.T, pool *pgxpool.Pool, userID string, occurrenceID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from river.river_job where args->>'occurrenceId' = $1`, occurrenceID)
		_, _ = pool.Exec(ctx, `delete from river.river_job where args->>'digestId' in (
			select id::text from app.digests where user_id = $1::uuid
		)`, userID)
		_, _ = pool.Exec(ctx, `delete from app.audit_events where actor_id = $1`, userID)
		_, _ = pool.Exec(ctx, `delete from app.outbox_events where aggregate_id = $1::uuid`, userID)
		_, _ = pool.Exec(ctx, `delete from app.users where id = $1::uuid`, userID)
	})
}
