package pgstore_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/dedupe/pgstore"
	"github.com/traweezy/relantern/internal/embedding"
)

func TestStorePersistsIdempotentDedupeAndClusters(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2035, time.August, 29, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool, clock.NewFixed(now.Add(time.Hour)), dedupe.DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sources := insertIntegrationSources(t, pool, now)

	fixtureID := time.Now().UnixNano()
	first := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://go.dev/blog/go1.27?utm_source=%d", fixtureID),
		RawText:        "official raw release body",
		NormalizedText: "Go 1.27 improves runtime scheduling, diagnostics, and production reliability for developers.",
		Title:          "Go 1.27 is released",
		Author:         "Go team",
		ObservedAt:     now,
	})
	duplicate := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[1],
		CanonicalURL:   fmt.Sprintf("https://go.dev/doc/devel/release?copy=%d", fixtureID),
		RawText:        "different page chrome around the same release",
		NormalizedText: first.NormalizedText,
		Title:          first.Title,
		Author:         first.Author,
		ObservedAt:     now.Add(time.Minute),
	})
	revision := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://go.dev/blog/go1.27?utm_campaign=%d", fixtureID),
		RawText:        "corrected official release body",
		NormalizedText: "Go 1.27 improves runtime scheduling, diagnostics, production reliability, and corrects the upgrade note.",
		Title:          "Go 1.27 is released",
		Author:         "Go team",
		ObservedAt:     now.Add(2 * time.Minute),
	})
	secondRevision := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://go.dev/blog/go1.27?utm_medium=%d", fixtureID),
		RawText:        "second corrected official release body",
		NormalizedText: "Go 1.27 improves runtime scheduling, diagnostics, production reliability, and includes the final corrected upgrade note.",
		Title:          "Go 1.27 is released",
		Author:         "Go team",
		ObservedAt:     now.Add(150 * time.Second),
	})
	clustered := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[1],
		CanonicalURL:   fmt.Sprintf("https://github.com/golang/go/releases/tag/go1.27-%d", fixtureID),
		RawText:        "independent release record",
		NormalizedText: "The repository tag documents compatibility changes, artifacts, and checksums for this toolchain release.",
		Title:          "Go toolchain artifacts are available",
		Author:         "golang release bot",
		ObservedAt:     now.Add(3 * time.Minute),
	})
	unrelated := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[2],
		CanonicalURL:   fmt.Sprintf("https://react.dev/blog/compiler-%d", fixtureID),
		RawText:        "react compiler raw body",
		NormalizedText: "React compiler rollout guidance covers browser rendering and application migration behavior.",
		Title:          "React Compiler rollout",
		Author:         "React team",
		ObservedAt:     now.Add(4 * time.Minute),
	})
	revisions := []insertedRevision{first, duplicate, revision, secondRevision, clustered, unrelated}
	cleanupIntegration(t, pool, revisions)

	created := processRevision(t, store, first, dedupe.ProcessRequest{
		RevisionID:           first.ID,
		NormalizedText:       first.NormalizedText,
		DeclaredCanonicalURL: "https://attacker.example/copied-release",
		PackageName:          "Go",
		Version:              "1.27",
	})
	if created.Outcome != dedupe.OutcomeCreate || created.Method != dedupe.MethodNew || created.Idempotent {
		t.Fatalf("created result = %+v", created)
	}
	exact := processRevision(t, store, duplicate, dedupe.ProcessRequest{
		RevisionID:     duplicate.ID,
		NormalizedText: duplicate.NormalizedText,
		PackageName:    "Go",
		Version:        "1.27",
	})
	if exact.Outcome != dedupe.OutcomeDuplicate || exact.Method != dedupe.MethodNormalizedSHA256 || exact.ItemID != created.ItemID || exact.ClusterID != created.ClusterID {
		t.Fatalf("exact duplicate result = %+v, created = %+v", exact, created)
	}
	updated := processRevision(t, store, revision, dedupe.ProcessRequest{
		RevisionID:     revision.ID,
		NormalizedText: revision.NormalizedText,
		PackageName:    "Go",
		Version:        "1.27",
	})
	if updated.Outcome != dedupe.OutcomeRevision || updated.Method != dedupe.MethodRevision || updated.ItemID != created.ItemID {
		t.Fatalf("revision result = %+v, created = %+v", updated, created)
	}
	updatedAgain := processRevision(t, store, secondRevision, dedupe.ProcessRequest{
		RevisionID:     secondRevision.ID,
		NormalizedText: secondRevision.NormalizedText,
		PackageName:    "Go",
		Version:        "1.27",
	})
	if updatedAgain.Outcome != dedupe.OutcomeRevision || updatedAgain.Method != dedupe.MethodRevision || updatedAgain.ItemID != created.ItemID {
		t.Fatalf("second revision result = %+v, created = %+v", updatedAgain, created)
	}
	clusterMember := processRevision(t, store, clustered, dedupe.ProcessRequest{
		RevisionID:     clustered.ID,
		NormalizedText: clustered.NormalizedText,
		PackageName:    "Go",
		Version:        "1.27",
	})
	if clusterMember.Outcome != dedupe.OutcomeCluster || clusterMember.Method != dedupe.MethodPackageVersion || clusterMember.ItemID == created.ItemID || clusterMember.ClusterID != created.ClusterID {
		t.Fatalf("cluster member result = %+v, created = %+v", clusterMember, created)
	}
	separate := processRevision(t, store, unrelated, dedupe.ProcessRequest{
		RevisionID:     unrelated.ID,
		NormalizedText: unrelated.NormalizedText,
		PackageName:    "React",
		Version:        "19.2",
	})
	if separate.Outcome != dedupe.OutcomeCreate || separate.ClusterID == created.ClusterID {
		t.Fatalf("unrelated result = %+v, created = %+v", separate, created)
	}
	var separateLifecycle string
	if err := pool.QueryRow(context.Background(), `
		select lifecycle_state from app.items where id = $1::uuid`, separate.ItemID).Scan(&separateLifecycle); err != nil {
		t.Fatalf("select provisional item lifecycle: %v", err)
	}
	if separateLifecycle != "needs_review" {
		t.Fatalf("T2-only item lifecycle = %q", separateLifecycle)
	}
	replayed := processRevision(t, store, first, dedupe.ProcessRequest{
		RevisionID:     first.ID,
		NormalizedText: first.NormalizedText,
	})
	if !replayed.Idempotent || replayed.ItemID != created.ItemID || replayed.ClusterID != created.ClusterID {
		t.Fatalf("replayed result = %+v, created = %+v", replayed, created)
	}

	assertDedupeGraph(t, pool, revisions, created, secondRevision)
}

