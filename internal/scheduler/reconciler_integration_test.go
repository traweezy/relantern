package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/scheduler"
)

func TestConcurrentReconcilersCreateOneOccurrenceAndRiverJob(t *testing.T) {
	pool := openSchedulerIntegrationPool(t)
	now := time.Date(2026, time.January, 1, 6, 30, 0, 0, time.UTC)
	userID, scheduleID := insertDueSchedule(t, pool, now)
	cleanupScheduleIntegration(t, pool, userID, scheduleID)

	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("NewInserter() error = %v", err)
	}
	first := scheduler.NewReconciler(
		pool,
		clock.NewFixed(now),
		"scheduler-integration-race",
		inserter,
		scheduler.WithScheduleIDs(scheduleID),
	)
	second := scheduler.NewReconciler(
		pool,
		clock.NewFixed(now),
		"scheduler-integration-race",
		inserter,
		scheduler.WithScheduleIDs(scheduleID),
	)

	results := make(chan scheduler.ReconcileResult, 2)
	errorsChannel := make(chan error, 2)
	var group sync.WaitGroup
	for _, reconciler := range []*scheduler.Reconciler{first, second} {
		group.Add(1)
		go func() {
			defer group.Done()
			result, reconcileErr := reconciler.Reconcile(context.Background(), "scheduler-integration-race")
			results <- result
			errorsChannel <- reconcileErr
		}()
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for reconcileErr := range errorsChannel {
		if reconcileErr != nil {
			t.Fatalf("Reconcile() error = %v", reconcileErr)
		}
	}

	var occurrencesCreated int64
	var jobsEnqueued int64
	for result := range results {
		occurrencesCreated += result.OccurrencesCreated
		jobsEnqueued += result.JobsEnqueued
	}
	if occurrencesCreated != 1 || jobsEnqueued != 1 {
		t.Fatalf("concurrent totals = occurrences %d, jobs %d", occurrencesCreated, jobsEnqueued)
	}

	var occurrenceCount int
	var jobCount int
	var state string
	var localDate time.Time
	var localOffsetSeconds int
	if err := pool.QueryRow(context.Background(), `
		select count(*), count(river_job.id), min(occurrence.state),
			min(occurrence.local_date), min(occurrence.local_offset_seconds)
		from app.schedule_occurrences occurrence
		left join river.river_job river_job on river_job.id = occurrence.river_job_id
		where occurrence.schedule_id = $1::uuid`, scheduleID).Scan(
		&occurrenceCount,
		&jobCount,
		&state,
		&localDate,
		&localOffsetSeconds,
	); err != nil {
		t.Fatalf("inspect occurrence ledger: %v", err)
	}
	if occurrenceCount != 1 || jobCount != 1 || state != "enqueued" {
		t.Fatalf("ledger = occurrences %d, jobs %d, state %q", occurrenceCount, jobCount, state)
	}
	if localDate.Format(time.DateOnly) != "2026-01-01" || localOffsetSeconds != -5*60*60 {
		t.Fatalf("local projection = %s offset %d", localDate, localOffsetSeconds)
	}

	restarted := scheduler.NewReconciler(
		pool,
		clock.NewFixed(now),
		"scheduler-integration-restart",
		inserter,
		scheduler.WithScheduleIDs(scheduleID),
	)
	restartResult, err := restarted.Reconcile(context.Background(), "scheduler-integration-restart")
	if err != nil {
		t.Fatalf("restart Reconcile() error = %v", err)
	}
	if restartResult.OccurrencesCreated != 0 || restartResult.JobsEnqueued != 0 {
		t.Fatalf("restart result = %+v, want no duplicate work", restartResult)
	}
}

func TestReconcileRollsBackOccurrenceWhenEnqueueFails(t *testing.T) {
	pool := openSchedulerIntegrationPool(t)
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	userID, scheduleID := insertDueSchedule(t, pool, now)
	cleanupScheduleIntegration(t, pool, userID, scheduleID)

	reconciler := scheduler.NewReconciler(
		pool,
		clock.NewFixed(now),
		"scheduler-integration-rollback",
		failingOccurrenceEnqueuer{},
		scheduler.WithScheduleIDs(scheduleID),
	)
	if _, err := reconciler.Reconcile(context.Background(), "scheduler-integration-rollback"); err == nil {
		t.Fatal("Reconcile() succeeded when River enqueue failed")
	}

	var occurrenceCount int
	var nextDueAt time.Time
	if err := pool.QueryRow(context.Background(), `
		select
			(select count(*) from app.schedule_occurrences where schedule_id = $1::uuid),
			next_due_at
		from app.schedule_definitions
		where id = $1::uuid`, scheduleID).Scan(&occurrenceCount, &nextDueAt); err != nil {
		t.Fatalf("inspect rolled back reconciliation: %v", err)
	}
	if occurrenceCount != 0 || !nextDueAt.Equal(now) {
		t.Fatalf("rollback left count %d and next_due_at %s", occurrenceCount, nextDueAt)
	}
}

type failingOccurrenceEnqueuer struct{}

func (failingOccurrenceEnqueuer) EnqueueScheduleOccurrence(
	context.Context,
	pgx.Tx,
	string,
	string,
) (int64, bool, error) {
	return 0, false, errors.New("injected River enqueue failure")
}

func openSchedulerIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for scheduler integration tests")
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
	var riverTable *string
	if err := pool.QueryRow(context.Background(), "select to_regclass('river.river_job')::text").Scan(&riverTable); err != nil {
		t.Fatalf("check River migration: %v", err)
	}
	if riverTable == nil {
		t.Skip("River migrations are required for scheduler integration tests")
	}
	return pool
}

func insertDueSchedule(t *testing.T, pool *pgxpool.Pool, dueAt time.Time) (string, string) {
	t.Helper()
	githubID := time.Now().UnixNano()
	login := fmt.Sprintf("scheduler-test-%d", githubID)
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		)
		values ($1, $2, 'Scheduler integration test', 'America/New_York', $3, true)
		returning id::text`, githubID, login, fmt.Sprintf("%s@tests.relantern.local", login)).Scan(&userID); err != nil {
		t.Fatalf("insert scheduler test user: %v", err)
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
		t.Fatalf("insert due schedule: %v", err)
	}
	return userID, scheduleID
}

func cleanupScheduleIntegration(
	t *testing.T,
	pool *pgxpool.Pool,
	userID string,
	scheduleID string,
) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			delete from river.river_job
			where id in (
				select river_job_id from app.schedule_occurrences
				where schedule_id = $1::uuid and river_job_id is not null
			)`, scheduleID)
		_, _ = pool.Exec(context.Background(), "delete from app.users where id = $1::uuid", userID)
	})
}
