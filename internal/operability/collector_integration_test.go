package operability_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/operability"
)

func TestCollectorReadsMigratedOperationalState(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for operability integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC()
	backupHash := sha256.Sum256([]byte(t.Name() + now.Format(time.RFC3339Nano)))
	var restoreID string
	err = pool.QueryRow(ctx, `
		insert into app.restore_drills (
			backup_sha256, release_git_sha, state, rpo_seconds, rto_seconds,
			rpo_target_seconds, rto_target_seconds, restored_migration_version,
			verification_counts, started_at, completed_at
		) values ($1, $2, 'passed', 60, 120, 86400, 14400, 20, '{"stories": 1}'::jsonb, $3, $4)
		returning id::text`,
		backupHash[:], strings.Repeat("a", 40), now.Add(-time.Minute), now,
	).Scan(&restoreID)
	if err != nil {
		t.Fatalf("insert restore drill fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := pool.Exec(cleanupCtx, `delete from app.restore_drills where id = $1::uuid`, restoreID); cleanupErr != nil {
			t.Errorf("delete restore drill fixture: %v", cleanupErr)
		}
	})

	collector, err := operability.NewCollector(pool)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	snapshot, err := collector.Collect(ctx, now)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.GeneratedAt != now {
		t.Fatalf("GeneratedAt = %s, want %s", snapshot.GeneratedAt, now)
	}
	if snapshot.LastRestoreState != "passed" {
		t.Fatalf("LastRestoreState = %q, want passed", snapshot.LastRestoreState)
	}

	var output bytes.Buffer
	if err := operability.WritePrometheus(&output, snapshot); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	if !strings.Contains(output.String(), "river_queue_depth") {
		t.Fatalf("metrics output lacks river_queue_depth: %s", output.String())
	}
	_ = operability.Evaluate(snapshot, now)
}

func TestCollectorCountsPendingAdvisoryObservations(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for operability integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC().Truncate(time.Microsecond)
	collector, err := operability.NewCollector(pool)
	if err != nil {
		t.Fatal(err)
	}
	before, err := collector.Collect(ctx, now)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	sourceID := "test-operability-observation-" + suffix
	if _, err := pool.Exec(ctx, `insert into app.sources
		(id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at)
		values ($1, 'Observation metric fixture', 'T1', 'Test owner', 'system',
		'active', 'https://github.com/advisories', 'link-and-excerpt', true,
		array['security'], $2)`, sourceID, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := pool.Exec(cleanupCtx, `delete from app.sources where id = $1`, sourceID); cleanupErr != nil {
			t.Errorf("delete observation metric fixture: %v", cleanupErr)
		}
	})
	digest := sha256.Sum256([]byte(sourceID))
	var rawID string
	if err := pool.QueryRow(ctx, `insert into app.raw_documents
		(source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt')
		returning id::text`, sourceID,
		"https://github.com/advisories/fixture-"+suffix,
		"test-operability/"+suffix+".json", digest[:], now.Add(-time.Hour)).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.advisory_collection_observations
		(source_id, source_registry_id, source_fetch_id, parent_raw_document_id,
		observed_at, state, entry_count, split_completed_at, processed_at)
		values ($1, $2, $3, $4::uuid, $5, 'processed', 0, $6, $6)`, sourceID,
		"test-operability-"+suffix, now.UnixNano(), rawID,
		now.Add(-time.Hour), now.Add(-time.Hour).Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.advisory_collection_observations
		(source_id, source_registry_id, source_fetch_id, parent_raw_document_id,
		observed_at)
		values ($1, $2, $3, $4::uuid, $5)`, sourceID,
		"test-operability-"+suffix, now.UnixNano()+1, rawID,
		now.Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	}

	after, err := collector.Collect(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.AdvisoryObservationsPending != before.AdvisoryObservationsPending+1 {
		t.Fatalf("pending observations = %d, want %d", after.AdvisoryObservationsPending, before.AdvisoryObservationsPending+1)
	}
	if after.AdvisoryObservationAge < 15*60 {
		t.Fatalf("oldest pending observation age = %f seconds, want at least 900", after.AdvisoryObservationAge)
	}
	alerts := operability.Evaluate(after, now)
	found := false
	for _, alert := range alerts {
		if alert.Name == "advisory_observation_stalled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stalled observation alert: %+v", alerts)
	}
}
