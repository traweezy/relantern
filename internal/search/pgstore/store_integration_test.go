package pgstore_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/search"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
)

func TestStoreReturnsExactAndSemanticHybridResults(t *testing.T) {
	pool := openSearchDatabase(t)
	embeddingStore, err := embeddingstore.New(pool)
	if err != nil {
		t.Fatalf("embeddingstore.New() error = %v", err)
	}
	store, err := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	if err != nil {
		t.Fatalf("searchstore.New() error = %v", err)
	}
	now := time.Date(2045, time.January, 2, 12, 0, 0, 0, time.UTC)
	fixtures := []searchFixture{
		{
			title:       "PostgreSQL 18 high availability guide",
			packageName: "PostgreSQL",
			content:     "Streaming replication and managed failover improve database availability.",
			sourceTier:  "T0",
			vectorIndex: 0,
		},
		{
			title:       "Node.js event loop diagnostics",
			packageName: "Node.js",
			content:     "Runtime tracing explains asynchronous JavaScript latency and continuity.",
			sourceTier:  "T1",
			vectorIndex: 1,
		},
		{
			title:       "Package security advisory",
			packageName: "Example",
			content:     "A vulnerability advisory describes patched versions and mitigations.",
			sourceTier:  "T2",
			vectorIndex: 2,
		},
	}
	inserted := make([]searchRecord, 0, len(fixtures))
	for index, fixture := range fixtures {
		record := insertSearchRecord(t, pool, fixture, now.Add(time.Duration(index)*time.Minute))
		inserted = append(inserted, record)
		if err := store.IndexDocument(context.Background(), searchstore.IndexRequest{
			ItemID:            record.itemID,
			RevisionID:        record.revisionID,
			Summary:           fixture.content,
			EntityNames:       []string{fixture.packageName},
			NormalizedContent: fixture.content,
		}); err != nil {
			t.Fatalf("IndexDocument(%s) error = %v", fixture.title, err)
		}
		if _, insertedEmbedding, err := embeddingStore.Put(context.Background(), embeddingstore.PutRequest{
			EntityType: "item",
			EntityID:   record.itemID,
			RevisionID: record.revisionID,
			ModelID:    embedding.DefaultModelID,
			Input:      fixture.content,
			Vector:     searchBasisVector(fixture.vectorIndex),
		}); err != nil || !insertedEmbedding {
			t.Fatalf("Put(%s) = inserted %t, error %v", fixture.title, insertedEmbedding, err)
		}
	}
	cleanupSearchRecords(t, pool, inserted)

	exact, err := store.Search(context.Background(), searchstore.Request{
		Query:          "PostgreSQL",
		QueryEmbedding: searchBasisVector(2),
		After:          timePointer(now.Add(-time.Hour)),
		Before:         timePointer(now.Add(time.Hour)),
		Limit:          3,
	})
	if err != nil {
		t.Fatalf("exact Search() error = %v", err)
	}
	if len(exact) != 3 || exact[0].ItemID != inserted[0].itemID || exact[0].KeywordRank == nil {
		t.Fatalf("exact Search() = %+v", exact)
	}

	semantic, err := store.Search(context.Background(), searchstore.Request{
		Query:          "resilience continuity",
		QueryEmbedding: searchBasisVector(1),
		After:          timePointer(now.Add(-time.Hour)),
		Before:         timePointer(now.Add(time.Hour)),
		Limit:          3,
	})
	if err != nil {
		t.Fatalf("semantic Search() error = %v", err)
	}
	if len(semantic) != 3 || semantic[0].ItemID != inserted[1].itemID || semantic[0].SemanticRank == nil {
		t.Fatalf("semantic Search() = %+v", semantic)
	}

	filtered, err := store.Search(context.Background(), searchstore.Request{
		Query:          "advisory",
		QueryEmbedding: searchBasisVector(2),
		SourceTier:     "T2",
		After:          timePointer(now.Add(-time.Hour)),
		Before:         timePointer(now.Add(time.Hour)),
		Limit:          3,
	})
	if err != nil {
		t.Fatalf("filtered Search() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].ItemID != inserted[2].itemID {
		t.Fatalf("filtered Search() = %+v", filtered)
	}
}

func TestStoreRejectsInvalidIndexAndSearchBoundaries(t *testing.T) {
	pool := openSearchDatabase(t)
	store, err := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := store.IndexDocument(context.Background(), searchstore.IndexRequest{}); err == nil {
		t.Fatal("IndexDocument() accepted an empty request")
	}
	for _, request := range []searchstore.Request{
		{},
		{Query: "valid", QueryEmbedding: embedding.Vector{1}},
		{Query: "valid", QueryEmbedding: searchBasisVector(0), LifecycleState: "unknown"},
		{Query: "valid", QueryEmbedding: searchBasisVector(0), SourceTier: "T9"},
		{Query: "valid", QueryEmbedding: searchBasisVector(0), Limit: 101},
		{
			Query:          "valid",
			QueryEmbedding: searchBasisVector(0),
			After:          timePointer(time.Now()),
			Before:         timePointer(time.Now().Add(-time.Hour)),
		},
	} {
		if _, err := store.Search(context.Background(), request); err == nil {
			t.Errorf("Search() accepted %+v", request)
		}
	}
	if _, err := searchstore.New(nil, embedding.DefaultDimensions, search.DefaultRRFK); err == nil {
		t.Fatal("New() accepted nil database")
	}
	if _, err := searchstore.New(pool, 32, search.DefaultRRFK); err == nil {
		t.Fatal("New() accepted unsupported dimensions")
	}
	if _, err := searchstore.New(pool, embedding.DefaultDimensions, 0); err == nil {
		t.Fatal("New() accepted zero RRF k")
	}
}

type searchFixture struct {
	title       string
	packageName string
	content     string
	sourceTier  string
	vectorIndex int
}

type searchRecord struct {
	sourceID   string
	rawID      string
	revisionID string
	itemID     string
}

func insertSearchRecord(t *testing.T, pool *pgxpool.Pool, fixture searchFixture, observedAt time.Time) searchRecord {
	t.Helper()
	suffix := uuid.NewString()
	sourceID := "search-" + suffix
	if _, err := pool.Exec(context.Background(), `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at
		) values (
			$1, $2, $3, 'search-integration', 'owner', 'active',
			$4, 'link-and-excerpt', false, array['search']::text[], $5
		)`, sourceID, "Search integration", fixture.sourceTier, "https://"+sourceID+".example.test/", observedAt); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	rawDigest := sha256.Sum256([]byte("raw-" + suffix))
	var rawID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, source_published_at, content_policy
		) values (
			$1, $2, $3, $4, $5, $5, $5, 'link-and-excerpt'
		)
		returning id::text`,
		sourceID,
		"https://"+sourceID+".example.test/story",
		"raw/"+sourceID+"/"+fmt.Sprintf("%x", rawDigest)+".txt",
		rawDigest[:],
		observedAt,
	).Scan(&rawID); err != nil {
		t.Fatalf("insert raw document: %v", err)
	}
	normalizedDigest := sha256.Sum256([]byte(fixture.content))
	var revisionID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, author, language,
			source_published_at, normalized_bytes, outline, offset_map,
			warnings, change_kind, change_reason, material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'search-fixture', '1', $4, 'Relantern', 'en',
			$5, $6, '[]'::jsonb, '[]'::jsonb,
			'[]'::jsonb, 'initial', 'search integration fixture', false, $5
		)
		returning id::text`,
		rawID,
		normalizedDigest[:],
		"normalized/"+sourceID+"/"+fmt.Sprintf("%x", normalizedDigest)+".txt",
		fixture.title,
		observedAt,
		len(fixture.content),
	).Scan(&revisionID); err != nil {
		t.Fatalf("insert content revision: %v", err)
	}
	var itemID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title,
			normalized_author, package_name, version, slug, lifecycle_state,
			event_type, published_at, first_seen_at, status, simhash
		) values (
			$1::uuid, $2, $3, lower($3), 'relantern', $4, '1.0.0',
			$5, 'clustered', 'release', $6, $6, 'active', decode('0000000000000001', 'hex')
		)
		returning id::text`,
		revisionID,
		"https://"+sourceID+".example.test/story",
		fixture.title,
		fixture.packageName,
		"search-"+suffix,
		observedAt,
	).Scan(&itemID); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		) values ($1::uuid, $2::uuid, $3, 'primary', $4, 0)`,
		revisionID,
		itemID,
		"https://"+sourceID+".example.test/story",
		fixture.sourceTier,
	); err != nil {
		t.Fatalf("insert item source: %v", err)
	}
	return searchRecord{sourceID: sourceID, rawID: rawID, revisionID: revisionID, itemID: itemID}
}

func cleanupSearchRecords(t *testing.T, pool *pgxpool.Pool, records []searchRecord) {
	t.Helper()
	t.Cleanup(func() {
		for _, record := range records {
			_, _ = pool.Exec(context.Background(), "delete from app.embeddings where entity_id = $1::uuid", record.itemID)
			_, _ = pool.Exec(context.Background(), "delete from app.item_sources where item_id = $1::uuid", record.itemID)
			_, _ = pool.Exec(context.Background(), "delete from app.items where id = $1::uuid", record.itemID)
			_, _ = pool.Exec(context.Background(), "delete from app.content_revisions where id = $1::uuid", record.revisionID)
			_, _ = pool.Exec(context.Background(), "delete from app.raw_documents where id = $1::uuid", record.rawID)
			_, _ = pool.Exec(context.Background(), "delete from app.sources where id = $1", record.sourceID)
		}
	})
}

func searchBasisVector(index int) embedding.Vector {
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[index] = 1
	return vector
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func openSearchDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for search integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
