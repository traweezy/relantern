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
	if err := seedOwnerControlPlane(ctx, tx, userID, common.Clock.Now().UTC()); err != nil {
		return err
	}
	if err := seedRadarEvidence(ctx, tx, userID, common.Clock.Now().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}
	return nil
}

func seedRadarEvidence(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	type evidenceSeed struct {
		name         string
		incumbent    string
		license      string
		contributors int
		advisories   int
		critical     int
		scorecard    string
		signed       bool
		provenance   bool
		downloads    int
	}
	for _, evidence := range []evidenceSeed{
		{
			name: "@fixture/typed-client", incumbent: "undici", license: "MIT",
			contributors: 12, scorecard: "8.7", signed: true, provenance: true,
			downloads: 85_000,
		},
		{
			name: "@fixture/solo-adapter", incumbent: "existing-adapter", license: "Apache-2.0",
			contributors: 1, scorecard: "6.2", signed: false, provenance: false,
			downloads: 18_000,
		},
		{
			name: "@fixture/popular-risk", incumbent: "current-security-client", license: "UNKNOWN",
			contributors: 24, advisories: 2, critical: 1, scorecard: "7.9",
			signed: true, provenance: true, downloads: 12_000_000,
		},
	} {
		if _, err := tx.Exec(ctx, `
			insert into app.package_candidate_evidence (
				user_id, ecosystem, package_name, repository_url, discovery_source,
				incumbent_package, stable_release, license, contributor_count,
				release_cadence_days, issue_response_days, security_response_days,
				security_advisory_count, critical_advisory_count, scorecard_score,
				signed_releases, provenance_verified, types_supported, bundle_size_bytes,
				runtime_compatibility, project_types, compatibility_requirements,
				exit_conditions, maintenance_signals, security_signals,
				popularity_signals, evidence_links, observed_at
			)
			select
				$1::uuid, 'npm', $2, 'https://fixtures.relantern.local/' || $2,
				'local-reviewed-fixture', $3, '1.2.3', $4, $5, 30, 4, 2, $6, $7,
				$8::numeric, $9, $10, true, 18000, array['Node 26', 'modern browsers'],
				array['web', 'service'], array['ESM', 'Node 26'],
				array['Retain the incumbent adapter', 'Rollback on measured regression'],
				'{"releaseCadence":"monthly","maintainerIdentity":"fixture-org"}'::jsonb,
				'{"osvChecked":true,"githubAdvisoriesChecked":true}'::jsonb,
				jsonb_build_object('weeklyDownloads', $11::bigint),
				'[{"label":"release","url":"https://fixtures.relantern.local/release","sourceTier":"T0"},{"label":"security","url":"https://fixtures.relantern.local/security","sourceTier":"T0"},{"label":"docs","url":"https://fixtures.relantern.local/docs","sourceTier":"T1"}]'::jsonb,
				$12
			where not exists (
				select 1 from app.package_candidate_evidence
				where user_id = $1::uuid and ecosystem = 'npm' and package_name = $2
					and discovery_source = 'local-reviewed-fixture'
			)`,
			userID, evidence.name, evidence.incumbent, evidence.license,
			evidence.contributors, evidence.advisories, evidence.critical,
			evidence.scorecard, evidence.signed, evidence.provenance,
			evidence.downloads, now,
		); err != nil {
			return fmt.Errorf("seed Radar evidence %q: %w", evidence.name, err)
		}
	}
	return nil
}

func seedOwnerControlPlane(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		insert into app.owner_settings (user_id)
		values ($1::uuid)
		on conflict (user_id) do nothing`, userID); err != nil {
		return fmt.Errorf("seed owner settings: %w", err)
	}
	var profileID string
	if err := tx.QueryRow(ctx, `
		with inserted as (
			insert into app.interest_profiles (
				user_id, name, profile_summary, created_at, updated_at
			) values (
				$1::uuid, 'Owner intelligence',
				'Backend, web-platform, data, infrastructure, security, and developer-tool intelligence.',
				$2, $2
			)
			on conflict (user_id, name) do nothing
			returning id
		)
		select id::text from inserted
		union all
		select id::text from app.interest_profiles
		where user_id = $1::uuid and is_active
			and not exists (select 1 from inserted)
		limit 1`, userID, now).Scan(&profileID); err != nil {
		return fmt.Errorf("seed owner interest profile: %w", err)
	}
	for index, topic := range []string{
		"go", "postgresql", "security", "typescript", "react", "nextjs", "developer-tools", "infrastructure",
	} {
		priority := int16(2)
		weight := "0.8000"
		if index < 3 {
			priority = 1
			weight = "1.0000"
		}
		if _, err := tx.Exec(ctx, `
			insert into app.interest_topics (
				profile_id, topic_id, priority, weight, keywords, exclusions, created_at, updated_at
			) values ($1::uuid, $2, $3, $4::numeric, array[]::text[], array[]::text[], $5, $5)
			on conflict (profile_id, topic_id) do nothing`,
			profileID, topic, priority, weight, now); err != nil {
			return fmt.Errorf("seed owner interest topic %q: %w", topic, err)
		}
	}
	type technologySeed struct {
		name        string
		packageName string
		version     string
	}
	for _, technology := range []technologySeed{
		{name: "Biome", packageName: "@biomejs/biome", version: "2.5.10"},
		{name: "Go", packageName: "go", version: "1.27.0"},
		{name: "Next.js", packageName: "next", version: "16.3.3"},
		{name: "Node.js", packageName: "node", version: "26.8.1"},
		{name: "pnpm", packageName: "pnpm", version: "11.24.0"},
		{name: "PostgreSQL", packageName: "postgresql", version: "18.6"},
		{name: "React", packageName: "react", version: "19.2.8"},
		{name: "TypeScript", packageName: "typescript", version: "5.9.3"},
	} {
		if _, err := tx.Exec(ctx, `
			insert into app.watched_technologies (
				user_id, technology, package_name, current_version,
				version_constraint, status, source, last_verified_at,
				created_at, updated_at
			) values (
				$1::uuid, $2, $3, $4, '', 'active', 'version-manifest', $5, $5, $5
			)
			on conflict (user_id, package_name) do nothing`,
			userID, technology.name, technology.packageName, technology.version, now); err != nil {
			return fmt.Errorf("seed watched technology %q: %w", technology.packageName, err)
		}
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
