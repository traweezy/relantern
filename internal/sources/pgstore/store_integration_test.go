package pgstore_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/sources"
)

func TestRegistryMirrorMatchesReviewedConfiguration(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for the registry mirror integration test")
	}
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	assertCount(t, pool, "select count(*) from app.sources", len(registry.Sources)+len(registry.Repositories))
	assertCount(t, pool, "select count(*) from app.source_endpoints", len(registry.Endpoints()))
	assertCount(t, pool, "select count(*) from app.github_repositories", len(registry.Repositories))
	assertCount(t, pool, "select count(*) from app.sources where validation_state = 'paused'", len(registry.Sources)+len(registry.Repositories))
	assertCount(t, pool, "select count(*) from app.source_endpoints where next_poll_at is null", len(registry.Endpoints()))
	assertCount(t, pool, "select count(*) from app.source_endpoints where health_state = 'paused'", len(registry.Endpoints()))
}

func assertCount(t *testing.T, pool *pgxpool.Pool, query string, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatalf("count query %q error = %v", query, err)
	}
	if count != expected {
		t.Fatalf("count query %q = %d, want %d", query, count, expected)
	}
}
