package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/sources"
)

const advisoryCatchUpPageSize = 16

type advisoryRevision struct {
	rawDocumentID string
	revisionID    string
	endpointURL   string
	parentURL     string
}

// CatchUp pages through current official advisory revisions. It queues the
// existing assessor rather than interpreting advisory ranges in this path.
// Each page and its successor commit together, so retries cannot silently
// leave a partially enqueued page behind.
func (store *Store) CatchUp(ctx context.Context, args jobqueue.ReassessCurrentAdvisoriesArgs) error {
	if _, err := uuid.Parse(args.UserID); err != nil || args.SettingsVersion < 1 {
		return errors.New("advisory catch-up requires an owner and settings version")
	}
	if args.Cursor != "" {
		if _, err := uuid.Parse(args.Cursor); err != nil {
			return errors.New("advisory catch-up cursor is invalid")
		}
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin advisory catch-up page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var hasActiveWatch bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from app.watched_technologies
			where user_id = $1::uuid and ecosystem is not null and status = 'active'
		)`, args.UserID).Scan(&hasActiveWatch); err != nil {
		return fmt.Errorf("check active advisory watches: %w", err)
	}
	if !hasActiveWatch {
		return tx.Commit(ctx)
	}

	rows, err := tx.Query(ctx, `
		select distinct revision.id, child.id, endpoint.url, parent.canonical_url
		from app.content_revisions revision
		join app.raw_documents child on child.id = revision.raw_document_id
		join app.raw_documents parent on parent.id = child.parent_raw_document_id
		join app.source_entries entry on entry.id = child.source_entry_id
		join app.sources source on source.id = child.source_id
		join app.source_endpoints endpoint on endpoint.registry_id = child.source_registry_id
		join app.item_sources item_source on item_source.revision_id = revision.id
		join app.items item on item.id = item_source.item_id
		where revision.id > coalesce(nullif($1::text, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
			and child.source_id = parent.source_id and child.source_id = entry.source_id
			and parent.source_registry_id = child.source_registry_id
			and parent.source_connector = 'github_advisories'
			and child.source_connector = 'source_entry'
			and endpoint.source_id = child.source_id and endpoint.connector = 'github_advisories'
			and (parent.canonical_url = endpoint.url or (
				endpoint.url = '`+sources.GlobalReviewedAdvisoriesURL+`' and exists (
					select 1 from app.source_fetches source_fetch
					where source_fetch.endpoint_id = endpoint.id
						and source_fetch.outcome = 'stored'
						and source_fetch.final_url = parent.canonical_url
						and source_fetch.object_key = parent.object_key
						and source_fetch.raw_sha256 = parent.raw_sha256
						and source_fetch.attempted_at = parent.first_seen_at
						and source_fetch.completed_at = parent.first_fetched_at
				)
			))
			and child.content_policy = 'link-and-excerpt'
			and child.object_key is not null and child.raw_pruned_at is null
			and child.ingestion_error_code is null and parent.ingestion_error_code is null
			and source.enabled and source.validation_state = 'active'
			and source.trust_tier in ('T0', 'T1')
			and item.status in ('active', 'updated')
			and item_source.source_tier = source.trust_tier
			and item_source.canonical_url = child.canonical_url
			and not exists (
				select 1 from app.raw_documents newer
				where newer.source_entry_id = child.source_entry_id
					and (newer.first_seen_at, newer.id) > (child.first_seen_at, child.id)
			)
		order by revision.id
		limit $2`, args.Cursor, advisoryCatchUpPageSize)
	if err != nil {
		return fmt.Errorf("select current advisory catch-up page: %w", err)
	}
	page := make([]advisoryRevision, 0, advisoryCatchUpPageSize)
	for rows.Next() {
		var candidate advisoryRevision
		if err := rows.Scan(&candidate.revisionID, &candidate.rawDocumentID,
			&candidate.endpointURL, &candidate.parentURL); err != nil {
			rows.Close()
			return fmt.Errorf("scan advisory catch-up candidate: %w", err)
		}
		page = append(page, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate advisory catch-up page: %w", err)
	}
	rows.Close()

	for _, candidate := range page {
		if !officialAdvisoryParent(candidate.endpointURL, candidate.parentURL) {
			continue
		}
		if _, _, err := store.jobs.EnqueueBackfillCriticalAdvisory(ctx, tx,
			jobqueue.AssessCriticalAdvisoryArgs{
				RawDocumentID: candidate.rawDocumentID, RevisionID: candidate.revisionID,
			}); err != nil {
			return err
		}
	}
	if len(page) == advisoryCatchUpPageSize {
		args.Cursor = page[len(page)-1].revisionID
		if _, _, err := store.jobs.EnqueueReassessCurrentAdvisories(ctx, tx, args); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit advisory catch-up page: %w", err)
	}
	return nil
}
