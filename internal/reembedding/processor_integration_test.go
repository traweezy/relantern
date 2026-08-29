package reembedding_test

import (
	"context"
	"crypto/sha256"
	"errors"
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
	"github.com/traweezy/relantern/internal/reembedding"
	"github.com/traweezy/relantern/internal/search"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
)

type fixtureReader struct {
	payload []byte
	err     error
}

func (reader fixtureReader) Read(context.Context, string, int64) ([]byte, error) {
	return append([]byte(nil), reader.payload...), reader.err
}

type fixtureEmbedder struct {
	vector embedding.Vector
	err    error
}

type failingEmbeddingWriter struct {
	err error
}

func (writer failingEmbeddingWriter) Put(
	context.Context,
	embeddingstore.PutRequest,
) (string, bool, error) {
	return "", false, writer.err
}

type failingSearchIndexer struct {
	err error
}

func (indexer failingSearchIndexer) IndexDocument(
	context.Context,
	searchstore.IndexRequest,
) error {
	return indexer.err
}

func (embedder fixtureEmbedder) Embed(context.Context, string) (embedding.Vector, error) {
	return append(embedding.Vector(nil), embedder.vector...), embedder.err
}

type reembeddingRecord struct {
	sourceID   string
	rawID      string
	revisionID string
	itemID     string
	content    string
}

func TestProcessorReembedsAndIndexesCurrentRevisionIdempotently(t *testing.T) {
	pool := openReembeddingDatabase(t)
	record := insertReembeddingRecord(t, pool)
	cleanupReembeddingRecord(t, pool, record)
	embeddingStore, err := embeddingstore.New(pool)
	if err != nil {
		t.Fatalf("embeddingstore.New() error = %v", err)
	}
	searchStore, err := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	if err != nil {
		t.Fatalf("searchstore.New() error = %v", err)
	}
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[0] = 1
	processor, err := reembedding.New(
		pool,
		fixtureReader{payload: []byte(record.content)},
		fixtureEmbedder{vector: vector},
		embeddingStore,
		searchStore,
		embedding.DefaultModelID,
		embedding.DefaultDimensions,
	)
	if err != nil {
		t.Fatalf("reembedding.New() error = %v", err)
	}
	request := reembedding.Request{
		EntityType: "item",
		EntityID:   record.itemID,
		RevisionID: record.revisionID,
		ModelID:    embedding.DefaultModelID,
	}
	first, err := processor.Process(context.Background(), request)
	if err != nil || !first.Inserted || first.EmbeddingID == "" || first.Obsolete {
		t.Fatalf("first Process() = %+v, %v", first, err)
	}
	second, err := processor.Process(context.Background(), request)
	if err != nil || second.Inserted || second.EmbeddingID != first.EmbeddingID {
		t.Fatalf("second Process() = %+v, %v", second, err)
	}

	var embeddings int
	var indexedContent string
	if err := pool.QueryRow(context.Background(), `
		select
			(select count(*) from app.embeddings where entity_id = $1::uuid),
			normalized_content
		from app.search_documents
		where item_id = $1::uuid`, record.itemID).Scan(&embeddings, &indexedContent); err != nil {
		t.Fatalf("inspect re-embedding projection: %v", err)
	}
	if embeddings != 1 || indexedContent != record.content {
		t.Fatalf("embeddings = %d, indexed content = %q", embeddings, indexedContent)
	}

	request.RevisionID = uuid.NewString()
	obsolete, err := processor.Process(context.Background(), request)
	if err != nil || !obsolete.Obsolete {
		t.Fatalf("obsolete Process() = %+v, %v", obsolete, err)
	}
	request.RevisionID = record.revisionID
	request.ModelID = "different-model"
	if _, err := processor.Process(context.Background(), request); !errors.Is(err, reembedding.ErrModelMismatch) {
		t.Fatalf("model mismatch error = %v", err)
	}
	request.ModelID = embedding.DefaultModelID
	request.EntityType = "story_cluster"
	if _, err := processor.Process(context.Background(), request); !errors.Is(err, reembedding.ErrInvalidTarget) {
		t.Fatalf("invalid target error = %v", err)
	}
}

