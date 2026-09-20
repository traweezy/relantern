package schema_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database/schema"
	"github.com/traweezy/relantern/migrations"
)

func TestCompletionMarkerRequiresExactReleaseAndVersion(t *testing.T) {
	pool := createSchemaFixtureDatabase(t, fixtureDatabaseURL(t))
	versions, err := migrations.RequiredVersions()
	if err != nil {
		t.Fatal(err)
	}
	seedGooseHistory(t, pool, versions)
	seedSchemaFootprint(t, pool)
	if _, err := pool.Exec(context.Background(), `
		create table app.migration_completions (
			release_sha text not null,
			goose_version bigint not null,
			completed_at timestamptz not null default now(),
			primary key (release_sha, goose_version)
		)`); err != nil {
		t.Fatal(err)
	}
	guard, err := schema.New(pool, config.EnvironmentTest, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	checkPending := func(stage string) {
		t.Helper()
		if err := guard.Check(context.Background()); !errors.Is(err, schema.ErrPending) {
			t.Fatalf("%s: Check() = %v, want pending", stage, err)
		}
	}
	checkPending("no marker")
	latest := versions[len(versions)-1]
	if _, err := pool.Exec(context.Background(), `
		insert into app.migration_completions (release_sha, goose_version)
		values ($1, $2), ($3, $4)`, strings.Repeat("a", 40), latest, "unknown", versions[len(versions)-2]); err != nil {
		t.Fatal(err)
	}
	checkPending("other SHA and older version")
	if err := guard.RecordCompletion(context.Background()); err != nil {
		t.Fatalf("RecordCompletion() = %v", err)
	}
	if err := guard.Check(context.Background()); err != nil {
		t.Fatalf("matching completion was rejected: %v", err)
	}
	if err := guard.RecordCompletion(context.Background()); err != nil {
		t.Fatalf("idempotent RecordCompletion() = %v", err)
	}
	if err := guard.Check(context.Background()); err != nil {
		t.Fatalf("matching completion was lost on repeat: %v", err)
	}
}

func seedSchemaFootprint(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		`create schema app`,
		`create table app.raw_documents (
			id bigint, source_registry_id text, source_connector text,
			source_content_type text, ingestion_error_code text,
			ingestion_failed_at timestamptz
		)`,
		`create table app.source_fetches (object_key text)`,
		`create index idx_source_fetches_object_key_partial on app.source_fetches (object_key)`,
		`create index idx_raw_documents_parent_source_url_sha256 on app.raw_documents (id)`,
		`create index idx_raw_documents_entry_sha256 on app.raw_documents (id)`,
		`create index idx_raw_documents_source_entry_first_seen_at on app.raw_documents (id)`,
		`create index idx_raw_documents_parent_raw_document_id_partial on app.raw_documents (id)`,
		`create index idx_raw_documents_ingestion_failed_partial on app.raw_documents (id)`,
	} {
		if _, err := pool.Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
