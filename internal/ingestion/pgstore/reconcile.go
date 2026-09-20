package pgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/research"
	researchstore "github.com/traweezy/relantern/internal/research/pgstore"
)

type pendingItem struct {
	itemID     string
	revisionID string
}

// ReconcilePending re-arms work that was held while a provider capability was
// disabled. Each enabled stage gets a fair share of the bounded batch.
func (store *Store) ReconcilePending(ctx context.Context, capabilities ingestion.HandoffCapabilities, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, errors.New("intelligence handoff batch size must be between 1 and 500")
	}
	stages := 0
	if capabilities.EmbeddingModelID != "" {
		stages++
	}
	if capabilities.ExtractEnabled {
		stages++
	}
	if capabilities.ResearchEnabled {
		stages++
	}
	if stages == 0 {
		return 0, nil
	}
	if (capabilities.ExtractEnabled || capabilities.ResearchEnabled) && store.clock == nil {
		return 0, errors.New("source intelligence reconciliation requires a clock")
	}
	if limit < stages {
		return 0, fmt.Errorf("intelligence handoff batch size must cover %d enabled stages", stages)
	}
	perStage := limit / stages
	remainder := limit % stages
	nextBudget := func() int {
		budget := perStage
		if remainder > 0 {
			budget++
			remainder--
		}
		return budget
	}
	total := 0
	var stageErrors []error
	if capabilities.EmbeddingModelID != "" {
		count, err := store.reconcileEmbeddings(ctx, capabilities.EmbeddingModelID, nextBudget())
		if err != nil {
			stageErrors = append(stageErrors, err)
		} else {
			total += count
		}
	}
	if capabilities.ExtractEnabled {
		count, err := store.reconcileExtractions(ctx, nextBudget())
		if err != nil {
			stageErrors = append(stageErrors, err)
		} else {
			total += count
		}
	}
	if capabilities.ResearchEnabled {
		count, err := store.reconcileResearch(ctx, nextBudget())
		if err != nil {
			stageErrors = append(stageErrors, err)
		} else {
			total += count
		}
	}
	return total, errors.Join(stageErrors...)
}

