package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestCatchUpPagesCurrentSupportingAdvisories(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, err := pool.Exec(cleanupCtx, `
			delete from river.river_job
			where queue = 'test_alert_assessment' and (
				(kind = 'assess_critical_advisory' and args->>'rawDocumentId' = $1)
				or (kind = 'reassess_current_advisories' and args->>'userId' = $2)
			)`, fixture.childID, fixture.userID)
		if err != nil {
			t.Errorf("clean up catch-up jobs: %v", err)
		}
	})
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: fixture.payload}, clock.NewFixed(fixture.now))
	if err != nil {
		t.Fatal(err)
	}

	// The advisory remains eligible when another revision becomes the story's
	// primary source. A later raw child for its source entry supersedes it.
	primaryDigest := sha256.Sum256([]byte("primary story"))
	var primaryRevisionID string
	err = pool.QueryRow(ctx, `
		insert into app.content_revisions (raw_document_id, normalized_sha256,
			normalized_text_object_key, parser_name, parser_version, title,
			language, normalized_bytes, outline, offset_map, warnings,
			change_kind, change_reason, material_change, observed_at)
		values ($1::uuid, $2, $3, 'github_advisories', '1', 'Primary story',
			'en', 13, '[]', '[]', '[]', 'initial', 'initial parse', false, $4)
		returning id::text`, fixture.parentID, primaryDigest[:],
		"normalized/"+fixture.sourceID+"/"+hex.EncodeToString(primaryDigest[:])+".txt",
		fixture.now.Add(time.Minute)).Scan(&primaryRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `update app.item_sources set source_role = 'supporting', sort_order = 1 where revision_id = $1::uuid`, fixture.revisionID)
	fixture.exec(t, ctx, `
		insert into app.item_sources (revision_id, item_id, canonical_url, source_role, source_tier, sort_order)
		values ($1::uuid, $2::uuid, $3, 'primary', 'T1', 0)`, primaryRevisionID, fixture.itemID, fixture.endpointURL)
	fixture.exec(t, ctx, `update app.items set current_revision_id = $2::uuid, status = 'updated' where id = $1::uuid`, fixture.itemID, primaryRevisionID)

	args := jobqueue.ReassessCurrentAdvisoriesArgs{UserID: fixture.userID, SettingsVersion: 1}
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null where id = $1::uuid`, fixture.watchID)
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go' where id = $1::uuid`, fixture.watchID)

	newerDigest := sha256.Sum256([]byte("unparsed newer advisory"))
	var newerID string
	err = pool.QueryRow(ctx, `
		insert into app.raw_documents (source_id, canonical_url, object_key,
			raw_sha256, first_seen_at, first_fetched_at, content_policy,
			parent_raw_document_id, source_entry_id, source_registry_id,
			source_connector, source_content_type, ingestion_error_code, ingestion_failed_at)
		values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $6::uuid,
			$7::uuid, $8, 'source_entry', 'application/json', 'pending_parse', $5)
		returning id::text`, fixture.sourceID, fixture.publicURL,
		"raw/"+fixture.sourceID+"/"+hex.EncodeToString(newerDigest[:])+".json",
		newerDigest[:], fixture.now.Add(time.Minute), fixture.parentID,
		fixture.entryID, fixture.registryID).Scan(&newerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
	fixture.exec(t, ctx, `delete from app.raw_documents where id = $1::uuid`, newerID)

	// Seventeen eligible revisions require two durable pages. The synthetic
	// supporting revisions exercise the pagination boundary without changing
	// the assessor's matching rules.
	for i := 0; i < advisoryCatchUpPageSize; i++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("supporting revision %d", i)))
		var revisionID string
		err := pool.QueryRow(ctx, `
			insert into app.content_revisions (raw_document_id, normalized_sha256,
				normalized_text_object_key, parser_name, parser_version, title,
				language, normalized_bytes, outline, offset_map, warnings,
				change_kind, change_reason, material_change, observed_at)
			values ($1::uuid, $2, $3, 'source_entry', '1', 'Critical widget update',
				'en', 23, '[]', '[]', '[]', 'initial', 'initial parse', false, $4)
			returning id::text`, fixture.childID, digest[:],
			"normalized/"+fixture.sourceID+"/"+hex.EncodeToString(digest[:])+".txt",
			fixture.now.Add(time.Duration(i+1)*time.Minute)).Scan(&revisionID)
		if err != nil {
			t.Fatal(err)
		}
		fixture.exec(t, ctx, `
			insert into app.item_sources (revision_id, item_id, canonical_url,
				source_role, source_tier, sort_order)
			values ($1::uuid, $2::uuid, $3, 'supporting', 'T1', $4)`,
			revisionID, fixture.itemID, fixture.publicURL, i+2)
	}
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, advisoryCatchUpPageSize)
	var cursor string
	err = pool.QueryRow(ctx, `
		select args->>'cursor' from river.river_job
		where queue = 'test_alert_assessment' and kind = 'reassess_current_advisories'
			and args->>'userId' = $1 and args->>'cursor' <> ''`, fixture.userID).Scan(&cursor)
	if err != nil || cursor == "" {
		t.Fatalf("durable next page cursor = %q, %v", cursor, err)
	}
	args.Cursor = cursor
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, advisoryCatchUpPageSize+1)
	var successorCount int
	err = pool.QueryRow(ctx, `
		select count(*) from river.river_job
		where queue = 'test_alert_assessment' and kind = 'reassess_current_advisories'
			and args->>'userId' = $1 and args->>'cursor' <> ''`, fixture.userID).Scan(&successorCount)
	if err != nil || successorCount != 1 {
		t.Fatalf("successor pages = %d, %v, want 1", successorCount, err)
	}
}

func assertCatchUpJobCount(t *testing.T, ctx context.Context, rawDocumentID string, pool *pgxpool.Pool, want int) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `
		select count(*) from river.river_job
		where queue = 'test_alert_assessment' and kind = 'assess_critical_advisory'
			and args->>'rawDocumentId' = $1`, rawDocumentID).Scan(&count)
	if err != nil || count != want {
		t.Fatalf("queued catch-up assessments = %d, %v, want %d", count, err, want)
	}
}