func TestStoreSerializesConcurrentRevisionProcessing(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2036, time.January, 1, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool, clock.NewFixed(now), dedupe.DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sources := insertIntegrationSources(t, pool, now)
	fixture := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://go.dev/blog/concurrent-%d", time.Now().UnixNano()),
		RawText:        "concurrent raw body",
		NormalizedText: "A deterministic concurrent clustering fixture with enough stable normalized content.",
		Title:          "Concurrent fixture 1.0",
		Author:         "Go team",
		ObservedAt:     now,
	})
	cleanupIntegration(t, pool, []insertedRevision{fixture})

	results := make(chan dedupe.ProcessResult, 2)
	errors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, processError := store.Process(context.Background(), dedupe.ProcessRequest{
				RevisionID:     fixture.ID,
				NormalizedText: fixture.NormalizedText,
			})
			if processError != nil {
				errors <- processError
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errors)
	for processError := range errors {
		t.Errorf("Process() concurrent error = %v", processError)
	}
	var seen []dedupe.ProcessResult
	for result := range results {
		seen = append(seen, result)
	}
	if len(seen) != 2 || seen[0].ItemID != seen[1].ItemID || seen[0].ClusterID != seen[1].ClusterID || seen[0].Idempotent == seen[1].Idempotent {
		t.Fatalf("concurrent results = %+v", seen)
	}
}

