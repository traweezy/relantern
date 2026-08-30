package operability_test

import (
	"bytes"
	"context"
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
	defer pool.Close()

	collector, err := operability.NewCollector(pool)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	now := time.Now().UTC()
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
