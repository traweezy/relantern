package pgstore

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/research"
	researchstore "github.com/traweezy/relantern/internal/research/pgstore"
	"github.com/traweezy/relantern/internal/search"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
)

func TestReconcilePendingRequiresCapacityForEachEnabledStage(t *testing.T) {
	count, err := (&Store{}).ReconcilePending(context.Background(), ingestion.HandoffCapabilities{
		ExtractEnabled: true, ResearchEnabled: true,
	}, 1)
	if err == nil || count != 0 {
		t.Fatalf("under-capacity reconciliation = %d jobs, %v", count, err)
	}
}

func TestReconcilePendingRequiresClockForResearch(t *testing.T) {
	count, err := (&Store{}).ReconcilePending(context.Background(), ingestion.HandoffCapabilities{
		ResearchEnabled: true,
	}, 1)
	if err == nil || count != 0 {
		t.Fatalf("research reconciliation without clock = %d jobs, %v", count, err)
	}
}

func TestReconcilePendingRecoversExtractionAfterFastCapabilityEnables(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	ctx := context.Background()

	count, err := fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{}, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ExtractItemKind) != 0 {
		t.Fatalf("disabled reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileLifecycle(t, fixture, "clustered")

	count, err = fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{ExtractEnabled: true}, 1)
	if err != nil || count != 1 || fixture.countJobs(t, jobqueue.ExtractItemKind) != 1 {
		t.Fatalf("enabled extraction reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileLifecycle(t, fixture, "awaiting_ai")
	assertReconcileJobRevision(t, fixture, jobqueue.ExtractItemKind, "itemId", fixture.itemID, fixture.revisionID, 1)

	count, err = fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{ExtractEnabled: true}, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ExtractItemKind) != 1 {
		t.Fatalf("repeated extraction reconciliation = %d jobs, %v", count, err)
	}
}

func TestReconcilePendingRecoversEmbeddingAfterCapabilityEnables(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	ctx := context.Background()

	count, err := fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{}, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 0 {
		t.Fatalf("disabled embedding reconciliation = %d jobs, %v", count, err)
	}

	capabilities := ingestion.HandoffCapabilities{EmbeddingModelID: "text-embedding-3-small"}
	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 1 {
		t.Fatalf("enabled embedding reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileJobRevision(t, fixture, jobqueue.ReembedEntityKind, "entityId", fixture.itemID, fixture.revisionID, 1)
	var modelJobs int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2 and args ->> 'entityId' = $3
			and args ->> 'revisionId' = $4 and args ->> 'modelId' = $5`,
		fixture.queue, jobqueue.ReembedEntityKind, fixture.itemID, fixture.revisionID,
		capabilities.EmbeddingModelID).Scan(&modelJobs); err != nil || modelJobs != 1 {
		t.Fatalf("embedding model handoff = %d jobs, %v", modelJobs, err)
	}
	assertReconcileLifecycle(t, fixture, "clustered")

	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 1 {
		t.Fatalf("repeated embedding reconciliation = %d jobs, %v", count, err)
	}
}

func TestReconcilePendingRepairsLegacyUnpinnedProjectionBeforeReembedding(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(ctx, `delete from app.search_documents where item_id = $1::uuid`, fixture.itemID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.embeddings where entity_id = $1::uuid`, fixture.itemID)
	})
	modelID := embedding.DefaultModelID
	embeddings, err := embeddingstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[0] = 1
	embeddingID, inserted, err := embeddings.Put(ctx, embeddingstore.PutRequest{
		EntityType: "item", EntityID: fixture.itemID, RevisionID: fixture.revisionID,
		ModelID: modelID, Input: "Go release evidence", Vector: vector,
	})
	if err != nil || !inserted {
		t.Fatalf("persist legacy embedding = %s/%t, %v", embeddingID, inserted, err)
	}
	searchIndex, err := searchstore.New(fixture.pool, embedding.DefaultDimensions, search.DefaultRRFK)
	if err != nil {
		t.Fatal(err)
	}
	if err := searchIndex.IndexDocument(ctx, searchstore.IndexRequest{
		ItemID: fixture.itemID, RevisionID: fixture.revisionID,
		EmbeddingID: embeddingID, EmbeddingModelID: modelID,
		NormalizedContent: "Go release evidence",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.search_documents set embedding_id = null
		where item_id = $1::uuid`, fixture.itemID); err != nil {
		t.Fatal(err)
	}
	legacyClient, err := river.NewClient(riverpgxv5.New(nil), &river.Config{Schema: jobqueue.Schema})
	if err != nil {
		t.Fatal(err)
	}
	legacyArgs := jobqueue.ReembedEntityArgs{
		EntityType: "item", EntityID: fixture.itemID,
		RevisionID: fixture.revisionID, ModelID: modelID,
	}
	legacyOpts := legacyArgs.InsertOpts()
	legacyOpts.Queue = fixture.queue
	tx, err := fixture.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	legacyJob, err := legacyClient.InsertTx(ctx, tx, legacyArgs, &legacyOpts)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `update river.river_job set state = 'completed',
		finalized_at = now() where id = $1`, legacyJob.Job.ID); err != nil {
		t.Fatal(err)
	}
	capabilities := ingestion.HandoffCapabilities{EmbeddingModelID: modelID}
	count, err := fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 1 {
		t.Fatalf("exact legacy projection repair = %d new jobs, %v", count, err)
	}
	var pinnedID string
	if err := fixture.pool.QueryRow(ctx, `select embedding_id::text
		from app.search_documents where item_id = $1::uuid`, fixture.itemID).Scan(&pinnedID); err != nil {
		t.Fatal(err)
	}
	if pinnedID != embeddingID {
		t.Fatalf("repaired pin = %s, want %s", pinnedID, embeddingID)
	}
	// When the prior vector is unavailable, the new projection identity must
	// bypass the completed legacy job and queue a real worker attempt.
	if _, err := fixture.pool.Exec(ctx, `delete from app.embeddings where id = $1::uuid`, embeddingID); err != nil {
		t.Fatal(err)
	}
	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 || fixture.countJobs(t, jobqueue.ReembedEntityKind) != 2 {
		t.Fatalf("missing vector reembedding = %d new jobs, %v", count, err)
	}
	var newVersion int
	if err := fixture.pool.QueryRow(ctx, `select (args ->> 'projectionVersion')::int
		from river.river_job where kind = $1 and queue = $2
			and args ->> 'entityId' = $3 and id <> $4`, jobqueue.ReembedEntityKind,
		fixture.queue, fixture.itemID, legacyJob.Job.ID).Scan(&newVersion); err != nil {
		t.Fatal(err)
	}
	if newVersion != jobqueue.ReembedProjectionVersion {
		t.Fatalf("queued projection version = %d", newVersion)
	}
}

func TestReconcilePendingQueuesSupersedingCurrentRevision(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	ctx := context.Background()
	capabilities := ingestion.HandoffCapabilities{ExtractEnabled: true}

	count, err := fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 {
		t.Fatalf("initial extraction reconciliation = %d jobs, %v", count, err)
	}
	newRevisionID := insertSupersedingReconcileRevision(t, fixture)
	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 {
		t.Fatalf("superseding extraction reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileJobRevision(t, fixture, jobqueue.ExtractItemKind, "itemId", fixture.itemID, fixture.revisionID, 1)
	assertReconcileJobRevision(t, fixture, jobqueue.ExtractItemKind, "itemId", fixture.itemID, newRevisionID, 1)
	assertReconcileLifecycle(t, fixture, "awaiting_ai")

	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 0 || fixture.countJobs(t, jobqueue.ExtractItemKind) != 2 {
		t.Fatalf("repeated superseding reconciliation = %d jobs, %v", count, err)
	}
}

func TestReconcilePendingBackfillsResearchForReadyCurrentRevision(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	insertVerifiedReconcileClaim(t, fixture)
	activateReconcileResearchSchedule(t, fixture)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := fixture.pool.Exec(ctx, `delete from river.river_job
			where queue = $1 and kind = $2 and args ->> 'clusterId' = $3`,
			fixture.queue, jobqueue.ResearchStoryKind, fixture.clusterID); err != nil {
			t.Errorf("remove research reconciliation job: %v", err)
		}
	})

	count, err := fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{}, 1)
	if err != nil || count != 0 {
		t.Fatalf("disabled research reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileJobRevision(t, fixture, jobqueue.ResearchStoryKind, "clusterId", fixture.clusterID, fixture.revisionID, 0)

	count, err = fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{ResearchEnabled: true}, 1)
	if err != nil || count != 1 {
		t.Fatalf("enabled research reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileJobRevision(t, fixture, jobqueue.ResearchStoryKind, "clusterId", fixture.clusterID, fixture.revisionID, 1)

	count, err = fixture.store.ReconcilePending(ctx, ingestion.HandoffCapabilities{ResearchEnabled: true}, 1)
	if err != nil || count != 0 {
		t.Fatalf("repeated research reconciliation = %d jobs, %v", count, err)
	}
	assertReconcileJobRevision(t, fixture, jobqueue.ResearchStoryKind, "clusterId", fixture.clusterID, fixture.revisionID, 1)
}

func TestReconcilePendingRefreshesResearchWhenVerifiedFactsChange(t *testing.T) {
	requireReconcileDatabase(t)
	fixture := newHandoffFixture(t)
	prioritizeReconcileFixture(t, fixture)
	insertVerifiedReconcileClaim(t, fixture)
	activateReconcileResearchSchedule(t, fixture)
	ctx := context.Background()
	var laterClaimID, supportingRawID, supportingRevisionID, supportingItemID, supportingExtractionRunID string
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(ctx, `delete from app.outbox_events
			where aggregate_type = 'story' and aggregate_id = $1::uuid`, fixture.clusterID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.research_assertion_sources where research_assertion_id in (
			select assertion.id from app.research_assertions assertion
			join app.research_briefs brief on brief.id = assertion.research_brief_id
			where brief.cluster_id = $1::uuid)`, fixture.clusterID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.research_assertion_claims where research_assertion_id in (
			select assertion.id from app.research_assertions assertion
			join app.research_briefs brief on brief.id = assertion.research_brief_id
			where brief.cluster_id = $1::uuid)`, fixture.clusterID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.research_assertions where research_brief_id in (
			select id from app.research_briefs where cluster_id = $1::uuid)`, fixture.clusterID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.research_sources where ai_run_id in (
			select id from app.ai_runs where cluster_id = $1::uuid)`, fixture.clusterID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.research_briefs where cluster_id = $1::uuid`, fixture.clusterID)
		if laterClaimID != "" {
			_, _ = fixture.pool.Exec(ctx, `delete from app.evidence_spans where claim_id = $1::uuid`, laterClaimID)
			_, _ = fixture.pool.Exec(ctx, `delete from app.claims where id = $1::uuid`, laterClaimID)
		}
		_, _ = fixture.pool.Exec(ctx, `delete from app.ai_runs
			where cluster_id = $1::uuid and purpose = $2`, fixture.clusterID, research.Purpose)
		if supportingExtractionRunID != "" {
			_, _ = fixture.pool.Exec(ctx, `delete from app.ai_runs where id = $1::uuid`, supportingExtractionRunID)
		}
		if supportingItemID != "" {
			_, _ = fixture.pool.Exec(ctx, `delete from app.cluster_members where item_id = $1::uuid`, supportingItemID)
			_, _ = fixture.pool.Exec(ctx, `delete from app.item_sources where item_id = $1::uuid`, supportingItemID)
			_, _ = fixture.pool.Exec(ctx, `delete from app.items where id = $1::uuid`, supportingItemID)
		}
		if supportingRevisionID != "" {
			_, _ = fixture.pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, supportingRevisionID)
		}
		if supportingRawID != "" {
			_, _ = fixture.pool.Exec(ctx, `delete from app.raw_documents where id = $1::uuid`, supportingRawID)
		}
	})
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(ctx, `delete from river.river_job
			where queue = $1 and kind = $2 and args @> jsonb_build_object('clusterId', $3::text)`,
			fixture.queue, jobqueue.ResearchStoryKind, fixture.clusterID)
	})
	capabilities := ingestion.HandoffCapabilities{ResearchEnabled: true}
	count, err := fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 {
		t.Fatalf("first research handoff = %d jobs, %v", count, err)
	}
	var firstJobID int64
	var firstHash string
	if err := fixture.pool.QueryRow(ctx, `select id, args ->> 'inputSha256'
		from river.river_job where queue = $1 and kind = $2
			and args @> jsonb_build_object('clusterId', $3::text)`,
		fixture.queue, jobqueue.ResearchStoryKind, fixture.clusterID).Scan(&firstJobID, &firstHash); err != nil {
		t.Fatalf("inspect first research job: %v", err)
	}
	researchRepository, err := researchstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	now := fixture.store.clock.Now().UTC().Add(time.Minute)
	first, err := researchRepository.Prepare(ctx, research.ProcessRequest{
		ClusterID: fixture.clusterID, RevisionID: fixture.revisionID, InputSHA256: firstHash,
	}, now)
	if err != nil || first.Obsolete {
		t.Fatalf("prepare initial research = %+v, %v", first, err)
	}
	if _, err := researchRepository.Complete(ctx, first, reconcileResearchCompletion(first,
		"Initial Go release brief"), now.Add(time.Second)); err != nil {
		t.Fatalf("publish initial research run: %v", err)
	}
	var firstRunCreatedAt time.Time
	if err := fixture.pool.QueryRow(ctx, `select created_at from app.ai_runs
		where id = $1::uuid`, first.RunID).Scan(&firstRunCreatedAt); err != nil {
		t.Fatalf("inspect initial research snapshot time: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update river.river_job
		set state = 'completed', finalized_at = now() where id = $1`, firstJobID); err != nil {
		t.Fatalf("complete initial research job: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items set lifecycle_state = 'published'
		where id = $1::uuid`, fixture.itemID); err != nil {
		t.Fatalf("mark primary story published before supporting evidence: %v", err)
	}
	supportingURL := "https://example.com/supporting-" + uuid.NewString()
	supportingDigest := sha256.Sum256([]byte("independent supporting release evidence"))
	if err := fixture.pool.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, source_registry_id, source_connector,
		source_content_type
	) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6, 'page', 'text/html')
	returning id::text`, fixture.sourceID, supportingURL,
		"raw/"+fixture.sourceID+"/supporting.html", supportingDigest[:], now,
		fixture.registryID).Scan(&supportingRawID); err != nil {
		t.Fatalf("record supporting source evidence: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into app.content_revisions (
		raw_document_id, normalized_sha256, normalized_text_object_key,
		parser_name, parser_version, title, language, normalized_bytes, outline,
		offset_map, warnings, change_kind, change_reason, material_change, observed_at
	) values ($1::uuid, $2, $3, 'fixture', '1.0.0', 'Supporting Go release', 'en',
		39, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial',
		'independent supporting source', false, $4) returning id::text`,
		supportingRawID, supportingDigest[:],
		"normalized/"+fixture.sourceID+"/supporting.txt", now).Scan(&supportingRevisionID); err != nil {
		t.Fatalf("normalize supporting source evidence: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into app.items (
		current_revision_id, canonical_url, title, normalized_title, normalized_author,
		slug, lifecycle_state, first_seen_at, status, simhash
	) values ($1::uuid, $2, 'Supporting Go release', 'supporting go release', '',
		$3, 'ready', $4, 'active', $5) returning id::text`, supportingRevisionID,
		supportingURL, "supporting-"+uuid.NewString(), now, make([]byte, 8)).Scan(&supportingItemID); err != nil {
		t.Fatalf("create supporting item: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into app.cluster_members (
		cluster_id, item_id, similarity, method
	) values ($1::uuid, $2::uuid, 0.91, 'new')`, fixture.clusterID, supportingItemID); err != nil {
		t.Fatalf("join supporting item to story: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into app.item_sources (
		revision_id, item_id, canonical_url, source_role, source_tier, sort_order
	) values ($1::uuid, $2::uuid, $3, 'primary', 'T0', 0)`,
		supportingRevisionID, supportingItemID, supportingURL); err != nil {
		t.Fatalf("attach supporting primary source: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into app.ai_runs (
		item_id, revision_id, purpose, model_config_id, prompt_version_id,
		background, state, input_sha256, started_at, completed_at
	) select $1::uuid, $2::uuid, 'structured_extraction', model.id, prompt.id,
		false, 'completed', $3, $4, $4
	from app.model_configs model cross join app.prompt_versions prompt
	where model.role = 'fast' and model.enabled
		and prompt.purpose = 'structured_extraction' and prompt.active
	returning id::text`, supportingItemID, supportingRevisionID,
		supportingDigest[:], now).Scan(&supportingExtractionRunID); err != nil {
		t.Fatalf("complete supporting extraction: %v", err)
	}
	// A transaction may start before the first research run yet commit its
	// supporting claim afterward. The facts hash, not created_at, decides work.
	if err := fixture.pool.QueryRow(ctx, `insert into app.claims (
		item_id, revision_id, ai_run_id, claim_index, claim_type, claim_text,
		normalized_value, confidence, material, verification_state, created_at
	) values ($1::uuid, $2::uuid, $3::uuid, 0, 'compatibility',
		'Go release changes package compatibility', '"changed"'::jsonb,
		'high', true, 'verified_span', $4) returning id::text`,
		supportingItemID, supportingRevisionID, supportingExtractionRunID,
		firstRunCreatedAt.Add(-time.Second)).Scan(&laterClaimID); err != nil {
		t.Fatalf("add later verified claim: %v", err)
	}
	claimText := "Go release changes package compatibility"
	quoteHash := sha256.Sum256([]byte(claimText))
	if _, err := fixture.pool.Exec(ctx, `insert into app.evidence_spans (
		claim_id, revision_id, span_identifier, section_path,
		start_offset, end_offset, quote_hash
	) values ($1::uuid, $2::uuid, 'span_0002', 'Compatibility', 0, $3, $4)`,
		laterClaimID, supportingRevisionID, len(claimText), quoteHash[:]); err != nil {
		t.Fatalf("add later claim evidence: %v", err)
	}
	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 1 {
		t.Fatalf("changed-facts research handoff = %d jobs, %v", count, err)
	}
	var nextHash string
	if err := fixture.pool.QueryRow(ctx, `select args ->> 'inputSha256' from river.river_job
		where queue = $1 and kind = $2 and id <> $3
			and args @> jsonb_build_object('clusterId', $4::text, 'revisionId', $5::text)`,
		fixture.queue, jobqueue.ResearchStoryKind, firstJobID,
		fixture.clusterID, fixture.revisionID).Scan(&nextHash); err != nil {
		t.Fatalf("inspect refreshed research job: %v", err)
	}
	if nextHash == firstHash || nextHash == "" {
		t.Fatalf("research facts hash did not change: %q", nextHash)
	}
	var stillPrimaryRevisionID string
	if err := fixture.pool.QueryRow(ctx, `select item.current_revision_id::text
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		where cluster.id = $1::uuid`, fixture.clusterID).Scan(&stillPrimaryRevisionID); err != nil {
		t.Fatalf("inspect unchanged story primary: %v", err)
	}
	if stillPrimaryRevisionID != fixture.revisionID {
		t.Fatalf("supporting item changed the primary revision to %s", stillPrimaryRevisionID)
	}
	stale, err := researchRepository.Prepare(ctx, research.ProcessRequest{
		ClusterID: fixture.clusterID, RevisionID: fixture.revisionID, InputSHA256: firstHash,
	}, now.Add(2*time.Second))
	if err != nil || !stale.Obsolete {
		t.Fatalf("stale queued facts = %+v, %v", stale, err)
	}
	next, err := researchRepository.Prepare(ctx, research.ProcessRequest{
		ClusterID: fixture.clusterID, RevisionID: fixture.revisionID, InputSHA256: nextHash,
	}, now.Add(3*time.Second))
	if err != nil || next.Obsolete || next.RunID == first.RunID ||
		next.RevisionID != first.RevisionID || len(next.Claims) != 2 {
		t.Fatalf("refreshed research run = %+v, %v", next, err)
	}
	if _, err := researchRepository.Complete(ctx, next, reconcileResearchCompletion(next,
		"Updated Go release brief"), now.Add(4*time.Second)); err != nil {
		t.Fatalf("publish refreshed research run: %v", err)
	}
	var updatedEvents int
	var latestHeadline string
	if err := fixture.pool.QueryRow(ctx, `select count(*) from app.outbox_events
		where aggregate_type = 'story' and aggregate_id = $1::uuid
			and event_type = 'story-updated'`, fixture.clusterID).Scan(&updatedEvents); err != nil {
		t.Fatalf("inspect refreshed Live event: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select headline from app.v_story_summaries
		where story_id = $1::uuid`, fixture.clusterID).Scan(&latestHeadline); err != nil {
		t.Fatalf("inspect latest research brief: %v", err)
	}
	if updatedEvents != 1 || latestHeadline != "Updated Go release brief" {
		t.Fatalf("refreshed publication = %d Live updates, %q", updatedEvents, latestHeadline)
	}
	count, err = fixture.store.ReconcilePending(ctx, capabilities, 1)
	if err != nil || count != 0 {
		t.Fatalf("repeated changed-facts handoff = %d jobs, %v", count, err)
	}
}

func reconcileResearchCompletion(prepared research.PreparedRun, headline string) research.Completion {
	claimIDs := make([]string, 0, len(prepared.Claims))
	sources := make([]research.Source, 0, len(prepared.Claims))
	sourceURLs := make([]string, 0, len(prepared.Claims))
	seenSources := make(map[string]struct{}, len(prepared.Claims))
	for _, claim := range prepared.Claims {
		claimIDs = append(claimIDs, claim.ID)
		if _, exists := seenSources[claim.SourceURL]; !exists {
			seenSources[claim.SourceURL] = struct{}{}
			sources = append(sources, research.Source{URL: claim.SourceURL, Domain: "example.com"})
			sourceURLs = append(sourceURLs, claim.SourceURL)
		}
	}
	return research.Completion{
		ProviderID: "resp_" + uuid.NewString(),
		Sources:    sources,
		Output: research.Output{
			Headline:          headline,
			Summary:           "The verified release facts are ready for review.",
			WhyItMatters:      "The evidence changes the supported release baseline.",
			RecommendedAction: "Review the source release before changing dependencies.",
			Confidence:        "high",
			Uncertainties:     []string{},
			Assertions: []research.Assertion{{
				Text: "The release has verified source evidence.", Material: true,
				ClaimIDs: claimIDs, SourceURLs: sourceURLs,
			}},
		},
	}
}

func requireReconcileDatabase(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for reconciliation integration")
	}
}

func prioritizeReconcileFixture(t *testing.T, fixture handoffFixture) {
	t.Helper()
	ctx := context.Background()
	firstSeen := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fixture.pool.Exec(ctx, `update app.items set first_seen_at = $2
		where id = $1::uuid`, fixture.itemID, firstSeen); err != nil {
		t.Fatalf("prioritize reconciliation item: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.story_clusters
		set first_seen_at = $2, last_changed_at = $2
		where id = $1::uuid`, fixture.clusterID, firstSeen); err != nil {
		t.Fatalf("prioritize reconciliation cluster: %v", err)
	}
}