func TestStoreClustersByEvaluatedEmbeddingSimilarity(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2036, time.February, 1, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool, clock.NewFixed(now), dedupe.DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sources := insertIntegrationSources(t, pool, now)
	fixtureID := time.Now().UnixNano()
	first := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://postgresql.org/about/news/failover-%d", fixtureID),
		RawText:        "primary availability report",
		NormalizedText: "PostgreSQL operators validate synchronous replicas, WAL retention, recovery objectives, and automated promotion drills.",
		Title:          "PostgreSQL availability operations",
		Author:         "PostgreSQL team",
		ObservedAt:     now,
	})
	second := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[1],
		CanonicalURL:   fmt.Sprintf("https://example.test/database-resilience-%d", fixtureID),
		RawText:        "independent resilience report",
		NormalizedText: "A database resilience guide covers standby takeover, replica delay, regional outage exercises, and service recovery targets.",
		Title:          "Database resilience field guide",
		Author:         "Infrastructure editors",
		ObservedAt:     now.Add(time.Minute),
	})
	if distance := dedupe.SimHashDistance(dedupe.SimHash(first.NormalizedText), dedupe.SimHash(second.NormalizedText)); distance <= dedupe.EvaluatedSimHashDistance {
		t.Fatalf("semantic fixture SimHash distance = %d, must exceed %d", distance, dedupe.EvaluatedSimHashDistance)
	}
	cleanupIntegration(t, pool, []insertedRevision{first, second})

	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[0] = 1
	created := processRevision(t, store, first, dedupe.ProcessRequest{
		RevisionID:       first.ID,
		NormalizedText:   first.NormalizedText,
		EmbeddingModelID: embedding.DefaultModelID,
		Embedding:        vector,
	})
	clustered := processRevision(t, store, second, dedupe.ProcessRequest{
		RevisionID:       second.ID,
		NormalizedText:   second.NormalizedText,
		EmbeddingModelID: embedding.DefaultModelID,
		Embedding:        vector,
	})
	if created.Outcome != dedupe.OutcomeCreate || clustered.Outcome != dedupe.OutcomeCluster ||
		clustered.Method != dedupe.MethodEmbedding || clustered.ItemID == created.ItemID ||
		clustered.ClusterID != created.ClusterID || clustered.Similarity != 1 {
		t.Fatalf("created = %+v, embedding-clustered = %+v", created, clustered)
	}

	var embeddings int
	var threshold float64
	if err := pool.QueryRow(context.Background(), `
		select
			(select count(*) from app.embeddings
			 where entity_type = 'item' and entity_id = any($1::uuid[])),
			(details ->> 'embeddingThreshold')::double precision
		from app.dedupe_decisions
		where revision_id = $2::uuid`,
		[]string{created.ItemID, clustered.ItemID},
		second.ID,
	).Scan(&embeddings, &threshold); err != nil {
		t.Fatalf("inspect semantic dedupe records: %v", err)
	}
	if embeddings != 2 || threshold != dedupe.EvaluatedEmbeddingSimilarity {
		t.Fatalf("embedding records = %d, threshold = %f", embeddings, threshold)
	}
}

func TestStoreRejectsInvalidBoundariesWithoutDecision(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2037, time.January, 1, 12, 0, 0, 0, time.UTC)
	store, err := pgstore.New(pool, clock.NewFixed(now), dedupe.DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sources := insertIntegrationSources(t, pool, now)
	fixture := insertRevision(t, pool, revisionFixture{
		SourceID:       sources[0],
		CanonicalURL:   fmt.Sprintf("https://go.dev/blog/validation-%d", time.Now().UnixNano()),
		RawText:        "validation raw body",
		NormalizedText: "A deterministic validation fixture with stable normalized content.",
		Title:          "Validation fixture 1.0",
		Author:         "Go team",
		ObservedAt:     now,
	})
	cleanupIntegration(t, pool, []insertedRevision{fixture})

	for _, request := range []dedupe.ProcessRequest{
		{RevisionID: "invalid", NormalizedText: fixture.NormalizedText},
		{RevisionID: fixture.ID},
		{RevisionID: fixture.ID, NormalizedText: "mismatched normalized text"},
		{RevisionID: fixture.ID, NormalizedText: fixture.NormalizedText, EmbeddingModelID: embedding.DefaultModelID},
		{RevisionID: fixture.ID, NormalizedText: fixture.NormalizedText, Embedding: embedding.Vector{1}},
		{RevisionID: fixture.ID, NormalizedText: fixture.NormalizedText, EmbeddingModelID: embedding.DefaultModelID, Embedding: make(embedding.Vector, embedding.DefaultDimensions)},
	} {
		if _, err := store.Process(context.Background(), request); err == nil {
			t.Errorf("Process() accepted %+v", request)
		}
	}
	var decisions int
	if err := pool.QueryRow(context.Background(), `
		select count(*) from app.dedupe_decisions where revision_id = $1::uuid`, fixture.ID).Scan(&decisions); err != nil {
		t.Fatalf("count rejected dedupe decisions: %v", err)
	}
	if decisions != 0 {
		t.Fatalf("rejected boundary persisted %d decisions", decisions)
	}
	if _, err := pgstore.New(nil, clock.NewFixed(now), dedupe.DefaultConfig()); err == nil {
		t.Fatal("New() accepted a nil database")
	}
	if _, err := pgstore.New(pool, nil, dedupe.DefaultConfig()); err == nil {
		t.Fatal("New() accepted a nil clock")
	}
	if _, err := pgstore.New(pool, clock.NewFixed(now), dedupe.Config{}); err == nil {
		t.Fatal("New() accepted invalid configuration")
	}
}

