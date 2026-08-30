package worker_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	digeststore "github.com/traweezy/relantern/internal/digest/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
	radarstore "github.com/traweezy/relantern/internal/radar/pgstore"
	"github.com/traweezy/relantern/internal/worker"
)

func TestProcessorMakesDailyDigestHandoffIdempotent(t *testing.T) {
	pool := openWorkerIntegrationPool(t)
	userID, occurrenceID := insertWorkerOccurrence(t, pool, "enqueued")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river.river_job where args->>'occurrenceId' = $1`, occurrenceID)
		_, _ = pool.Exec(context.Background(), "delete from app.users where id = $1::uuid", userID)
	})
	digests := newDigestHandoff(t, pool)
	processor := worker.NewProcessor(pool, "", 2*time.Second, worker.WithDigestScheduleHandoff(digests))
	if err := processor.Process(context.Background(), occurrenceID); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if err := processor.Process(context.Background(), occurrenceID); err != nil {
		t.Fatalf("second Process() error = %v", err)
	}

	var state string
	if err := pool.QueryRow(context.Background(), `
		select state from app.schedule_occurrences where id = $1::uuid`, occurrenceID).Scan(&state); err != nil {
		t.Fatalf("inspect delivered occurrence: %v", err)
	}
	if state != "preparing" {
		t.Fatalf("occurrence state = %q, want preparing", state)
	}
	var jobs int
	if err := pool.QueryRow(context.Background(), `
		select count(*)::integer from river.river_job
		where kind = 'preflight_digest_sources' and args->>'occurrenceId' = $1`, occurrenceID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("preflight jobs = %d, want 1", jobs)
	}
}

func TestProcessorResumesDailyDigestHandoffAfterRestart(t *testing.T) {
	pool := openWorkerIntegrationPool(t)
	userID, occurrenceID := insertWorkerOccurrence(t, pool, "delivering")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river.river_job where args->>'occurrenceId' = $1`, occurrenceID)
		_, _ = pool.Exec(context.Background(), "delete from app.users where id = $1::uuid", userID)
	})
	processor := worker.NewProcessor(pool, "", 2*time.Second, worker.WithDigestScheduleHandoff(newDigestHandoff(t, pool)))
	if err := processor.Process(context.Background(), occurrenceID); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	var state string
	if err := pool.QueryRow(context.Background(), `
		select state from app.schedule_occurrences where id = $1::uuid`, occurrenceID).Scan(&state); err != nil {
		t.Fatalf("inspect resumed occurrence: %v", err)
	}
	if state != "preparing" {
		t.Fatalf("resumed occurrence state = %q, want preparing", state)
	}
}

func TestProcessorHandsWeeklyRadarOccurrenceToDurableDiscovery(t *testing.T) {
	pool := openWorkerIntegrationPool(t)
	userID, occurrenceID := insertWorkerOccurrence(t, pool, "enqueued", "weekly_radar")
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from river.river_job where args->>'userId' = $1`, userID)
		_, _ = pool.Exec(ctx, `
			delete from app.audit_events
			where action = 'radar_discovery_scheduled'
				and target_id in (
					select id::text from app.radar_discovery_runs where user_id = $1::uuid
				)`, userID)
		_, _ = pool.Exec(ctx, `delete from app.outbox_events where aggregate_id = $1::uuid`, userID)
		_, _ = pool.Exec(ctx, `delete from app.users where id = $1::uuid`, userID)
	})
	jobs, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatal(err)
	}
	radar, err := radarstore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}
	processor := worker.NewProcessor(
		pool,
		"",
		2*time.Second,
		worker.WithRadarScheduleHandoff(radar),
	)
	if err := processor.Process(context.Background(), occurrenceID); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if err := processor.Process(context.Background(), occurrenceID); err != nil {
		t.Fatalf("replayed Process() error = %v", err)
	}
	var state, runID string
	if err := pool.QueryRow(context.Background(), `
		select state, metadata->>'radarDiscoveryRunId'
		from app.schedule_occurrences
		where id = $1::uuid`, occurrenceID).Scan(&state, &runID); err != nil {
		t.Fatal(err)
	}
	if state != "delivered" || runID == "" {
		t.Fatalf("Radar occurrence = state %q, run %q", state, runID)
	}
	var runs, jobsQueued int
	if err := pool.QueryRow(context.Background(), `
		select count(*) from app.radar_discovery_runs
		where id = $1::uuid and user_id = $2::uuid and trigger_type = 'scheduled'`, runID, userID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		select count(*) from river.river_job
		where kind = 'run_weekly_radar_discovery' and args->>'runId' = $1`, runID).Scan(&jobsQueued); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || jobsQueued != 1 {
		t.Fatalf("Radar handoff = %d runs, %d jobs", runs, jobsQueued)
	}
}