func (store *Store) reconcileEmbeddings(ctx context.Context, modelID string, limit int) (int, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin embedding reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select item.id::text, item.current_revision_id::text,
			coalesce(document.normalized_content, ''), revision.normalized_sha256
		from app.items item
		join app.content_revisions revision on revision.id = item.current_revision_id
		join app.item_sources source on source.item_id = item.id
			and source.revision_id = item.current_revision_id
			and source.source_role = 'primary'
		left join app.search_documents document on document.item_id = item.id
			and document.revision_id = item.current_revision_id
		where item.status <> 'suppressed' and item.lifecycle_state <> 'failed_terminal'
			and not exists (
				select 1 from app.embeddings embedding
				where embedding.id = document.embedding_id
					and embedding.entity_type = 'item' and embedding.entity_id = item.id
					and embedding.model_id = $1
			)
			and not exists (
				select 1 from river.river_job job
				where job.kind = $2 and job.args @> jsonb_build_object(
					'entityType', 'item', 'entityId', item.id::text,
					'revisionId', item.current_revision_id::text, 'modelId', $1::text,
					'projectionVersion', $3::int)
			)
		order by item.first_seen_at, item.id
		limit $4
		for update of item skip locked`, modelID, jobqueue.ReembedEntityKind,
		jobqueue.ReembedProjectionVersion, limit)
	if err != nil {
		return 0, fmt.Errorf("select unindexed current revisions: %w", err)
	}
	type pendingEmbedding struct {
		itemID, revisionID, normalizedContent string
		revisionDigest                        []byte
	}
	items := make([]pendingEmbedding, 0, limit)
	for rows.Next() {
		var item pendingEmbedding
		if err := rows.Scan(&item.itemID, &item.revisionID,
			&item.normalizedContent, &item.revisionDigest); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan unindexed revision: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate unindexed revisions: %w", err)
	}
	rows.Close()
	inserted := 0
	for _, item := range items {
		// A completed legacy job may already have produced the exact vector.
		// Pin it without invoking the embedding provider again. Verify both
		// the current revision text and the immutable embedding input digest.
		contentDigest := sha256.Sum256([]byte(item.normalizedContent))
		if item.normalizedContent != "" && bytes.Equal(contentDigest[:], item.revisionDigest) {
			input, err := embedding.BoundedInput(item.normalizedContent)
			if err == nil {
				inputDigest := embedding.ContentDigest(input)
				result, err := tx.Exec(ctx, `
					update app.search_documents document
					set embedding_id = vector.id, updated_at = now()
					from app.embeddings vector
					where document.item_id = $1::uuid
						and document.revision_id = $2::uuid
						and vector.entity_type = 'item'
						and vector.entity_id = document.item_id
						and vector.model_id = $3
						and vector.content_sha256 = $4`,
					item.itemID, item.revisionID, modelID, inputDigest[:])
				if err != nil {
					return 0, fmt.Errorf("pin existing embedding: %w", err)
				}
				if result.RowsAffected() == 1 {
					continue
				}
			}
		}
		_, newJob, err := store.jobs.EnqueueReembedEntity(ctx, tx, jobqueue.ReembedEntityArgs{
			EntityType: "item", EntityID: item.itemID,
			RevisionID: item.revisionID, ModelID: modelID,
		})
		if err != nil {
			return 0, fmt.Errorf("reconcile item embedding: %w", err)
		}
		if newJob {
			inserted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit embedding reconciliation: %w", err)
	}
	return inserted, nil
}

func (store *Store) reconcileExtractions(ctx context.Context, limit int) (int, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin extraction reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select item.id::text, item.current_revision_id::text
		from app.items item
		join app.item_sources source on source.item_id = item.id
			and source.revision_id = item.current_revision_id
			and source.source_role = 'primary' and source.source_tier in ('T0', 'T1')
		where item.lifecycle_state = 'clustered' and item.status <> 'suppressed'
			and not exists (
				select 1 from river.river_job job
				where job.kind = $1 and job.args @> jsonb_build_object(
					'itemId', item.id::text, 'revisionId', item.current_revision_id::text)
			)
		order by item.first_seen_at, item.id
		limit $2
		for update of item skip locked`, jobqueue.ExtractItemKind, limit)
	if err != nil {
		return 0, fmt.Errorf("select unextracted current revisions: %w", err)
	}
	items, err := collectPendingItems(rows, limit)
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, item := range items {
		_, newJob, err := store.jobs.EnqueueExtractItem(ctx, tx, jobqueue.ExtractItemArgs{
			ItemID: item.itemID, RevisionID: item.revisionID,
		})
		if err != nil {
			return 0, fmt.Errorf("reconcile item extraction: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update app.items set lifecycle_state = 'awaiting_ai', updated_at = $3
			where id = $1::uuid and current_revision_id = $2::uuid
				and lifecycle_state = 'clustered'`, item.itemID, item.revisionID, store.clock.Now().UTC()); err != nil {
			return 0, fmt.Errorf("mark item awaiting extraction: %w", err)
		}
		if newJob {
			inserted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit extraction reconciliation: %w", err)
	}
	return inserted, nil
}

func (store *Store) reconcileResearch(ctx context.Context, limit int) (int, error) {
	// The cursor rotates a bounded scan through old clusters. Without it, a
	// large set of unchanged candidate snapshots could indefinitely hide a
	// later changed cluster behind LIMIT.
	store.researchMu.Lock()
	defer store.researchMu.Unlock()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin research reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	admitted, err := researchstore.ResearchAdmissions(ctx, tx, store.clock.Now().UTC(), nil)
	if err != nil {
		return 0, fmt.Errorf("select research admission: %w", err)
	}
	if len(admitted) == 0 {
		return 0, tx.Commit(ctx)
	}
	admittedIDs := make([]string, 0, len(admitted))
	for clusterID := range admitted {
		admittedIDs = append(admittedIDs, clusterID)
	}
	scanLimit := limit * 10
	if scanLimit < 100 {
		scanLimit = 100
	}
	if scanLimit > 500 {
		scanLimit = 500
	}
	rows, err := tx.Query(ctx, `
		select cluster.id::text, item.current_revision_id::text
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		join app.item_sources source on source.item_id = item.id
			and source.revision_id = item.current_revision_id
			and source.source_role = 'primary' and source.source_tier in ('T0', 'T1')
		where item.lifecycle_state in ('ready', 'published') and item.status <> 'suppressed'
			and cluster.id = any($1::uuid[])
			and cluster.id > coalesce(nullif($2::text, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
			and exists (
				select 1 from app.cluster_members member
				join app.items evidence_item on evidence_item.id = member.item_id
				join app.item_sources evidence_source on evidence_source.item_id = evidence_item.id
					and evidence_source.revision_id = evidence_item.current_revision_id
					and evidence_source.source_tier in ('T0', 'T1')
				join app.claims claim on claim.item_id = evidence_item.id
					and claim.revision_id = evidence_item.current_revision_id
					and claim.verification_state = 'verified_span'
				where member.cluster_id = cluster.id
			)
		order by cluster.id
		limit $3
		for update of item skip locked`, admittedIDs, store.researchCursor, scanLimit)
	if err != nil {
		return 0, fmt.Errorf("select ready current stories: %w", err)
	}
	stories, err := collectPendingItems(rows, scanLimit)
	if err != nil {
		return 0, err
	}
	inserted := 0
	lastProcessedID := ""
	processedAll := true
	for index, story := range stories {
		lastProcessedID = story.itemID
		if _, eligible := admitted[story.itemID]; !eligible {
			continue
		}
		currentRevisionID, inputSHA256, err := researchstore.CurrentInputSHA256(ctx, tx, story.itemID)
		if err != nil {
			return 0, fmt.Errorf("inspect current research facts: %w", err)
		}
		if currentRevisionID != story.revisionID {
			continue
		}
		var attempted, queued bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from app.ai_runs run
				where run.item_id = (
					select primary_item_id from app.story_clusters where id = $1::uuid)
					and run.cluster_id = $1::uuid and run.revision_id = $2::uuid
					and run.purpose = $4 and run.input_sha256 = decode($3, 'hex')
			), exists (
				select 1 from river.river_job job
				where job.kind = $5 and job.args @> jsonb_build_object(
						'clusterId', $1::text, 'revisionId', $2::text, 'inputSha256', $3::text)
			)`, story.itemID, story.revisionID, inputSHA256,
			research.Purpose, jobqueue.ResearchStoryKind).Scan(&attempted, &queued); err != nil {
			return 0, fmt.Errorf("inspect research snapshot handoff: %w", err)
		}
		if attempted || queued {
			continue
		}
		_, newJob, err := store.jobs.EnqueueResearchStory(ctx, tx, jobqueue.ResearchStoryArgs{
			ClusterID: story.itemID, RevisionID: story.revisionID, InputSHA256: inputSHA256,
		})
		if err != nil {
			return 0, fmt.Errorf("reconcile story research: %w", err)
		}
		if newJob {
			inserted++
			if inserted == limit {
				processedAll = index == len(stories)-1
				break
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit research reconciliation: %w", err)
	}
	if processedAll && len(stories) < scanLimit {
		store.researchCursor = ""
	} else {
		store.researchCursor = lastProcessedID
	}
	return inserted, nil
}

func collectPendingItems(rows pgx.Rows, limit int) ([]pendingItem, error) {
	defer rows.Close()
	items := make([]pendingItem, 0, limit)
	for rows.Next() {
		var item pendingItem
		if err := rows.Scan(&item.itemID, &item.revisionID); err != nil {
			return nil, fmt.Errorf("scan pending intelligence handoff: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending intelligence handoffs: %w", err)
	}
	return items, nil
}