type revisionFixture struct {
	SourceID       string
	CanonicalURL   string
	RawText        string
	NormalizedText string
	Title          string
	Author         string
	ObservedAt     time.Time
}

type insertedRevision struct {
	ID             string
	RawDocumentID  string
	NormalizedText string
	Title          string
	Author         string
}

func insertRevision(t *testing.T, pool *pgxpool.Pool, fixture revisionFixture) insertedRevision {
	t.Helper()
	rawDigest := sha256.Sum256([]byte(fixture.RawText))
	normalizedDigest := sha256.Sum256([]byte(fixture.NormalizedText))
	var rawDocumentID string
	err := pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, source_published_at, content_policy
		) values (
			$1, $2, $3, $4, $5, $5, $5, 'link-and-excerpt'
		) returning id::text`,
		fixture.SourceID,
		fixture.CanonicalURL,
		fmt.Sprintf("raw/%s/%x-%d.bin", fixture.SourceID, rawDigest, fixture.ObservedAt.UnixNano()),
		rawDigest[:],
		fixture.ObservedAt,
	).Scan(&rawDocumentID)
	if err != nil {
		t.Fatalf("insert raw document: %v", err)
	}
	var revisionID string
	err = pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, author, language,
			source_published_at, normalized_bytes, outline, offset_map,
			warnings, change_kind, change_reason, material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'dedupe-integration', '1.0.0', $4, $5, 'en',
			$6, $7, '[]'::jsonb, '[]'::jsonb,
			'[]'::jsonb, 'initial', 'dedupe integration fixture', false, $6
		) returning id::text`,
		rawDocumentID,
		normalizedDigest[:],
		fmt.Sprintf("normalized/%s/%x.txt", fixture.SourceID, normalizedDigest),
		fixture.Title,
		fixture.Author,
		fixture.ObservedAt,
		len(fixture.NormalizedText),
	).Scan(&revisionID)
	if err != nil {
		t.Fatalf("insert content revision: %v", err)
	}
	return insertedRevision{
		ID:             revisionID,
		RawDocumentID:  rawDocumentID,
		NormalizedText: fixture.NormalizedText,
		Title:          fixture.Title,
		Author:         fixture.Author,
	}
}

func insertIntegrationSources(t *testing.T, pool *pgxpool.Pool, observedAt time.Time) [3]string {
	t.Helper()
	fixtureID := time.Now().UnixNano()
	sources := [3]string{
		fmt.Sprintf("dedupe-%d-t0", fixtureID),
		fmt.Sprintf("dedupe-%d-t1", fixtureID),
		fmt.Sprintf("dedupe-%d-t2", fixtureID),
	}
	for index, sourceID := range sources {
		tier := fmt.Sprintf("T%d", index)
		_, err := pool.Exec(context.Background(), `
			insert into app.sources (
				id, name, trust_tier, owner, origin, validation_state,
				homepage_url, content_policy, enabled, topics, reviewed_at
			) values (
				$1, $2, $3, 'dedupe-integration', 'owner', 'active',
				$4, 'link-and-excerpt', false, array['dedupe']::text[], $5
			)`,
			sourceID,
			"Dedupe integration "+tier,
			tier,
			"https://"+sourceID+".example.test/",
			observedAt,
		)
		if err != nil {
			t.Fatalf("insert integration source %s: %v", sourceID, err)
		}
	}
	t.Cleanup(func() {
		for index := len(sources) - 1; index >= 0; index-- {
			if _, err := pool.Exec(context.Background(), `delete from app.sources where id = $1`, sources[index]); err != nil {
				t.Errorf("delete integration source %s: %v", sources[index], err)
			}
		}
	})
	return sources
}

func processRevision(
	t *testing.T,
	store *pgstore.Store,
	fixture insertedRevision,
	request dedupe.ProcessRequest,
) dedupe.ProcessResult {
	t.Helper()
	result, err := store.Process(context.Background(), request)
	if err != nil {
		t.Fatalf("Process(%s) error = %v", fixture.ID, err)
	}
	return result
}

