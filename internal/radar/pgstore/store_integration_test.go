package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/radar"
	radarstore "github.com/traweezy/relantern/internal/radar/pgstore"
)

func TestRadarEvidenceToOwnerDecisionRoundTrip(t *testing.T) {
	pool := openRadarDatabase(t)
	// Keep owner decisions newer than work claimed by an already-running local
	// worker so the decision-history ordering assertion remains race-safe.
	now := time.Now().UTC().Truncate(time.Second)
	userID := insertRadarUser(t, pool)
	cleanupRadarFixture(t, pool, userID)
	insertRadarEvidence(t, pool, userID, now)

	jobs, err := jobqueue.NewIsolatedTestInserter("test_radar")
	if err != nil {
		t.Fatal(err)
	}
	store, err := radarstore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}
	service, err := radar.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	request := radar.QueueDiscoveryRequest{
		UserID: userID, TriggerType: "owner", IdempotencyKey: "radar-integration-discovery-run",
	}
	first, err := service.QueueDiscovery(context.Background(), request, now)
	if err != nil {
		t.Fatalf("QueueDiscovery() error = %v", err)
	}
	replayed, err := service.QueueDiscovery(context.Background(), request, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("replayed QueueDiscovery() error = %v", err)
	}
	if replayed.ID != first.ID {
		t.Fatalf("replayed run ID = %q, want %q", replayed.ID, first.ID)
	}
	var queued int
	if err := pool.QueryRow(context.Background(), `
		select count(*) from river.river_job
		where kind = 'run_weekly_radar_discovery' and args->>'runId' = $1`, first.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued Radar jobs = %d, want 1", queued)
	}

	processed, err := store.ProcessDiscovery(context.Background(), first.ID, userID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ProcessDiscovery() error = %v", err)
	}
	if processed.EvidenceCount != 2 || processed.CandidateCount != 2 || processed.MisleadingCount != 1 {
		t.Fatalf("discovery result = %+v", processed)
	}
	snapshot, err := service.Snapshot(context.Background(), userID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Candidates) != 2 || len(snapshot.Runs) != 1 || snapshot.Runs[0].State != "completed" {
		t.Fatalf("Radar snapshot = %+v", snapshot)
	}
	var good radar.Candidate
	for _, candidate := range snapshot.Candidates {
		if candidate.PackageName == "credible-package" {
			good = candidate
		}
		if candidate.PackageName == "popular-risk" &&
			(candidate.CurrentState != radar.StateReject || candidate.LatestComparison == nil || !candidate.LatestComparison.Misleading) {
			t.Fatalf("misleading candidate = %+v", candidate)
		}
	}
	if good.ID == "" || good.CurrentState != radar.StateAssess || good.LatestComparison == nil ||
		len(good.LatestComparison.Dimensions) != 7 || len(good.Decisions) != 1 {
		t.Fatalf("credible candidate = %+v", good)
	}

	adopted, err := service.Decide(context.Background(), radar.DecisionRequest{
		UserID: userID, CandidateID: good.ID, ExpectedVersion: good.Version,
		State: radar.StateAdopt, Rationale: "Owner approved after a bounded compatibility trial.",
		Evidence: map[string]any{"trialUrl": "https://example.test/trial"},
		ReviewAt: now.Add(90 * 24 * time.Hour), ApplicableProjectTypes: []string{"web"},
		CompatibilityRequirements: []string{"Node 26 ESM"},
		ExitConditions:            []string{"Revert to incumbent on latency regression"},
	}, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if adopted.CurrentState != radar.StateAdopt || adopted.Version != good.Version+1 ||
		len(adopted.Decisions) != 2 || adopted.Decisions[0].DecisionSource != "owner" {
		t.Fatalf("adopted candidate = %+v", adopted)
	}
	if _, err := service.Decide(context.Background(), radar.DecisionRequest{
		UserID: userID, CandidateID: good.ID, ExpectedVersion: good.Version,
		State: radar.StateHold, Rationale: "Stale update",
		Evidence: map[string]any{"url": "https://example.test/stale"},
		ReviewAt: now.Add(120 * 24 * time.Hour), ApplicableProjectTypes: []string{"web"},
		CompatibilityRequirements: []string{"Node 26"}, ExitConditions: []string{"Revert"},
	}, now.Add(5*time.Minute)); !errors.Is(err, radar.ErrConflict) {
		t.Fatalf("stale decision error = %v, want ErrConflict", err)
	}

	_, err = pool.Exec(context.Background(), `
		insert into app.radar_decisions (
			candidate_id, owner_user_id, state, decision_source, rationale, evidence,
			decided_at, review_at, applicable_project_types,
			compatibility_requirements, exit_conditions
		) values ($1::uuid, $2::uuid, 'adopt', 'system', 'Forbidden automatic adoption',
			'{}'::jsonb, $3, $4, array['web'], array['Node 26'], array['Revert'])`,
		good.ID, userID, now, now.Add(24*time.Hour))
	if err == nil {
		t.Fatal("database accepted a system-originated Adopt decision")
	}
}

func openRadarDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for Radar integration tests")
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

func insertRadarUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	suffix := time.Now().UnixNano()
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		) values ($1, $2, 'Radar integration', 'America/New_York', $3, true)
		returning id::text`, suffix, fmt.Sprintf("radar-%d", suffix),
		fmt.Sprintf("radar-%d@tests.relantern.local", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func insertRadarEvidence(t *testing.T, pool *pgxpool.Pool, userID string, now time.Time) {
	t.Helper()
	for _, fixture := range []struct {
		name       string
		license    string
		advisories int
		critical   int
		downloads  int
	}{
		{name: "credible-package", license: "MIT", downloads: 50_000},
		{name: "popular-risk", license: "UNKNOWN", advisories: 1, critical: 1, downloads: 10_000_000},
	} {
		if _, err := pool.Exec(context.Background(), `
			insert into app.package_candidate_evidence (
				user_id, ecosystem, package_name, repository_url, discovery_source,
				incumbent_package, stable_release, license, contributor_count,
				release_cadence_days, issue_response_days, security_response_days,
				security_advisory_count, critical_advisory_count, scorecard_score,
				signed_releases, provenance_verified, types_supported, bundle_size_bytes,
				runtime_compatibility, project_types, compatibility_requirements,
				exit_conditions, maintenance_signals, security_signals,
				popularity_signals, evidence_links, observed_at
			) values (
				$1::uuid, 'npm', $2, $3, 'integration-fixture', 'incumbent', '1.2.3',
				$4, 8, 30, 4, 2, $5, $6, 8.4, true, true, true, 18000,
				array['Node 26'], array['web'], array['ESM'], array['Keep incumbent adapter'],
				'{"releaseCadence":"monthly"}'::jsonb, '{"osvChecked":true}'::jsonb,
				jsonb_build_object('weeklyDownloads', $7::bigint),
				'[{"label":"release","url":"https://example.test/release","sourceTier":"T0"},{"label":"security","url":"https://example.test/security","sourceTier":"T0"},{"label":"docs","url":"https://example.test/docs","sourceTier":"T1"}]'::jsonb,
				$8
			)`, userID, fixture.name, "https://example.test/"+fixture.name,
			fixture.license, fixture.advisories, fixture.critical, fixture.downloads, now); err != nil {
			t.Fatalf("insert Radar evidence %q: %v", fixture.name, err)
		}
	}
}

func cleanupRadarFixture(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from river.river_job where args->>'userId' = $1`, userID)
		_, _ = pool.Exec(ctx, `delete from app.audit_events where actor_id = $1`, userID)
		_, _ = pool.Exec(ctx, `delete from app.outbox_events where aggregate_id = $1::uuid`, userID)
		_, _ = pool.Exec(ctx, `delete from app.users where id = $1::uuid`, userID)
	})
}
