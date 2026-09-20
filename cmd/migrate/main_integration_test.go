package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database/schema"
)

func TestMigrateRecordsCompletionOnlyAfterRiverAndSourceSync(t *testing.T) {
	databaseURL := createMigrationFixtureDatabase(t)
	t.Chdir("../..")
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("GIT_SHA", "unknown")
	t.Setenv("SOURCE_REGISTRY_PATH", "sources/registry.yaml")
	t.Setenv("SOURCE_FIXTURES_PATH", "sources/fixtures.yaml")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run([]string{"up"}, logger); err != nil {
		t.Fatalf("initial migrate up: %v", err)
	}
	if err := run([]string{"status"}, logger); err != nil {
		t.Fatalf("initial completion status: %v", err)
	}

	// A new release with the same Goose version must not inherit the old
	// completion when its source-registry phase fails.
	newSHA := strings.Repeat("a", 40)
	t.Setenv("GIT_SHA", newSHA)
	t.Setenv("SOURCE_REGISTRY_PATH", "sources/missing-schema-guard-fixture.yaml")
	if err := run([]string{"up"}, logger); err == nil {
		t.Fatal("new release migration succeeded with missing source registry")
	}
	if err := run([]string{"status"}, logger); !errors.Is(err, schema.ErrPending) {
		t.Fatalf("new release status = %v, want pending completion", err)
	}
	t.Setenv("GIT_SHA", "unknown")
	if err := run([]string{"status"}, logger); err != nil {
		t.Fatalf("previous release lost its valid completion: %v", err)
	}
	t.Setenv("GIT_SHA", newSHA)
	t.Setenv("SOURCE_REGISTRY_PATH", "sources/registry.yaml")
	if err := run([]string{"up"}, logger); err != nil {
		t.Fatalf("new release migration recovery: %v", err)
	}
	if err := run([]string{"status"}, logger); err != nil {
		t.Fatalf("new release completion status: %v", err)
	}
}

func createMigrationFixtureDatabase(t *testing.T) string {
	t.Helper()
	if os.Getenv("APP_ENV") != "test" ||
		(os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "") {
		t.Skip("local test PostgreSQL is required")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(settings.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains([]string{"postgres", "localhost", "127.0.0.1"}, parsed.Hostname()) {
		t.Skip("migration fixtures are only allowed on local PostgreSQL")
	}
	configuration, err := pgxpool.ParseConfig(settings.URL)
	if err != nil {
		t.Fatal(err)
	}
	name := "migration_gate_" + uuid.NewString()[:8]
	maintenanceConfiguration := configuration.ConnConfig.Copy()
	maintenanceConfiguration.Database = "postgres"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, maintenanceConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := connection.Exec(ctx, "create database "+identifier); err != nil {
		t.Fatalf("create migration fixture database: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		admin, connectErr := pgx.ConnectConfig(cleanupContext, maintenanceConfiguration)
		if connectErr != nil {
			t.Errorf("connect to remove migration fixture: %v", connectErr)
			return
		}
		defer admin.Close(cleanupContext)
		if _, dropErr := admin.Exec(cleanupContext, "drop database "+identifier+" with (force)"); dropErr != nil {
			t.Errorf("remove migration fixture database: %v", dropErr)
		}
	})
	parsed.Path = "/" + name
	return parsed.String()
}