func activateReconcileResearchSchedule(t *testing.T, fixture handoffFixture) {
	t.Helper()
	ctx := context.Background()
	now := fixture.store.clock.Now().UTC()
	login := "research-admission-" + uuid.NewString()
	var userID string
	if err := fixture.pool.QueryRow(ctx, `insert into app.users (
		github_user_id, login, display_name, timezone, email, email_verified
	) values ($1, $2, $2, 'UTC', $3, true) returning id::text`,
		time.Now().UnixNano(), login, login+"@tests.relantern.local").Scan(&userID); err != nil {
		t.Fatalf("create research admission owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(ctx, `delete from app.schedule_definitions where user_id = $1::uuid`, userID)
		_, _ = fixture.pool.Exec(ctx, `delete from app.users where id = $1::uuid`, userID)
	})
	if _, err := fixture.pool.Exec(ctx, `insert into app.schedule_definitions (
		user_id, schedule_type, timezone, local_time, days_of_week,
		enabled, catchup_policy, catchup_grace, next_due_at
	) values ($1::uuid, 'daily_digest', 'UTC', '08:00',
		array[1,2,3,4,5,6,7]::smallint[], true, 'catch_up', interval '1 hour', $2)`,
		userID, now.Add(20*time.Hour)); err != nil {
		t.Fatalf("create research admission schedule: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.story_clusters
		set first_seen_at = $2, last_changed_at = $2 where id = $1::uuid`,
		fixture.clusterID, now); err != nil {
		t.Fatalf("place research story in upcoming digest window: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items set event_type = 'release'
		where id = $1::uuid`, fixture.itemID); err != nil {
		t.Fatalf("mark research story as a release: %v", err)
	}
}

func assertReconcileLifecycle(t *testing.T, fixture handoffFixture, expected string) {
	t.Helper()
	var lifecycle string
	if err := fixture.pool.QueryRow(context.Background(), `select lifecycle_state
		from app.items where id = $1::uuid`, fixture.itemID).Scan(&lifecycle); err != nil {
		t.Fatalf("inspect reconciliation lifecycle: %v", err)
	}
	if lifecycle != expected {
		t.Fatalf("item lifecycle = %q, want %q", lifecycle, expected)
	}
}

func assertReconcileJobRevision(t *testing.T, fixture handoffFixture, kind, identifierKey, identifier, revisionID string, expected int) {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(context.Background(), `select count(*)
		from river.river_job where queue = $1 and kind = $2
			and args ->> $3 = $4 and args ->> 'revisionId' = $5`,
		fixture.queue, kind, identifierKey, identifier, revisionID).Scan(&count); err != nil {
		t.Fatalf("inspect %s reconciliation job: %v", kind, err)
	}
	if count != expected {
		t.Fatalf("%s jobs for revision %s = %d, want %d", kind, revisionID, count, expected)
	}
}

func insertSupersedingReconcileRevision(t *testing.T, fixture handoffFixture) string {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte(uuid.NewString()))
	var revisionID string
	if err := fixture.pool.QueryRow(ctx, `insert into app.content_revisions (
		raw_document_id, previous_revision_id, normalized_sha256,
		normalized_text_object_key, parser_name, parser_version, title,
		language, normalized_bytes, outline, offset_map, warnings,
		change_kind, change_reason, material_change, observed_at
	) values ($1::uuid, $2::uuid, $3, $4, 'readeck-readability', 'v1',
		'Updated Go release', 'en', 18, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
		'material', 'updated source evidence', true, $5)
	returning id::text`, fixture.rawID, fixture.revisionID, digest[:],
		"normalized/"+fixture.sourceID+"/updated.txt", now).Scan(&revisionID); err != nil {
		t.Fatalf("insert superseding revision: %v", err)
	}
	t.Cleanup(func() {
		if _, err := fixture.pool.Exec(ctx, `update app.item_sources set revision_id = $2::uuid
			where item_id = $1::uuid`, fixture.itemID, fixture.revisionID); err != nil {
			t.Errorf("restore primary source revision: %v", err)
		}
		if _, err := fixture.pool.Exec(ctx, `update app.items set current_revision_id = $2::uuid
			where id = $1::uuid`, fixture.itemID, fixture.revisionID); err != nil {
			t.Errorf("restore current item revision: %v", err)
		}
		if _, err := fixture.pool.Exec(ctx, `delete from app.content_revisions
			where id = $1::uuid`, revisionID); err != nil {
			t.Errorf("remove superseding revision: %v", err)
		}
	})
	if _, err := fixture.pool.Exec(ctx, `update app.item_sources set revision_id = $2::uuid
		where item_id = $1::uuid`, fixture.itemID, revisionID); err != nil {
		t.Fatalf("switch primary source revision: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items
		set current_revision_id = $2::uuid, lifecycle_state = 'clustered'
		where id = $1::uuid`, fixture.itemID, revisionID); err != nil {
		t.Fatalf("switch current item revision: %v", err)
	}
	return revisionID
}

func insertVerifiedReconcileClaim(t *testing.T, fixture handoffFixture) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("verified research claim " + fixture.revisionID))
	var runID, claimID string
	if err := fixture.pool.QueryRow(ctx, `insert into app.ai_runs (
		item_id, revision_id, purpose, model_config_id, prompt_version_id,
		background, state, input_sha256, started_at, completed_at
	) select $1::uuid, $2::uuid, 'structured_extraction', model.id, prompt.id,
		false, 'completed', $3, $4, $4
	from app.model_configs model cross join app.prompt_versions prompt
	where model.role = 'fast' and model.enabled
		and prompt.purpose = 'structured_extraction' and prompt.active
	returning id::text`, fixture.itemID, fixture.revisionID, digest[:], now).Scan(&runID); err != nil {
		t.Fatalf("insert completed extraction: %v", err)
	}
	t.Cleanup(func() {
		if claimID != "" {
			if _, err := fixture.pool.Exec(ctx, `delete from app.evidence_spans
				where claim_id = $1::uuid`, claimID); err != nil {
				t.Errorf("remove verified evidence span: %v", err)
			}
			if _, err := fixture.pool.Exec(ctx, `delete from app.claims
				where id = $1::uuid`, claimID); err != nil {
				t.Errorf("remove verified claim: %v", err)
			}
		}
		if _, err := fixture.pool.Exec(ctx, `delete from app.ai_runs
			where id = $1::uuid`, runID); err != nil {
			t.Errorf("remove completed extraction: %v", err)
		}
	})
	if err := fixture.pool.QueryRow(ctx, `insert into app.claims (
		item_id, revision_id, ai_run_id, claim_index, claim_type, claim_text,
		normalized_value, confidence, material, verification_state
	) values ($1::uuid, $2::uuid, $3::uuid, 0, 'release',
		'Go release is published', '"published"'::jsonb, 'high', true, 'verified_span')
	returning id::text`, fixture.itemID, fixture.revisionID, runID).Scan(&claimID); err != nil {
		t.Fatalf("insert verified claim: %v", err)
	}
	quoteHash := sha256.Sum256([]byte("Go release is published"))
	if _, err := fixture.pool.Exec(ctx, `insert into app.evidence_spans (
		claim_id, revision_id, span_identifier, section_path,
		start_offset, end_offset, quote_hash
	) values ($1::uuid, $2::uuid, 'span_0001', 'Release', 0, 23, $3)`,
		claimID, fixture.revisionID, quoteHash[:]); err != nil {
		t.Fatalf("insert verified evidence span: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items set lifecycle_state = 'ready'
		where id = $1::uuid`, fixture.itemID); err != nil {
		t.Fatalf("mark item ready for research: %v", err)
	}
}
