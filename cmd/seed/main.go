package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/scheduler"
)

const ownerGitHubID int64 = 5276132

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background()); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
	logger.Info("local seed complete")
}

func run(ctx context.Context) error {
	common, err := config.LoadCommon()
	if err != nil {
		return err
	}
	if common.Environment != config.EnvironmentLocal && common.Environment != config.EnvironmentTest {
		return fmt.Errorf("seed is restricted to local and test environments")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return err
	}
	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	if err := tx.QueryRow(ctx, `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		)
		values (
			$1::bigint, 'traweezy', 'Relantern owner', 'America/New_York',
			($1::bigint)::text || '@github.relantern.local', true
		)
		on conflict (github_user_id) do update
		set login = excluded.login, display_name = excluded.display_name,
			timezone = excluded.timezone, email = excluded.email,
			email_verified = excluded.email_verified, updated_at = now()
		returning id::text`, ownerGitHubID).Scan(&userID); err != nil {
		return fmt.Errorf("seed owner: %w", err)
	}

	if err := seedSchedule(ctx, tx, userID, common.Clock.Now(), "daily_digest", scheduler.LocalTime{Hour: 8}, []int16{1, 2, 3, 4, 5, 6, 7}); err != nil {
		return err
	}
	if err := seedSchedule(ctx, tx, userID, common.Clock.Now(), "weekly_radar", scheduler.LocalTime{Hour: 9}, []int16{6}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

func seedSchedule(ctx context.Context, tx pgx.Tx, userID string, now time.Time, scheduleType string, localTime scheduler.LocalTime, weekdays []int16) error {
	definition, err := scheduler.NewDefinition("America/New_York", localTime, weekdays)
	if err != nil {
		return err
	}
	nextDueAt, err := scheduler.NextOccurrence(now.Add(-time.Minute), definition)
	if err != nil {
		return fmt.Errorf("calculate %s next occurrence: %w", scheduleType, err)
	}
	_, err = tx.Exec(ctx, `
		insert into app.schedule_definitions (
			user_id, schedule_type, timezone, local_time, days_of_week,
			catchup_policy, catchup_grace, next_due_at, config
		) values ($1::uuid, $2, 'America/New_York', $3::time, $4, 'catch_up', interval '6 hours', $5, $6)
		on conflict (user_id, schedule_type) do update
		set timezone = excluded.timezone, local_time = excluded.local_time,
			days_of_week = excluded.days_of_week, catchup_policy = excluded.catchup_policy,
			catchup_grace = excluded.catchup_grace, next_due_at = excluded.next_due_at,
			config = excluded.config, version = app.schedule_definitions.version + 1,
			updated_at = now()`,
		userID,
		scheduleType,
		fmt.Sprintf("%02d:%02d", localTime.Hour, localTime.Minute),
		weekdays,
		nextDueAt,
		`{"delivery":"fake-only","maximumItems":10}`,
	)
	if err != nil {
		return fmt.Errorf("seed %s schedule: %w", scheduleType, err)
	}
	return nil
}
