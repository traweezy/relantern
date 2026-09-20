package pgstore

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/sources"
)

type handoffFixture struct {
	pool       *pgxpool.Pool
	store      *Store
	sourceID   string
	registryID string
	rawID      string
	revisionID string
	itemID     string
	clusterID  string
	queue      string
}

func newHandoffFixture(t *testing.T) handoffFixture {
	t.Helper()
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	queue := "test_source_handoff"
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store, err := NewWithEntries(pool, jobs, newEntryObjects(), clock.NewFixed(now))
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	fixture := handoffFixture{pool: pool, store: store,
		sourceID: "test-handoff-" + uuid.NewString(), queue: queue}
	fixture.registryID = fixture.sourceID + "-page"
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job where queue = $1
			and args ->> 'itemId' = $2`, queue, fixture.itemID)
		_, _ = pool.Exec(ctx, `delete from river.river_job where queue = $1
			and args ->> 'entityId' = $2`, queue, fixture.itemID)
		_, _ = pool.Exec(ctx, `delete from app.dedupe_decisions where revision_id = nullif($1, '')::uuid`, fixture.revisionID)
		_, _ = pool.Exec(ctx, `delete from app.cluster_members where item_id = nullif($1, '')::uuid`, fixture.itemID)
		_, _ = pool.Exec(ctx, `delete from app.story_clusters where id = nullif($1, '')::uuid`, fixture.clusterID)
		_, _ = pool.Exec(ctx, `delete from app.item_sources where item_id = nullif($1, '')::uuid`, fixture.itemID)
		_, _ = pool.Exec(ctx, `delete from app.items where id = nullif($1, '')::uuid`, fixture.itemID)
		_, _ = pool.Exec(ctx, `delete from app.content_revisions where id = nullif($1, '')::uuid`, fixture.revisionID)
		_, _ = pool.Exec(ctx, `delete from app.raw_documents where id = nullif($1, '')::uuid`, fixture.rawID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, fixture.sourceID)
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Handoff fixture', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, fixture.sourceID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.source_endpoints (
		registry_id, source_id, connector, url, poll_interval, priority,
		robots_policy, expected_content_types, max_response_bytes, fixture_suite
	) values ($1, $2, 'page', 'https://example.com/release', interval '5 minutes',
		'critical', 'page', array['text/html'], 1048576, 'page-v1')`, fixture.registryID, fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	textDigest := sha256.Sum256([]byte("Go release evidence"))
	if err := pool.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, source_registry_id, source_connector,
		source_content_type, ingestion_error_code, ingestion_failed_at
	) values ($1, 'https://example.com/release', $2, $3, $4, $4,
		'link-and-excerpt', $5, 'page', 'text/html', 'pending_parse', $4)
	returning id::text`, fixture.sourceID, "raw/"+fixture.sourceID+"/release.html",
		textDigest[:], now, fixture.registryID).Scan(&fixture.rawID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.content_revisions (
		raw_document_id, normalized_sha256, normalized_text_object_key,
		parser_name, parser_version, title, language, normalized_bytes, outline,
		offset_map, warnings, change_kind, change_reason, material_change, observed_at
	) values ($1::uuid, $2, $3, 'readeck-readability', 'v1', 'Go release', 'en',
		19, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial', 'first revision', false, $4)
	returning id::text`, fixture.rawID, textDigest[:], "normalized/"+fixture.sourceID+"/release.txt", now).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.items (
		current_revision_id, canonical_url, title, normalized_title, normalized_author,
		slug, lifecycle_state, first_seen_at, status, simhash
	) values ($1::uuid, 'https://example.com/release', 'Go release', 'go release', '',
		$2, 'clustered', $3, 'active', $4) returning id::text`,
		fixture.revisionID, "handoff-"+uuid.NewString(), now, make([]byte, 8)).Scan(&fixture.itemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.story_clusters (
		primary_item_id, cluster_key, title, first_seen_at, last_changed_at
	) values ($1::uuid, $2, 'Go release', $3, $3) returning id::text`,
		fixture.itemID, "story:"+fixture.itemID, now).Scan(&fixture.clusterID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.cluster_members (
		cluster_id, item_id, similarity, method
	) values ($1::uuid, $2::uuid, 1, 'new')`, fixture.clusterID, fixture.itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.item_sources (
		revision_id, item_id, canonical_url, source_role, source_tier, sort_order
	) values ($1::uuid, $2::uuid, 'https://example.com/release', 'primary', 'T0', 0)`,
		fixture.revisionID, fixture.itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into app.dedupe_decisions (
		revision_id, item_id, cluster_id, outcome, method, similarity,
		details, evaluated_at
	) values ($1::uuid, $2::uuid, $3::uuid, 'created', 'new', 1, '{}'::jsonb, $4)`,
		fixture.revisionID, fixture.itemID, fixture.clusterID, now); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture handoffFixture) document() ingestion.RawDocument {
	return ingestion.RawDocument{ID: fixture.rawID, SourceID: fixture.sourceID,
		Connector: sources.ConnectorPage, ContentType: "text/html"}
}

func (fixture handoffFixture) decision() dedupe.ProcessResult {
	return dedupe.ProcessResult{ItemID: fixture.itemID, ClusterID: fixture.clusterID,
		Outcome: dedupe.OutcomeCreate}
}

func (fixture handoffFixture) countJobs(t *testing.T, kind string) int {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(context.Background(), `select count(*) from river.river_job
		where queue = $1 and kind = $2 and
			(args ->> 'itemId' = $3 or args ->> 'entityId' = $3)`,
		fixture.queue, kind, fixture.itemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestParsedRevisionHandoffIsAtomicAndIdempotent(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for handoff integration")
	}
	fixture := newHandoffFixture(t)
	ctx := context.Background()
	capabilities := ingestion.HandoffCapabilities{
		EmbeddingModelID: "text-embedding-3-small", ExtractEnabled: true,
	}
	wrongDecision := fixture.decision()
	wrongDecision.ClusterID = uuid.NewString()
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		wrongDecision, fixture.revisionID, capabilities); err == nil {
		t.Fatal("handoff accepted an unrelated dedupe decision")
	}
	var marker *string
	if err := fixture.pool.QueryRow(ctx, `select ingestion_error_code from app.raw_documents
		where id = $1::uuid`, fixture.rawID).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker == nil || *marker != "pending_parse" || fixture.countJobs(t, jobqueue.ExtractItemKind) != 0 {
		t.Fatalf("invalid handoff acknowledged raw or queued work: marker=%v", marker)
	}
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		fixture.decision(), fixture.revisionID, capabilities); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `select ingestion_error_code from app.raw_documents
		where id = $1::uuid`, fixture.rawID).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	var lifecycle string
	if err := fixture.pool.QueryRow(ctx, `select lifecycle_state from app.items
		where id = $1::uuid`, fixture.itemID).Scan(&lifecycle); err != nil {
		t.Fatal(err)
	}
	if marker != nil || lifecycle != "awaiting_ai" ||
		fixture.countJobs(t, jobqueue.ExtractItemKind) != 1 ||
		fixture.countJobs(t, jobqueue.ReembedEntityKind) != 1 {
		t.Fatalf("handoff result marker=%v lifecycle=%s extract=%d reembed=%d",
			marker, lifecycle, fixture.countJobs(t, jobqueue.ExtractItemKind),
			fixture.countJobs(t, jobqueue.ReembedEntityKind))
	}
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		fixture.decision(), fixture.revisionID, capabilities); err != nil {
		t.Fatal(err)
	}
	if fixture.countJobs(t, jobqueue.ExtractItemKind) != 1 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 1 {
		t.Fatal("replayed revision queued duplicate downstream work")
	}
}

func TestParsedRevisionHandoffRespectsCapabilitiesAndProvenance(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for handoff integration")
	}
	fixture := newHandoffFixture(t)
	ctx := context.Background()
	wrongDocument := fixture.document()
	wrongDocument.SourceID = "different-source"
	if err := fixture.store.CompleteParsedRevision(ctx, wrongDocument, fixture.registryID,
		fixture.decision(), fixture.revisionID, ingestion.HandoffCapabilities{}); err == nil {
		t.Fatal("handoff accepted a different source")
	}
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		fixture.decision(), fixture.revisionID, ingestion.HandoffCapabilities{}); err != nil {
		t.Fatal(err)
	}
	var lifecycle string
	if err := fixture.pool.QueryRow(ctx, `select lifecycle_state from app.items where id = $1::uuid`,
		fixture.itemID).Scan(&lifecycle); err != nil {
		t.Fatal(err)
	}
	if lifecycle != "clustered" || fixture.countJobs(t, jobqueue.ExtractItemKind) != 0 ||
		fixture.countJobs(t, jobqueue.ReembedEntityKind) != 0 {
		t.Fatal("disabled capabilities queued downstream work")
	}
	if _, err := fixture.pool.Exec(ctx, `update app.item_sources set source_role = 'supporting'
		where revision_id = $1::uuid`, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.raw_documents
		set ingestion_error_code = 'pending_parse', ingestion_failed_at = now()
		where id = $1::uuid`, fixture.rawID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		fixture.decision(), fixture.revisionID, ingestion.HandoffCapabilities{ExtractEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if fixture.countJobs(t, jobqueue.ExtractItemKind) != 0 {
		t.Fatal("supporting revision entered extraction")
	}
}

func TestParsedRevisionHandoffRejectsLowerTierExtraction(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for handoff integration")
	}
	fixture := newHandoffFixture(t)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `update app.item_sources set source_tier = 'T2'
		where revision_id = $1::uuid`, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.CompleteParsedRevision(ctx, fixture.document(), fixture.registryID,
		fixture.decision(), fixture.revisionID,
		ingestion.HandoffCapabilities{ExtractEnabled: true}); err != nil {
		t.Fatal(err)
	}
	var marker *string
	var lifecycle string
	if err := fixture.pool.QueryRow(ctx, `select raw.ingestion_error_code, item.lifecycle_state
		from app.raw_documents raw
		join app.content_revisions revision on revision.raw_document_id = raw.id
		join app.items item on item.current_revision_id = revision.id
		where raw.id = $1::uuid`, fixture.rawID).Scan(&marker, &lifecycle); err != nil {
		t.Fatal(err)
	}
	if marker != nil || lifecycle != "clustered" || fixture.countJobs(t, jobqueue.ExtractItemKind) != 0 {
		t.Fatalf("lower-tier handoff marker=%v lifecycle=%q extraction jobs=%d",
			marker, lifecycle, fixture.countJobs(t, jobqueue.ExtractItemKind))
	}
}
