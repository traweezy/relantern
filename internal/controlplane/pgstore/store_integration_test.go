package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/controlplane"
	controlplanestore "github.com/traweezy/relantern/internal/controlplane/pgstore"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/scheduler"
)

func TestOwnerControlPlaneRoundTrip(t *testing.T) {
	pool := openControlPlanePool(t)
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	userID, scheduleID, sourceID := seedControlPlaneFixture(t, pool, now)
	cleanupControlPlaneFixture(t, pool, userID)
	restoreCompletedAt := seedRestoreDrillFixture(t, pool)

	jobs, err := jobqueue.NewIsolatedTestInserter("test_controlplane")
	if err != nil {
		t.Fatalf("NewIsolatedTestInserter() error = %v", err)
	}
	store, err := controlplanestore.New(pool, jobs)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service, err := controlplane.NewService(store, controlplane.DeploymentMetadata{
		Environment: "test", Version: "1.0.0-test", GitSHA: "fixture-sha",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	sources, err := service.Sources(context.Background(), userID, now)
	if err != nil {
		t.Fatalf("Sources() error = %v", err)
	}
	if len(sources.Sources) == 0 {
		t.Fatal("Sources() returned an empty registry")
	}
	preference, err := service.UpdateSourcePreference(context.Background(), controlplane.UpdateSourcePreferenceRequest{
		UserID: userID, SourceID: sourceID, Muted: true,
		ExcludeFromDigest: true, RelevanceAdjustment: -0.25, ExpectedVersion: 0,
	}, now)
	if err != nil {
		t.Fatalf("UpdateSourcePreference() error = %v", err)
	}
	if preference.Version != 1 || !preference.Muted || !preference.ExcludeFromDigest {
		t.Fatalf("source preference = %+v", preference)
	}
	if _, err := service.UpdateSourcePreference(context.Background(), controlplane.UpdateSourcePreferenceRequest{
		UserID: userID, SourceID: sourceID, ExpectedVersion: 0,
	}, now); !errors.Is(err, controlplane.ErrConflict) {
		t.Fatalf("stale preference error = %v, want ErrConflict", err)
	}
	testedSource, err := service.ActOnSource(context.Background(), controlplane.SourceActionRequest{
		UserID: userID, SourceID: sourceID, Action: "test", Reason: "Verify reviewed local configuration",
	}, now)
	if err != nil {
		t.Fatalf("ActOnSource(test) error = %v", err)
	}
	if testedSource.LatestValidation == nil || testedSource.LatestValidation.State != "passed" {
		t.Fatalf("source validation = %+v", testedSource.LatestValidation)
	}

	settings, err := service.Settings(context.Background(), userID, now)
	if err != nil {
		t.Fatalf("Settings() error = %v", err)
	}
	if len(settings.Schedules) != 1 || settings.Schedules[0].ID != scheduleID {
		t.Fatalf("settings schedules = %+v", settings.Schedules)
	}
	updatedSettings, err := service.UpdateSettings(context.Background(), controlplane.UpdateSettingsRequest{
		UserID: userID, ExpectedVersion: settings.Owner.Version,
		ProfileName: "Platform intelligence", ProfileSummary: "Go, PostgreSQL, and security changes.",
		Topics: []controlplane.InterestTopic{
			{TopicID: "go", Priority: 1, Weight: 1, Keywords: []string{"toolchain"}, Exclusions: []string{"tutorial"}},
			{TopicID: "postgresql", Priority: 1, Weight: 0.9, Keywords: []string{}, Exclusions: []string{}},
		},
		Technologies: []controlplane.WatchedTechnology{{
			Technology: "Go", PackageName: "go", CurrentVersion: "1.27.0",
			VersionConstraint: ">=1.27", Status: "active", Source: "test-fixture",
		}},
		Timezone: "America/New_York", QuietHoursStart: "22:00", QuietHoursEnd: "07:00",
		CriticalAlertsBypass: true, MonthlySoftBudgetUSD: "20.00", MonthlyHardBudgetUSD: "40.00",
		RawRetentionDays: 120, AuditRetentionDays: 400,
	}, now)
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if updatedSettings.Owner.Version != settings.Owner.Version+1 ||
		len(updatedSettings.Profile.Topics) != 2 || len(updatedSettings.Technologies) != 1 {
		t.Fatalf("updated settings = %+v", updatedSettings)
	}

	schedule := updatedSettings.Schedules[0]
	schedule, err = service.UpdateSchedule(context.Background(), controlplane.UpdateScheduleRequest{
		UserID: userID, ScheduleID: schedule.ID, ExpectedVersion: schedule.Version,
		Timezone: schedule.Timezone, LocalTime: "08:30", DaysOfWeek: []int16{1, 2, 3, 4, 5},
		Enabled: true, CatchupPolicy: "catch_up", CatchupGraceMinutes: 360,
		WeekendMode: "off", MaximumItems: 15, MinimumScore: 0.7,
		IncludeComingSoon: true, IncludeRadarCandidates: true,
		IncludeLaterReminders: false, EmptyBehavior: "dashboard_only", Channels: []string{"dashboard"},
	}, now)
	if err != nil {
		t.Fatalf("UpdateSchedule() error = %v", err)
	}
	if schedule.LocalTime != "08:30" || schedule.MaximumItems != 15 || schedule.Version < 2 {
		t.Fatalf("updated schedule = %+v", schedule)
	}

	runRequest := controlplane.ScheduleActionRequest{
		UserID: userID, ScheduleID: schedule.ID, Action: "run_now",
		Reason: "Preview the current evidence window", IdempotencyKey: "fixture-run-now-idempotency-key",
	}
	firstRun, err := service.ActOnSchedule(context.Background(), runRequest, now)
	if err != nil {
		t.Fatalf("ActOnSchedule(run_now) error = %v", err)
	}
	secondRun, err := service.ActOnSchedule(context.Background(), runRequest, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("replayed ActOnSchedule(run_now) error = %v", err)
	}
	if firstRun.OccurrenceID == "" || secondRun.OccurrenceID != firstRun.OccurrenceID {
		t.Fatalf("run-now occurrences = %q and %q", firstRun.OccurrenceID, secondRun.OccurrenceID)
	}
	var externalDelivery bool
	if err := pool.QueryRow(context.Background(), `
		select coalesce((metadata->>'externalDelivery')::boolean, true)
		from app.schedule_occurrences where id = $1::uuid`, firstRun.OccurrenceID).Scan(&externalDelivery); err != nil {
		t.Fatal(err)
	}
	if externalDelivery {
		t.Fatal("run-now preview enabled external delivery")
	}
	preview, err := service.PreviewSchedule(context.Background(), userID, schedule.ID, now)
	if err != nil {
		t.Fatalf("PreviewSchedule() error = %v", err)
	}
	if preview.ExternalDelivery || preview.MaximumItems != 15 {
		t.Fatalf("schedule preview = %+v", preview)
	}

	operations, err := service.Operations(context.Background(), userID, now)
	if err != nil {
		t.Fatalf("Operations() error = %v", err)
	}
	if len(operations.Queues) < 7 || len(operations.Schedules) != 1 ||
		len(operations.Occurrences) != 1 || operations.Deployment.GitSHA != "fixture-sha" ||
		operations.Restore.State != "passed" || operations.Restore.Explanation != fmt.Sprintf(
		"Verified %s · RPO 12s/86400s · RTO 34s/14400s",
		restoreCompletedAt.Format(time.RFC3339),
	) {
		t.Fatalf("operations snapshot = %+v", operations)
	}
}

func seedRestoreDrillFixture(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	startedAt := time.Now().UTC().AddDate(50, 0, 0)
	completedAt := startedAt.Add(34 * time.Second)
	backupSHA256 := []byte(fmt.Sprintf("%032d", time.Now().UnixNano()))
	var drillID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.restore_drills (
			backup_sha256, release_git_sha, state, rpo_seconds, rto_seconds,
			rpo_target_seconds, rto_target_seconds, restored_migration_version,
			verification_counts, started_at, completed_at
		) values ($1, 'abcdef0', 'passed', 12, 34, 86400, 14400, 20, '{}'::jsonb, $2, $3)
		returning id::text`, backupSHA256, startedAt, completedAt).Scan(&drillID); err != nil {
		t.Fatalf("insert restore drill fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from app.restore_drills where id = $1::uuid`, drillID)
	})
	return completedAt
}

func openControlPlanePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for control-plane integration tests")
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

func seedControlPlaneFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
) (string, string, string) {
	t.Helper()
	transaction, err := pool.BeginTx(context.Background(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("begin control-plane fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	githubID := time.Now().UnixNano()
	login := fmt.Sprintf("control-plane-%d", githubID)
	var userID string
	if err := transaction.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		) values ($1, $2, 'Control plane test', 'America/New_York', $3, true)
		returning id::text`, githubID, login, login+"@tests.relantern.local").Scan(&userID); err != nil {
		t.Fatalf("insert control-plane user: %v", err)
	}
	if _, err := transaction.Exec(context.Background(), `
		insert into app.owner_settings (user_id) values ($1::uuid)`, userID); err != nil {
		t.Fatalf("insert owner settings: %v", err)
	}
	var profileID string
	if err := transaction.QueryRow(context.Background(), `
		insert into app.interest_profiles (user_id, name, profile_summary)
		values ($1::uuid, 'Fixture', 'Fixture profile')
		returning id::text`, userID).Scan(&profileID); err != nil {
		t.Fatalf("insert interest profile: %v", err)
	}
	if _, err := transaction.Exec(context.Background(), `
		insert into app.interest_topics (profile_id, topic_id, priority, weight)
		values ($1::uuid, 'go', 1, 1)`, profileID); err != nil {
		t.Fatalf("insert interest topic: %v", err)
	}
	definition, err := scheduler.NewDefinition("America/New_York", scheduler.LocalTime{Hour: 8}, []int16{1, 2, 3, 4, 5, 6, 7})
	if err != nil {
		t.Fatalf("NewDefinition() error = %v", err)
	}
	nextDueAt, err := scheduler.NextOccurrence(now, definition)
	if err != nil {
		t.Fatalf("NextOccurrence() error = %v", err)
	}
	var scheduleID string
	if err := transaction.QueryRow(context.Background(), `
		insert into app.schedule_definitions (
			user_id, schedule_type, timezone, local_time, days_of_week,
			catchup_policy, catchup_grace, next_due_at
		) values (
			$1::uuid, 'daily_digest', 'America/New_York', '08:00'::time,
			array[1,2,3,4,5,6,7]::smallint[], 'catch_up', interval '6 hours', $2
		)
		returning id::text`, userID, nextDueAt).Scan(&scheduleID); err != nil {
		t.Fatalf("insert control-plane schedule: %v", err)
	}
	var sourceID string
	if err := transaction.QueryRow(context.Background(), `
		select id
		from app.sources
		where origin = 'system'
		order by id
		limit 1`).Scan(&sourceID); err != nil {
		t.Fatalf("select control-plane source: %v", err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatalf("commit control-plane fixture: %v", err)
	}
	return userID, scheduleID, sourceID
}

func cleanupControlPlaneFixture(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from river.river_job where args->>'occurrenceId' in (
			select occurrence.id::text from app.schedule_occurrences occurrence
			join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
			where schedule.user_id = $1::uuid
		)`, userID)
		_, _ = pool.Exec(context.Background(), `delete from app.audit_events where actor_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `delete from app.outbox_events where aggregate_id = $1::uuid`, userID)
		_, _ = pool.Exec(context.Background(), `delete from app.users where id = $1::uuid`, userID)
	})
}
