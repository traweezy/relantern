package pgstore

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

func TestReplaySelectionSkipsActiveJobsAndEmptyCollectionClearsMarker(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for source lifecycle integration")
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
	const queue = "test_source_lifecycle"
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store, err := NewWithEntries(pool, jobs, newEntryObjects(), clock.NewFixed(now))
	if err != nil {
		t.Fatal(err)
	}
	sourceID := "test-lifecycle-" + uuid.NewString()
	registryID := sourceID + "-rss"
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job
			where queue = $1 and args ->> 'registryId' = $2`, queue, registryID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
	})
	if _, err := pool.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Lifecycle fixture', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.source_endpoints (
		registry_id, source_id, connector, url, poll_interval, priority,
		robots_policy, expected_content_types, max_response_bytes, fixture_suite,
		health_state, next_poll_at
	) values ($1, $2, 'rss', 'https://example.com/feed.xml', interval '5 minutes',
		'normal', 'feed', array['application/rss+xml'], 1048576, 'rss-v1',
		'healthy', $3)`, registryID, sourceID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var rawIDs [2]string
	for index := range rawIDs {
		digest := sha256.Sum256([]byte{byte(index + 1)})
		key := "raw/" + sourceID + "/2026/09/19/" + uuid.NewString() + ".xml"
		if err := pool.QueryRow(ctx, `insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, content_policy, source_registry_id, source_connector,
			source_content_type, ingestion_error_code, ingestion_failed_at
		) values ($1, 'https://example.com/feed.xml', $2, $3, $4, $4,
			'link-and-excerpt', $5, 'rss', 'application/rss+xml', 'pending_entries', $4)
		returning id::text`, sourceID, key, digest[:], now.Add(time.Duration(index)*time.Second), registryID).Scan(&rawIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
		RawDocumentID: rawIDs[0], RegistryID: registryID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 1); err != nil || count != 1 {
		t.Fatalf("later unresolved marker starved behind active job: count=%d error=%v", count, err)
	}
	var laterJobs int
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2 and args ->> 'rawDocumentId' = $3`,
		queue, jobqueue.ParseRawDocumentKind, rawIDs[1]).Scan(&laterJobs); err != nil {
		t.Fatal(err)
	}
	if laterJobs != 1 {
		t.Fatalf("later marker has %d parse jobs, want 1", laterJobs)
	}
	if count, err := store.ScheduleDue(ctx, now, 1); err != nil || count != 0 {
		t.Fatalf("active replay jobs were duplicated: count=%d error=%v", count, err)
	}
	parent := ingestion.RawDocument{
		ID: rawIDs[1], SourceID: sourceID, Connector: sources.ConnectorRSS,
		URL: "https://example.com/feed.xml", ContentPolicy: "link-and-excerpt",
	}
	if count, err := store.RecordEntries(ctx, parent, registryID, []parsing.Entry{}); err != nil || count != 0 {
		t.Fatalf("record valid empty collection: count=%d error=%v", count, err)
	}
	var marker *string
	if err := pool.QueryRow(ctx, `select ingestion_error_code from app.raw_documents
		where id = $1::uuid`, rawIDs[1]).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != nil {
		t.Fatalf("empty collection retained replay marker %q", *marker)
	}
}
