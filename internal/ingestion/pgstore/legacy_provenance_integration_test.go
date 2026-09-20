package pgstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

func TestLoadLegacyRawDocumentRequiresUniqueOriginalFetch(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for legacy provenance integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sourceID := "test-legacy-provenance-" + uuid.NewString()
	registryA := sourceID + "-a"
	registryB := sourceID + "-b"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
		pool.Close()
	})
	firstSeen := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	firstFetched := firstSeen.Add(time.Second)
	if _, err := pool.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Legacy provenance fixture', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, firstSeen); err != nil {
		t.Fatal(err)
	}
	for index, registryID := range []string{registryA, registryB} {
		endpointURL := "https://example.com/feed-a.xml"
		if index == 1 {
			endpointURL = "https://example.com/feed-b.xml"
		}
		if _, err := pool.Exec(ctx, `insert into app.source_endpoints (
			registry_id, source_id, connector, url, poll_interval, priority,
			robots_policy, expected_content_types, max_response_bytes, fixture_suite
		) values ($1, $2, 'rss', $3, interval '5 minutes',
			'normal', 'feed', array['application/rss+xml'], 1048576, 'rss-v1')`, registryID, sourceID, endpointURL); err != nil {
			t.Fatal(err)
		}
	}
	insertRaw := func(url string, body string) (string, string, [sha256.Size]byte) {
		t.Helper()
		key := "raw/" + sourceID + "/" + uuid.NewString() + ".xml"
		digest := sha256.Sum256([]byte(body))
		var id string
		if err := pool.QueryRow(ctx, `insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256,
			first_seen_at, first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $6, 'link-and-excerpt')
		returning id::text`, sourceID, url, key, digest[:], firstSeen, firstFetched).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id, key, digest
	}
	insertFetch := func(registryID, url, key string, digest [sha256.Size]byte, attempted, completed time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `insert into app.source_fetches (
			endpoint_id, attempted_at, completed_at, outcome, status_code,
			final_url, content_type, duration_ms, raw_sha256, object_key
		) select endpoint.id, $2, $3, 'stored', 200, $4,
			'application/rss+xml', 1000, $5, $6
		from app.source_endpoints endpoint where endpoint.registry_id = $1`,
			registryID, attempted, completed, url, digest[:], key)
		if err != nil {
			t.Fatal(err)
		}
	}
	store := &Store{pool: pool}
	const url = "https://example.com/feed.xml"
	firstID, firstKey, firstDigest := insertRaw(url, "first")
	insertFetch(registryA, url, firstKey, firstDigest, firstSeen, firstFetched)
	insertFetch(registryB, url, firstKey, firstDigest,
		firstSeen.Add(time.Minute), firstFetched.Add(time.Minute))
	if document, err := store.LoadRawDocument(ctx, registryA, firstID); err != nil || document.ID != firstID {
		t.Fatalf("original endpoint load = %+v, %v", document, err)
	}
	if _, err := store.LoadRawDocument(ctx, registryB, firstID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("later endpoint gained original provenance: %v", err)
	}

	ambiguousID, ambiguousKey, ambiguousDigest := insertRaw(url, "ambiguous")
	insertFetch(registryA, url, ambiguousKey, ambiguousDigest, firstSeen, firstFetched)
	insertFetch(registryB, url, ambiguousKey, ambiguousDigest, firstSeen, firstFetched)
	for _, registryID := range []string{registryA, registryB} {
		if _, err := store.LoadRawDocument(ctx, registryID, ambiguousID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("ambiguous creator was attributed to %s: %v", registryID, err)
		}
	}

	mismatchID, mismatchKey, _ := insertRaw(url, "digest mismatch")
	insertFetch(registryA, url, mismatchKey, firstDigest, firstSeen, firstFetched)
	if _, err := store.LoadRawDocument(ctx, registryA, mismatchID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("different digest gained provenance: %v", err)
	}
}

