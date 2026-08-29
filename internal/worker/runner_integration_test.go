package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fakeprovider"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/scheduler"
)

func TestRiverOneShotRunsReconciliationAndOccurrenceToCompletion(t *testing.T) {
	pool := openRunnerIntegrationPool(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := fakeprovider.New(fakeprovider.KindDelivery, logger)
	if err != nil {
		t.Fatalf("fakeprovider.New() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Keep this fixture before every scheduler-package test clock. This worker
	// can claim its own due row without also claiming concurrent scheduler test
	// rows or the current seed schedule.
	now := time.Date(2025, time.January, 3, 6, 30, 0, 0, time.UTC)
	userID, scheduleID := insertRunnerSchedule(t, pool, now)
	runID := uuid.NewString()
	cleanupRunnerIntegration(t, pool, userID, scheduleID, runID)
	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("jobqueue.NewInserter() error = %v", err)
	}
	reconciler := scheduler.NewReconciler(
		pool,
		clock.NewFixed(now),
		"worker-runner-integration",
		inserter,
		scheduler.WithScheduleIDs(scheduleID),
	)
	processor := NewProcessor(pool, server.URL+"/capture", 2*time.Second)
	health := NewSchedulerHealth(pool, clock.NewFixed(now), time.Minute)
	client, err := NewRiverClient(
		pool,
		clock.NewFixed(now),
		time.Minute,
		2*time.Second,
		false,
		logger,
		health,
		reconciler,
		processor,
		nil,
		nil,
		10*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewRiverClient() error = %v", err)
	}
	runner := NewRunner(client, pool, health, 2*time.Second)
	runner.newRunID = func() string { return runID }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runner.Once(ctx, logger); err != nil {
		var jobs string
		if inspectErr := pool.QueryRow(context.Background(), `
			select coalesce(
				jsonb_agg(jsonb_build_object(
					'id', id,
					'kind', kind,
					'state', state,
					'attempt', attempt,
					'args', args
				) order by id),
				'[]'::jsonb
			)::text
			from river.river_job
			where args ->> 'runId' = $1`, runID).Scan(&jobs); inspectErr != nil {
			jobs = "inspection failed: " + inspectErr.Error()
		}
		t.Fatalf("runner.Once() error = %v; jobs = %s", err, jobs)
	}

	var occurrenceState string
	var riverState string
	if err := pool.QueryRow(context.Background(), `
		select occurrence.state, river_job.state::text
		from app.schedule_occurrences occurrence
		join river.river_job river_job on river_job.id = occurrence.river_job_id
		where occurrence.schedule_id = $1::uuid`, scheduleID).Scan(&occurrenceState, &riverState); err != nil {
		t.Fatalf("inspect one-shot result: %v", err)
	}
	if occurrenceState != "delivered" || riverState != "completed" {
		t.Fatalf("one-shot states = occurrence %q, River %q", occurrenceState, riverState)
	}
	var occurrenceID string
	if err := pool.QueryRow(context.Background(), `
		select id::text from app.schedule_occurrences where schedule_id = $1::uuid`, scheduleID).Scan(&occurrenceID); err != nil {
		t.Fatalf("select delivered occurrence: %v", err)
	}
	if err := processor.recordDelivery(context.Background(), occurrenceID); err != nil {
		t.Fatalf("idempotent recordDelivery() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		update app.schedule_occurrences set state = 'failed' where id = $1::uuid`, occurrenceID); err != nil {
		t.Fatalf("prepare unexpected delivery state: %v", err)
	}
	if err := processor.recordDelivery(context.Background(), occurrenceID); err == nil {
		t.Fatal("recordDelivery() accepted an unexpected failed state")
	}

	readyRequest := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyResponse := httptest.NewRecorder()
	runner.healthHandler().ServeHTTP(readyResponse, readyRequest)
	if readyResponse.Code != http.StatusOK {
		t.Fatalf("ready status = %d, body = %s", readyResponse.Code, readyResponse.Body.String())
	}
}

func TestRunnerReadinessAndGracefulStop(t *testing.T) {
	pool := openRunnerIntegrationPool(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	health := NewSchedulerHealth(pool, clock.NewFixed(now), time.Minute)
	client, err := NewRiverClient(
		pool,
		clock.NewFixed(now),
		time.Minute,
		2*time.Second,
		false,
		logger,
		health,
		scheduler.NewReconciler(pool, clock.NewFixed(now), "unused-runner", failingRunnerEnqueuer{}),
		NewProcessor(pool, "http://127.0.0.1:1/capture", time.Second),
		nil,
		nil,
		10*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewRiverClient() error = %v", err)
	}
	runner := NewRunner(client, pool, health, 2*time.Second)

	unreadyRequest := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	unreadyResponse := httptest.NewRecorder()
	runner.healthHandler().ServeHTTP(unreadyResponse, unreadyRequest)
	if unreadyResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("initial ready status = %d", unreadyResponse.Code)
	}
	health.RecordSuccess()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := runner.Run(ctx, logger, 0); err != nil {
		t.Fatalf("Runner.Run() error = %v", err)
	}
	if _, err := NewRiverClient(
		pool,
		clock.NewFixed(now),
		time.Minute,
		2*time.Second,
		true,
		logger,
		health,
		scheduler.NewReconciler(pool, clock.NewFixed(now), "periodic-runner", failingRunnerEnqueuer{}),
		NewProcessor(pool, "http://127.0.0.1:1/capture", time.Second),
		nil,
		nil,
		10*time.Minute,
	); err != nil {
		t.Fatalf("NewRiverClient() with periodic schedule error = %v", err)
	}
}

func TestRunnerStopsRiverWhenHealthServerCannotBind(t *testing.T) {
	pool := openRunnerIntegrationPool(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	health := NewSchedulerHealth(pool, clock.NewFixed(now), time.Minute)
	client, err := NewRiverClient(
		pool,
		clock.NewFixed(now),
		time.Minute,
		2*time.Second,
		false,
		logger,
		health,
		scheduler.NewReconciler(pool, clock.NewFixed(now), "bind-error-runner", failingRunnerEnqueuer{}),
		NewProcessor(pool, "http://127.0.0.1:1/capture", time.Second),
		nil,
		nil,
		10*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewRiverClient() error = %v", err)
	}
	runner := NewRunner(client, pool, health, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.Run(ctx, logger, uint16(port)); err == nil {
		t.Fatal("Runner.Run() succeeded while its health port was already bound")
	}
}

func TestRiverErrorHandlerReturnsDefaultRetryDecision(t *testing.T) {
	t.Parallel()

	handler := &errorHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	job := &rivertype.JobRow{ID: 7, Kind: "fixture", Queue: "maintenance", Attempt: 1, MaxAttempts: 3}
	if result := handler.HandleError(context.Background(), job, context.DeadlineExceeded); result != nil {
		t.Fatalf("HandleError() = %+v, want default nil decision", result)
	}
	if result := handler.HandlePanic(context.Background(), job, "fixture panic", "redacted trace"); result != nil {
		t.Fatalf("HandlePanic() = %+v, want default nil decision", result)
	}
}

func TestScheduleOccurrenceWorkerCancelsPermanentFailure(t *testing.T) {
	t.Parallel()

	worker := &scheduleOccurrenceWorker{
		processor: NewProcessor(nil, "http://127.0.0.1:1/capture", time.Second),
	}
	err := worker.Work(context.Background(), &river.Job[jobqueue.ScheduleOccurrenceArgs]{
		Args: jobqueue.ScheduleOccurrenceArgs{OccurrenceID: "invalid"},
	})
	if err == nil {
		t.Fatal("Work() succeeded for a permanent invalid occurrence ID")
	}
}

type failingRunnerEnqueuer struct{}

func (failingRunnerEnqueuer) EnqueueScheduleOccurrence(
	context.Context,
	pgx.Tx,
	string,
	string,
) (int64, bool, error) {
	return 0, false, context.Canceled
}

func openRunnerIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for runner integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("config.LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertRunnerSchedule(t *testing.T, pool *pgxpool.Pool, dueAt time.Time) (string, string) {
	t.Helper()
	githubID := time.Now().UnixNano()
	login := fmt.Sprintf("runner-test-%d", githubID)
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		)
		values ($1, $2, 'Runner integration test', 'America/New_York', $3, true)
		returning id::text`, githubID, login, fmt.Sprintf("%s@tests.relantern.local", login)).Scan(&userID); err != nil {
		t.Fatalf("insert runner test user: %v", err)
	}
	var scheduleID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.schedule_definitions (
			user_id, schedule_type, timezone, local_time, days_of_week,
			catchup_policy, catchup_grace, next_due_at
		) values (
			$1::uuid, 'daily_digest', 'America/New_York', '01:30'::time,
			array[1,2,3,4,5,6,7]::smallint[], 'catch_up', interval '6 hours', $2
		)
		returning id::text`, userID, dueAt).Scan(&scheduleID); err != nil {
		t.Fatalf("insert runner test schedule: %v", err)
	}
	return userID, scheduleID
}

func cleanupRunnerIntegration(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	scheduleID string,
	runID string,
) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			delete from river.river_job
			where (kind = $1 and args ->> 'runId' = $2)
				or id in (
					select river_job_id from app.schedule_occurrences
					where schedule_id = $3::uuid and river_job_id is not null
				)`, jobqueue.ReconcileSchedulesKind, runID, scheduleID)
		_, _ = pool.Exec(context.Background(), "delete from app.users where id = $1::uuid", userID)
	})
}
