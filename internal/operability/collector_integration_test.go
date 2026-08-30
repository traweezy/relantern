package operability_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

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