func assertDedupeGraph(
	t *testing.T,
	pool *pgxpool.Pool,
	revisions []insertedRevision,
	created dedupe.ProcessResult,
	updated insertedRevision,
) {
	t.Helper()
	ids := revisionIDs(revisions)
	var decisions int
	var items int
	var clusters int
	var members int
	var sources int
	if err := pool.QueryRow(context.Background(), `
		select
			(select count(*) from app.dedupe_decisions where revision_id = any($1::uuid[])),
			(select count(distinct item_id) from app.dedupe_decisions where revision_id = any($1::uuid[])),
			(select count(distinct cluster_id) from app.dedupe_decisions where revision_id = any($1::uuid[])),
			(select count(*) from app.cluster_members where item_id in (
				select distinct item_id from app.dedupe_decisions where revision_id = any($1::uuid[])
			)),
			(select count(*) from app.item_sources where revision_id = any($1::uuid[]))`, ids).Scan(
		&decisions,
		&items,
		&clusters,
		&members,
		&sources,
	); err != nil {
		t.Fatalf("inspect dedupe graph: %v", err)
	}
	if decisions != 6 || items != 3 || clusters != 2 || members != 3 || sources != 6 {
		t.Fatalf("decisions=%d items=%d clusters=%d members=%d sources=%d", decisions, items, clusters, members, sources)
	}
	var currentRevision string
	var canonicalURL string
	if err := pool.QueryRow(context.Background(), `
		select current_revision_id::text, canonical_url
		from app.items where id = $1::uuid`, created.ItemID).Scan(&currentRevision, &canonicalURL); err != nil {
		t.Fatalf("select promoted item: %v", err)
	}
	if currentRevision != updated.ID || canonicalURL != "https://go.dev/blog/go1.27" {
		t.Fatalf("promoted item revision=%q canonical=%q", currentRevision, canonicalURL)
	}
	var primaryTier string
	if err := pool.QueryRow(context.Background(), `
		select source_tier from app.item_sources
		where item_id = $1::uuid and source_role = 'primary'`, created.ItemID).Scan(&primaryTier); err != nil {
		t.Fatalf("select primary source tier: %v", err)
	}
	if primaryTier != "T0" {
		t.Fatalf("primary source tier = %q", primaryTier)
	}
}

func cleanupIntegration(t *testing.T, pool *pgxpool.Pool, revisions []insertedRevision) {
	t.Helper()
	t.Cleanup(func() {
		ids := revisionIDs(revisions)
		ctx := context.Background()
		execute := func(label string, query string, arguments ...any) {
			t.Helper()
			if _, err := pool.Exec(ctx, query, arguments...); err != nil {
				t.Errorf("clean up dedupe integration %s: %v", label, err)
			}
		}
		execute("decisions", `delete from app.dedupe_decisions where revision_id = any($1::uuid[])`, ids)
		execute("embeddings", `
			delete from app.embeddings where revision_id = any($1::uuid[])
				or (entity_type = 'item' and entity_id in (
					select item_id from app.item_sources where revision_id = any($1::uuid[])
				))`, ids)
		execute("search documents", `
			delete from app.search_documents where revision_id = any($1::uuid[])
				or item_id in (
					select item_id from app.item_sources where revision_id = any($1::uuid[])
				)`, ids)
		execute("cluster members", `
			delete from app.cluster_members where item_id in (
				select item_id from app.item_sources where revision_id = any($1::uuid[])
			)`, ids)
		execute("story clusters", `
			delete from app.story_clusters cluster
			where not exists (
				select 1 from app.cluster_members member where member.cluster_id = cluster.id
			) and cluster.primary_item_id in (
				select item_id from app.item_sources where revision_id = any($1::uuid[])
			)`, ids)
		execute("item sources", `delete from app.item_sources where revision_id = any($1::uuid[])`, ids)
		execute("items", `
			delete from app.items item where item.current_revision_id = any($1::uuid[])`, ids)
		execute("content revisions", `delete from app.content_revisions where id = any($1::uuid[])`, ids)
		for _, revision := range revisions {
			execute("raw document", `delete from app.raw_documents where id = $1::uuid`, revision.RawDocumentID)
		}
	})
}

func revisionIDs(revisions []insertedRevision) []string {
	ids := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		ids = append(ids, revision.ID)
	}
	return ids
}

func openIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for dedupe integration tests")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