func TestProcessorRejectsNormalizedContentDigestMismatch(t *testing.T) {
	pool := openReembeddingDatabase(t)
	record := insertReembeddingRecord(t, pool)
	cleanupReembeddingRecord(t, pool, record)
	embeddingStore, _ := embeddingstore.New(pool)
	searchStore, _ := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[0] = 1
	processor, err := reembedding.New(
		pool,
		fixtureReader{payload: []byte("tampered content")},
		fixtureEmbedder{vector: vector},
		embeddingStore,
		searchStore,
		embedding.DefaultModelID,
		embedding.DefaultDimensions,
	)
	if err != nil {
		t.Fatalf("reembedding.New() error = %v", err)
	}
	_, err = processor.Process(context.Background(), reembedding.Request{
		EntityType: "item",
		EntityID:   record.itemID,
		RevisionID: record.revisionID,
		ModelID:    embedding.DefaultModelID,
	})
	if !errors.Is(err, reembedding.ErrRevisionIntegrity) {
		t.Fatalf("Process() error = %v, want ErrRevisionIntegrity", err)
	}
}

func TestProcessorClassifiesBoundaryAndDependencyFailures(t *testing.T) {
	pool := openReembeddingDatabase(t)
	record := insertReembeddingRecord(t, pool)
	cleanupReembeddingRecord(t, pool, record)
	embeddingStore, _ := embeddingstore.New(pool)
	searchStore, _ := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[0] = 1
	validReader := fixtureReader{payload: []byte(record.content)}
	validEmbedder := fixtureEmbedder{vector: vector}

	if _, err := reembedding.New(
		nil,
		validReader,
		validEmbedder,
		embeddingStore,
		searchStore,
		embedding.DefaultModelID,
		embedding.DefaultDimensions,
	); err == nil {
		t.Fatal("New() accepted a nil database")
	}
	if _, err := reembedding.New(
		pool,
		validReader,
		validEmbedder,
		embeddingStore,
		searchStore,
		embedding.DefaultModelID,
		32,
	); err == nil {
		t.Fatal("New() accepted unsupported dimensions")
	}

	baseRequest := reembedding.Request{
		EntityType: "item",
		EntityID:   record.itemID,
		RevisionID: record.revisionID,
		ModelID:    embedding.DefaultModelID,
	}
	processor := newFixtureProcessor(
		t,
		pool,
		validReader,
		validEmbedder,
		embeddingStore,
		searchStore,
	)
	invalidID := baseRequest
	invalidID.EntityID = "invalid"
	if _, err := processor.Process(context.Background(), invalidID); !errors.Is(err, reembedding.ErrInvalidTarget) {
		t.Fatalf("invalid entity ID error = %v", err)
	}
	invalidRevision := baseRequest
	invalidRevision.RevisionID = "invalid"
	if _, err := processor.Process(context.Background(), invalidRevision); !errors.Is(err, reembedding.ErrInvalidTarget) {
		t.Fatalf("invalid revision ID error = %v", err)
	}
	missing := baseRequest
	missing.EntityID = uuid.NewString()
	if _, err := processor.Process(context.Background(), missing); !errors.Is(err, reembedding.ErrInvalidTarget) {
		t.Fatalf("missing item error = %v", err)
	}

	tests := []struct {
		name       string
		reader     fixtureReader
		embedder   fixtureEmbedder
		embeddings reembedding.EmbeddingWriter
		search     reembedding.SearchIndexer
	}{
		{
			name:       "object read",
			reader:     fixtureReader{err: errors.New("object unavailable")},
			embedder:   validEmbedder,
			embeddings: embeddingStore,
			search:     searchStore,
		},
		{
			name:       "provider",
			reader:     validReader,
			embedder:   fixtureEmbedder{err: errors.New("provider unavailable")},
			embeddings: embeddingStore,
			search:     searchStore,
		},
		{
			name:       "invalid provider vector",
			reader:     validReader,
			embedder:   fixtureEmbedder{vector: embedding.Vector{1}},
			embeddings: embeddingStore,
			search:     searchStore,
		},
		{
			name:       "embedding store",
			reader:     validReader,
			embedder:   validEmbedder,
			embeddings: failingEmbeddingWriter{err: errors.New("database unavailable")},
			search:     searchStore,
		},
		{
			name:       "search index",
			reader:     validReader,
			embedder:   validEmbedder,
			embeddings: embeddingStore,
			search:     failingSearchIndexer{err: errors.New("index unavailable")},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := newFixtureProcessor(
				t,
				pool,
				test.reader,
				test.embedder,
				test.embeddings,
				test.search,
			)
			if _, err := processor.Process(context.Background(), baseRequest); err == nil {
				t.Fatal("Process() ignored dependency failure")
			}
		})
	}
}

