package schema_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database/schema"
	"github.com/traweezy/relantern/migrations"
)

// Fixture databases are created only in the explicitly local test profile.
// They exercise PostgreSQL catalog queries without modifying the app database.
func TestGuardRejectsIncompleteAndDriftedPostgreSQL(t *testing.T) {
	databaseURL := fixtureDatabaseURL(t)
	versions, err := migrations.RequiredVersions()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		omitLast bool
		want     error
	}{
		{name: "latest migration pending", omitLast: true, want: schema.ErrPending},
		{name: "latest recorded but schema absent", want: schema.ErrDrift},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := createSchemaFixtureDatabase(t, databaseURL)
			appliedVersions := versions
			if test.omitLast {
				appliedVersions = versions[:len(versions)-1]
			}
			seedGooseHistory(t, pool, appliedVersions)
			guard, err := schema.New(pool, config.EnvironmentTest, "unknown")
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Check(context.Background()); !errors.Is(err, test.want) {
				t.Fatalf("Check() = %v, want %v", err, test.want)
			}
		})
	}
}

func fixtureDatabaseURL(t *testing.T) string {
	t.Helper()
	if os.Getenv("APP_ENV") != "test" ||
		(os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "") {
		t.Skip("isolated PostgreSQL fixture databases require the local test profile")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(settings.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains([]string{"postgres", "localhost", "127.0.0.1"}, address.Hostname()) {
		t.Skip("fixture databases are only allowed on local PostgreSQL")
	}
	return settings.URL
}

func seedGooseHistory(t *testing.T, pool *pgxpool.Pool, versions []int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		create table public.goose_db_version (
			id bigint generated always as identity primary key,
			version_id bigint not null,
			is_applied boolean not null
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into public.goose_db_version (version_id, is_applied)
		select version, true from unnest($1::bigint[]) as version`, versions); err != nil {
		t.Fatal(err)
	}
}

func createSchemaFixtureDatabase(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	name := "schema_guard_" + uuid.NewString()[:8]
	maintenanceConfig := config.ConnConfig.Copy()
	maintenanceConfig.Database = "postgres"
	maintenance, err := pgx.ConnectConfig(ctx, maintenanceConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close(ctx)
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := maintenance.Exec(ctx, "create database "+identifier); err != nil {
		t.Fatalf("create isolated schema fixture database: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		admin, connectErr := pgx.ConnectConfig(cleanupContext, maintenanceConfig)
		if connectErr != nil {
			t.Errorf("connect to drop isolated schema fixture: %v", connectErr)
			return
		}
		defer admin.Close(cleanupContext)
		if _, dropErr := admin.Exec(cleanupContext, "drop database "+identifier+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated schema fixture database: %v", dropErr)
		}
	})
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(fmt.Errorf("connect isolated schema fixture database: %w", err))
	}
	t.Cleanup(pool.Close)
	return pool
}
