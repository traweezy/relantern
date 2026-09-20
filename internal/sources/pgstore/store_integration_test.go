package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/sources/pgstore"
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

	sourceIDs := make([]string, 0, len(registry.Sources)+len(registry.Repositories))
	repositoryIDs := make([]string, 0, len(registry.Repositories))
	for _, source := range registry.Sources {
		sourceIDs = append(sourceIDs, source.ID)
	}
	for _, repository := range registry.Repositories {
		sourceIDs = append(sourceIDs, repository.ID)
		repositoryIDs = append(repositoryIDs, repository.ID)
	}
	endpoints := registry.Endpoints()
	endpointIDs := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpointIDs = append(endpointIDs, endpoint.ID)
	}

	assertCount(t, pool, "select count(*) from app.sources where id = any($1::text[])", len(sourceIDs), sourceIDs)
	assertCount(t, pool, "select count(*) from app.source_endpoints where registry_id = any($1::text[])", len(endpointIDs), endpointIDs)
	assertCount(t, pool, "select count(*) from app.github_repositories where source_id = any($1::text[])", len(repositoryIDs), repositoryIDs)
	assertCount(t, pool, "select count(*) from app.sources where id = any($1::text[]) and validation_state = 'paused'", len(sourceIDs), sourceIDs)
	assertCount(t, pool, "select count(*) from app.source_endpoints where registry_id = any($1::text[]) and next_poll_at is null", len(endpointIDs), endpointIDs)
	assertCount(t, pool, "select count(*) from app.source_endpoints where registry_id = any($1::text[]) and health_state = 'paused'", len(endpointIDs), endpointIDs)
}

func TestRegistrySyncPreservesFailedEndpointUntilExplicitResume(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for registry sync integration")
	}
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	nextPoll := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `update app.source_endpoints
		set health_state = 'failed', next_poll_at = null where registry_id = 'go-blog'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update app.source_endpoints
		set health_state = 'healthy', next_poll_at = $1 where registry_id = 'go-security'`, nextPoll); err != nil {
		t.Fatal(err)
	}
	registry.Enabled = true
	for range 2 {
		if err := (pgstore.Store{}).Sync(ctx, tx, registry); err != nil {
			t.Fatal(err)
		}
	}
	var failedHealth, healthyHealth string
	var scheduledAt time.Time
	if err := tx.QueryRow(ctx, `select health_state from app.source_endpoints
		where registry_id = 'go-blog'`).Scan(&failedHealth); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select health_state, next_poll_at from app.source_endpoints
		where registry_id = 'go-security'`).Scan(&healthyHealth, &scheduledAt); err != nil {
		t.Fatal(err)
	}
	if failedHealth != "failed" || healthyHealth != "healthy" || !scheduledAt.Equal(nextPoll) {
		t.Fatalf("registry sync changed runtime health: failed=%q healthy=%q next=%s", failedHealth, healthyHealth, scheduledAt)
	}
	registry.Enabled = false
	if err := (pgstore.Store{}).Sync(ctx, tx, registry); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select health_state from app.source_endpoints
		where registry_id = 'go-blog'`).Scan(&failedHealth); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select health_state from app.source_endpoints
		where registry_id = 'go-security'`).Scan(&healthyHealth); err != nil {
		t.Fatal(err)
	}
	if failedHealth != "failed" || healthyHealth != "paused" {
		t.Fatalf("disabled registry health: failed=%q healthy=%q", failedHealth, healthyHealth)
	}
	registry.Enabled = true
	if err := (pgstore.Store{}).Sync(ctx, tx, registry); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select health_state from app.source_endpoints
		where registry_id = 'go-blog'`).Scan(&failedHealth); err != nil {
		t.Fatal(err)
	}
	var resumedAt *time.Time
	if err := tx.QueryRow(ctx, `select health_state, next_poll_at from app.source_endpoints
		where registry_id = 'go-security'`).Scan(&healthyHealth, &resumedAt); err != nil {
		t.Fatal(err)
	}
	if failedHealth != "failed" || healthyHealth != "unverified" || resumedAt != nil {
		t.Fatalf("re-enabled registry health: failed=%q resumed=%q next=%v", failedHealth, healthyHealth, resumedAt)
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, query string, expected int, arguments ...any) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, arguments...).Scan(&count); err != nil {
		t.Fatalf("count query %q error = %v", query, err)
	}
	if count != expected {
		t.Fatalf("count query %q = %d, want %d", query, count, expected)
	}
}