func newFixtureProcessor(
	t *testing.T,
	pool *pgxpool.Pool,
	reader fixtureReader,
	embedder fixtureEmbedder,
	embeddings reembedding.EmbeddingWriter,
	searchIndex reembedding.SearchIndexer,
) *reembedding.Processor {
	t.Helper()
	processor, err := reembedding.New(
		pool,
		reader,
		embedder,
		embeddings,
		searchIndex,
		embedding.DefaultModelID,
		embedding.DefaultDimensions,
	)
	if err != nil {
		t.Fatalf("reembedding.New() error = %v", err)
	}
	return processor
}

func insertReembeddingRecord(t *testing.T, pool *pgxpool.Pool) reembeddingRecord {
	t.Helper()
	suffix := uuid.NewString()
	sourceID := "reembed-" + suffix
	now := time.Date(2046, time.January, 2, 12, 0, 0, 0, time.UTC)
	content := "PostgreSQL standby promotion and replication lag determine database recovery readiness."
	if _, err := pool.Exec(context.Background(), `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at
		) values (
			$1, 'Re-embedding integration', 'T1', 'reembedding-integration',
			'owner', 'active', $2, 'link-and-excerpt', false,
			array['search']::text[], $3
		)`, sourceID, "https://"+sourceID+".example.test/", now); err != nil {
		t.Fatalf("insert re-embedding source: %v", err)
	}
	rawDigest := sha256.Sum256([]byte("raw-" + suffix))
	var rawID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, source_published_at, content_policy
		) values (
			$1, $2, $3, $4, $5, $5, $5, 'link-and-excerpt'
		) returning id::text`,
		sourceID,
		"https://"+sourceID+".example.test/story",
		fmt.Sprintf("raw/%s/2046/01/02/%x.txt", sourceID, rawDigest),
		rawDigest[:],
		now,
	).Scan(&rawID); err != nil {
		t.Fatalf("insert re-embedding raw document: %v", err)
	}
	normalizedDigest := sha256.Sum256([]byte(content))
	var revisionID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, author, language,
			source_published_at, normalized_bytes, outline, offset_map,
			warnings, change_kind, change_reason, material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'reembedding-fixture', '1',
			'PostgreSQL recovery readiness', 'Relantern', 'en', $4, $5,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial',
			're-embedding integration fixture', false, $4
		) returning id::text`,
		rawID,
		normalizedDigest[:],
		fmt.Sprintf("normalized/%s/%x.txt", sourceID, normalizedDigest),
		now,
		len(content),
	).Scan(&revisionID); err != nil {
		t.Fatalf("insert re-embedding revision: %v", err)
	}
	var itemID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title,
			normalized_author, package_name, version, slug, lifecycle_state,
			event_type, published_at, first_seen_at, status, simhash
		) values (
			$1::uuid, $2, 'PostgreSQL recovery readiness',
			'postgresql recovery readiness', 'relantern', 'PostgreSQL', '18',
			$3, 'clustered', 'guide', $4, $4, 'active', decode('0000000000000001', 'hex')
		) returning id::text`,
		revisionID,
		"https://"+sourceID+".example.test/story",
		"reembed-"+suffix,
		now,
	).Scan(&itemID); err != nil {
		t.Fatalf("insert re-embedding item: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		) values ($1::uuid, $2::uuid, $3, 'primary', 'T1', 0)`,
		revisionID,
		itemID,
		"https://"+sourceID+".example.test/story",
	); err != nil {
		t.Fatalf("insert re-embedding item source: %v", err)
	}
	return reembeddingRecord{
		sourceID: sourceID, rawID: rawID, revisionID: revisionID, itemID: itemID, content: content,
	}
}

func cleanupReembeddingRecord(t *testing.T, pool *pgxpool.Pool, record reembeddingRecord) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "delete from app.embeddings where entity_id = $1::uuid", record.itemID)
		_, _ = pool.Exec(ctx, "delete from app.item_sources where item_id = $1::uuid", record.itemID)
		_, _ = pool.Exec(ctx, "delete from app.items where id = $1::uuid", record.itemID)
		_, _ = pool.Exec(ctx, "delete from app.content_revisions where id = $1::uuid", record.revisionID)
		_, _ = pool.Exec(ctx, "delete from app.raw_documents where id = $1::uuid", record.rawID)
		_, _ = pool.Exec(ctx, "delete from app.sources where id = $1", record.sourceID)
	})
}

func openReembeddingDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for re-embedding integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("config.LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