func TestAmbiguousLegacyRefetchStopsWithoutClaimingOwner(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for legacy provenance integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	const queue = "test_legacy_refetch"
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	ctx := context.Background()
	sourceID := "test-legacy-refetch-" + uuid.NewString()
	registryA := sourceID + "-a"
	registryB := sourceID + "-b"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job where queue = $1
			and args ->> 'registryId' in ($2, $3)`, queue, registryA, registryB)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
		pool.Close()
	})
	firstSeen := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	firstFetched := firstSeen.Add(time.Second)
	later := firstSeen.Add(24 * time.Hour)
	store, err := NewWithEntries(pool, jobs, newEntryObjects(), clock.NewFixed(later))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Legacy refetch fixture', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, firstSeen); err != nil {
		t.Fatal(err)
	}
	for index, registryID := range []string{registryA, registryB} {
		endpointURL := "https://example.com/feed-a.xml"
		if index == 1 {
			endpointURL = "https://example.com/feed-b.xml"
		}
		if _, err := pool.Exec(ctx, `insert into app.source_endpoints (
			registry_id, source_id, connector, url, poll_interval, priority,
			robots_policy, expected_content_types, max_response_bytes, fixture_suite
		) values ($1, $2, 'rss', $3, interval '5 minutes', 'normal',
			'feed', array['application/rss+xml'], 1048576, 'rss-v1')`,
			registryID, sourceID, endpointURL); err != nil {
			t.Fatal(err)
		}
	}
	const finalURL = "https://example.com/shared.xml"
	digest := sha256.Sum256([]byte("ambiguous legacy evidence"))
	key := "raw/" + sourceID + "/shared.xml"
	var rawID string
	if err := pool.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, raw_pruned_at
	) values ($1, $2, $3, $4, $5, 'link-and-excerpt', $6)
	returning id::text`, sourceID, finalURL, digest[:], firstSeen, firstFetched,
		later).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	for _, registryID := range []string{registryA, registryB} {
		if _, err := pool.Exec(ctx, `insert into app.source_fetches (
			endpoint_id, attempted_at, completed_at, outcome, status_code,
			final_url, content_type, duration_ms, raw_sha256, object_key
		) select endpoint.id, $2, $3, 'stored', 200, $4,
			'application/rss+xml', 1000, $5, $6
		from app.source_endpoints endpoint where endpoint.registry_id = $1`,
			registryID, firstSeen, firstFetched, finalURL, digest[:], key); err != nil {
			t.Fatal(err)
		}
	}
	endpoint, err := store.LoadEndpoint(ctx, registryB)
	if err != nil || endpoint == nil {
		t.Fatalf("load later endpoint = %+v, %v", endpoint, err)
	}
	result := fetcher.Result{
		Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: key,
		Attempts: []fetcher.Attempt{{
			AttemptedAt: later, CompletedAt: later.Add(time.Second),
			StatusCode: 200, FinalURL: finalURL, ContentType: "application/rss+xml",
		}},
	}
	if err := store.RecordFetch(ctx, *endpoint, result, nil); err == nil {
		t.Fatal("ambiguous legacy refetch unexpectedly scheduled a parser")
	}
	var owner *string
	var marker, restoredKey, health string
	if err := pool.QueryRow(ctx, `select raw.source_registry_id, raw.ingestion_error_code,
		raw.object_key, endpoint.health_state
	from app.raw_documents raw
	join app.source_endpoints endpoint on endpoint.registry_id = $2
	where raw.id = $1::uuid`, rawID, registryB).Scan(&owner, &marker, &restoredKey, &health); err != nil {
		t.Fatal(err)
	}
	if owner != nil || marker != "provenance_unresolved" || restoredKey != key || health != "failed" {
		t.Fatalf("ambiguous refetch state: owner=%v marker=%q key=%q health=%q",
			owner, marker, restoredKey, health)
	}
	var parseJobs int
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where kind = $1 and args ->> 'rawDocumentId' = $2`,
		jobqueue.ParseRawDocumentKind, rawID).Scan(&parseJobs); err != nil {
		t.Fatal(err)
	}
	if parseJobs != 0 {
		t.Fatalf("ambiguous raw has %d parse jobs", parseJobs)
	}
	document := ingestion.RawDocument{
		ID: rawID, SourceID: sourceID, Connector: sources.ConnectorRSS,
		URL: finalURL, ContentPolicy: fetcher.ContentPolicyLinkAndExcerpt,
	}
	if err := store.RecordIngestionFailure(ctx, document, registryB, parsing.ErrorInvalidDocument); err == nil {
		t.Fatal("direct failure asserted ambiguous provenance")
	}
	if err := store.ClearIngestionFailure(ctx, document, registryB); err == nil {
		t.Fatal("direct clear asserted ambiguous provenance")
	}
	if err := pool.QueryRow(ctx, `select source_registry_id, ingestion_error_code
		from app.raw_documents where id = $1::uuid`, rawID).Scan(&owner, &marker); err != nil {
		t.Fatal(err)
	}
	if owner != nil || marker != "provenance_unresolved" {
		t.Fatalf("direct failure or clear changed unresolved raw: owner=%v marker=%q", owner, marker)
	}
}