func TestProcessorFailsClosedWithoutDigestHandoff(t *testing.T) {
	pool := openWorkerIntegrationPool(t)
	userID, occurrenceID := insertWorkerOccurrence(t, pool, "enqueued")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "delete from app.users where id = $1::uuid", userID)
	})
	err := worker.NewProcessor(pool, "", 2*time.Second).Process(context.Background(), occurrenceID)
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("Process() error = %v, want permanent configuration failure", err)
	}
	var state, errorCode string
	if err := pool.QueryRow(context.Background(), `
		select state, error_code from app.schedule_occurrences where id = $1::uuid`, occurrenceID).Scan(&state, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || errorCode != "digest_handoff_unconfigured" {
		t.Fatalf("failed occurrence = %q/%q", state, errorCode)
	}
}

func TestProcessorRejectsInvalidAndMissingOccurrenceIDs(t *testing.T) {
	pool := openWorkerIntegrationPool(t)
	processor := worker.NewProcessor(pool, "http://127.0.0.1:1/capture", time.Second)
	if err := processor.Process(context.Background(), "not-a-uuid"); !jobqueue.IsPermanent(err) {
		t.Fatalf("invalid ID error = %v, want permanent", err)
	}
	if err := processor.Process(context.Background(), "018f3f1e-7b2a-7cc0-8000-000000000001"); !jobqueue.IsPermanent(err) {
		t.Fatalf("missing ID error = %v, want permanent", err)
	}
}

func openWorkerIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for worker integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newDigestHandoff(t *testing.T, pool *pgxpool.Pool) *digeststore.Store {
	t.Helper()
	jobs, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatal(err)
	}
	store, err := digeststore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func insertWorkerOccurrence(
	t *testing.T,
	pool *pgxpool.Pool,
	state string,
	scheduleTypes ...string,
) (string, string) {
	t.Helper()
	scheduleType := "daily_digest"
	if len(scheduleTypes) > 0 {
		scheduleType = scheduleTypes[0]
	}
	githubID := time.Now().UnixNano()
	login := fmt.Sprintf("worker-test-%d", githubID)
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		)
		values ($1, $2, 'Worker integration test', 'America/New_York', $3, true)
		returning id::text`, githubID, login, fmt.Sprintf("%s@tests.relantern.local", login)).Scan(&userID); err != nil {
		t.Fatalf("insert worker test user: %v", err)
	}
	var scheduleID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.schedule_definitions (
			user_id, schedule_type, timezone, local_time, days_of_week,
			catchup_policy, catchup_grace, next_due_at
		) values (
			$1::uuid, $2, 'America/New_York', '08:00'::time,
			array[1,2,3,4,5,6,7]::smallint[], 'catch_up', interval '6 hours',
			'2026-08-30T12:00:00Z'
		)
		returning id::text`, userID, scheduleType).Scan(&scheduleID); err != nil {
		t.Fatalf("insert worker test schedule: %v", err)
	}
	var occurrenceID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.schedule_occurrences (
			schedule_id, scheduled_for, local_date, local_offset_seconds,
			state, trigger_type, idempotency_key
		) values (
			$1::uuid, '2026-08-29T12:00:00Z', '2026-08-29', -14400,
			$3, 'scheduled', $2
		)
		returning id::text`, scheduleID, "worker-test:"+scheduleID, state).Scan(&occurrenceID); err != nil {
		t.Fatalf("insert worker test occurrence: %v", err)
	}
	return userID, occurrenceID
}
