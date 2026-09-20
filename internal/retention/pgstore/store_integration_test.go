package pgstore_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/parsing"
	parsingstore "github.com/traweezy/relantern/internal/parsing/pgstore"
	"github.com/traweezy/relantern/internal/retention"
	retentionstore "github.com/traweezy/relantern/internal/retention/pgstore"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

type retentionObjects struct {
	deleted []string
	fail    bool
}

type blockingRetentionObjects struct {
	entered chan struct{}
	release chan struct{}
}

func (objects *blockingRetentionObjects) Delete(ctx context.Context, _ string) error {
	close(objects.entered)
	select {
	case <-objects.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (objects *retentionObjects) Delete(_ context.Context, key string) error {
	objects.deleted = append(objects.deleted, key)
	if objects.fail {
		return errors.New("object store unavailable")
	}
	return nil
}

type retentionFixture struct {
	pool     *pgxpool.Pool
	sourceID string
	old      time.Time
	now      time.Time
}

func TestRetentionStoreRejectsSingleConnectionPool(t *testing.T) {
	configuration, err := pgxpool.ParseConfig("postgres://relantern@127.0.0.1/relantern?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	configuration.MaxConns = 1
	configuration.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := retentionstore.New(pool); err == nil {
		t.Fatal("retention store accepted a pool that cannot hold its source guard")
	}
}

func newRetentionFixture(t *testing.T) *retentionFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for retention integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	fixture := &retentionFixture{
		pool: pool, sourceID: "retention-" + uuid.NewString(),
		old: now.Add(-400 * 24 * time.Hour), now: now,
	}
	_, err = pool.Exec(context.Background(), `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at
		) values ($1, 'Retention fixture', 'T0', 'owner', 'owner', 'active',
			'https://example.test', 'link-and-excerpt', false, array['test'], $2)`,
		fixture.sourceID, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `delete from app.source_parse_attempts where raw_document_id in (
			select id from app.raw_documents where source_id = $1)`, fixture.sourceID)
		_, _ = pool.Exec(ctx, `delete from app.item_sources where revision_id in (
			select revision.id from app.content_revisions revision
			join app.raw_documents raw on raw.id = revision.raw_document_id where raw.source_id = $1)`, fixture.sourceID)
		_, _ = pool.Exec(ctx, `delete from app.items where current_revision_id in (
			select revision.id from app.content_revisions revision
			join app.raw_documents raw on raw.id = revision.raw_document_id where raw.source_id = $1)`, fixture.sourceID)
		_, _ = pool.Exec(ctx, `delete from app.content_revisions where raw_document_id in (
			select id from app.raw_documents where source_id = $1)`, fixture.sourceID)
		_, _ = pool.Exec(ctx, `delete from app.raw_documents where source_id = $1`, fixture.sourceID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, fixture.sourceID)
	})
	return fixture
}

func (fixture *retentionFixture) raw(t *testing.T, suffix string, observedAt time.Time) (string, string) {
	t.Helper()
	digest := sha256.Sum256([]byte(suffix))
	key := "raw/" + fixture.sourceID + "/2025/01/01/" + fmtDigest(digest) + ".json"
	var id string
	err := fixture.pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256,
			first_seen_at, first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt') returning id::text`,
		fixture.sourceID, "https://example.test/"+suffix, key, digest[:], observedAt).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id, key
}

func (fixture *retentionFixture) revision(t *testing.T, rawID string, normalizedKey string, observedAt time.Time) string {
	t.Helper()
	digest := sha256.Sum256([]byte(normalizedKey))
	var id string
	err := fixture.pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, language, normalized_bytes,
			outline, offset_map, warnings, change_kind, change_reason,
			material_change, observed_at
		) values ($1::uuid, $2, $3, 'retention-fixture', '1', 'Fixture',
			'und', 7, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
			'initial', 'fixture', false, $4) returning id::text`,
		rawID, digest[:], normalizedKey, observedAt).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture *retentionFixture) item(t *testing.T, revisionID string, lifecycle string) string {
	t.Helper()
	var id string
	err := fixture.pool.QueryRow(context.Background(), `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title,
			normalized_author, slug, lifecycle_state, first_seen_at,
			status, simhash
		) values ($1::uuid, 'https://example.test/item', 'Fixture', 'fixture',
			'', $2, $3, $4, 'active', $5) returning id::text`,
		revisionID, "retention-"+uuid.NewString(), lifecycle, fixture.old, make([]byte, 8)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.pool.Exec(context.Background(), `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		) values ($1::uuid, $2::uuid, 'https://example.test/item', 'primary', 'T0', 0)`,
		revisionID, id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fmtDigest(digest [sha256.Size]byte) string {
	return fmt.Sprintf("%x", digest[:])
}

func TestRetentionRunLedgerIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for retention integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := retentionstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "retention:2099-12-31"
	_, _ = pool.Exec(ctx, `delete from app.retention_runs where idempotency_key = $1`, key)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from app.retention_runs where idempotency_key = $1`, key)
	})
	now := time.Date(2099, 12, 31, 12, 0, 0, 0, time.UTC)
	runID, counts, execute, err := store.Start(ctx, key, retention.DefaultPolicy(), now)
	if err != nil || !execute || runID == "" || counts != (retention.Counts{}) {
		t.Fatalf("Start() = %q, %+v, %t, %v", runID, counts, execute, err)
	}
	want := retention.Counts{FetchAttemptsPruned: 2, OutboxEventsPruned: 1}
	if err := store.Complete(ctx, runID, want, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	secondID, secondCounts, secondExecute, err := store.Start(ctx, key, retention.DefaultPolicy(), now.Add(time.Minute))
	if err != nil || secondExecute || secondID != runID || secondCounts != want {
		t.Fatalf("replayed Start() = %q, %+v, %t, %v", secondID, secondCounts, secondExecute, err)
	}
	if raw, err := store.RawCandidates(ctx, time.Unix(0, 0), 10); err != nil || len(raw) != 0 {
		t.Fatalf("RawCandidates() = %+v, %v", raw, err)
	}
	if normalized, err := store.NormalizedCandidates(ctx, time.Unix(0, 0), 10); err != nil || len(normalized) != 0 {
		t.Fatalf("NormalizedCandidates() = %+v, %v", normalized, err)
	}
}

func TestFailedRetentionRunPreservesCountsWhenResumed(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for retention integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := retentionstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "retention:2099-12-30"
	_, _ = pool.Exec(ctx, `delete from app.retention_runs where idempotency_key = $1`, key)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from app.retention_runs where idempotency_key = $1`, key)
	})
	now := time.Date(2099, 12, 30, 12, 0, 0, 0, time.UTC)
	runID, _, execute, err := store.Start(ctx, key, retention.DefaultPolicy(), now)
	if err != nil || !execute {
		t.Fatalf("Start() = %q, %t, %v", runID, execute, err)
	}
	want := retention.Counts{RawObjectsPruned: 2, MutationStatesCompacted: 1}
	if err := store.Fail(ctx, runID, "fixture_failure", want, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	resumedID, resumedCounts, resumed, err := store.Start(ctx, key, retention.DefaultPolicy(), now.Add(time.Minute))
	if err != nil || !resumed || resumedID != runID || resumedCounts != want {
		t.Fatalf("resumed Start() = %q, %+v, %t, %v", resumedID, resumedCounts, resumed, err)
	}
}

func TestRawRetentionRechecksLatePublicationAndPreservesPendingReplay(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	rawID, key := fixture.raw(t, "late-publication", fixture.old)
	revisionID := fixture.revision(t, rawID, "normalized/"+fixture.sourceID+"/late.txt", fixture.old)
	cutoff := fixture.now.Add(-retention.DefaultPolicy().RawSnapshots)
	candidates, err := store.RawCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 1 || candidates[0].ID != rawID {
		t.Fatalf("initial raw candidates = %+v, %v", candidates, err)
	}
	itemID := fixture.item(t, revisionID, "ready")
	if _, err := fixture.pool.Exec(ctx, `update app.items set lifecycle_state = 'published' where id = $1::uuid`, itemID); err != nil {
		t.Fatal(err)
	}
	objects := &retentionObjects{}
	pruned, err := store.PruneRaw(ctx, candidates[0], cutoff, fixture.now, objects)
	if err != nil || pruned || len(objects.deleted) != 0 {
		t.Fatalf("late published raw prune = %t, %v, deleted=%v", pruned, err, objects.deleted)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items set lifecycle_state = 'ready' where id = $1::uuid`, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.raw_documents
		set ingestion_error_code = 'pending_parse', ingestion_failed_at = $2
		where id = $1::uuid`, rawID, fixture.now); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.RawCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("pending replay raw candidates = %+v, %v", candidates, err)
	}
	pruned, err = store.PruneRaw(ctx, retention.ObjectCandidate{ID: rawID, Key: key}, cutoff, fixture.now, objects)
	if err != nil || pruned || len(objects.deleted) != 0 {
		t.Fatalf("stale pending raw prune = %t, %v, deleted=%v", pruned, err, objects.deleted)
	}
	var retainedKey string
	if err := fixture.pool.QueryRow(ctx, `select object_key from app.raw_documents where id = $1::uuid`, rawID).Scan(&retainedKey); err != nil || retainedKey != key {
		t.Fatalf("pending raw evidence key = %q, %v", retainedKey, err)
	}
}

func TestRawRetentionPreservesPendingAdvisoryObservationEvidence(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	priorParentID, _ := fixture.raw(t, "advisory-prior-parent", fixture.old)
	currentParentID, currentParentKey := fixture.raw(t, "advisory-current-parent", fixture.old)
	childID, childKey := fixture.raw(t, "advisory-reused-child", fixture.old)
	var sourceEntryID string
	if err := fixture.pool.QueryRow(ctx, `
		insert into app.source_entries (source_id, external_id, first_seen_at, last_seen_at)
		values ($1, 'github_advisories:GHSA-abcd-1234-efgh', $2, $2)
		returning id::text`, fixture.sourceID, fixture.old).Scan(&sourceEntryID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		update app.raw_documents
		set parent_raw_document_id = $2::uuid, source_entry_id = $3::uuid
		where id = $1::uuid`, childID, priorParentID, sourceEntryID); err != nil {
		t.Fatal(err)
	}
	cutoff := fixture.now.Add(-retention.DefaultPolicy().RawSnapshots)
	candidates, err := store.RawCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 3 {
		t.Fatalf("advisory evidence before observation = %+v, %v", candidates, err)
	}
	var observationID int64
	if err := fixture.pool.QueryRow(ctx, `
		insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at
		) values ($1, $2,
			nextval(pg_get_serial_sequence('app.source_fetches', 'id')),
			$3::uuid, $4)
		returning id`, fixture.sourceID, fixture.sourceID+"-advisories",
		currentParentID, fixture.now).Scan(&observationID); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.RawCandidates(ctx, cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.ID == currentParentID || candidate.ID == childID {
			t.Fatalf("pending advisory evidence was selected for retention: %+v", candidates)
		}
	}
	objects := &retentionObjects{}
	for _, candidate := range []retention.ObjectCandidate{
		{ID: currentParentID, Key: currentParentKey},
		{ID: childID, Key: childKey},
	} {
		pruned, pruneErr := store.PruneRaw(ctx, candidate, cutoff, fixture.now, objects)
		if pruneErr != nil || pruned {
			t.Fatalf("late pending advisory raw prune = %t, %v for %s", pruned, pruneErr, candidate.ID)
		}
	}
	if len(objects.deleted) != 0 {
		t.Fatalf("pending advisory object was deleted: %v", objects.deleted)
	}
	for _, id := range []string{currentParentID, childID} {
		var retained bool
		if err := fixture.pool.QueryRow(ctx, `
			select object_key is not null and raw_pruned_at is null
			from app.raw_documents where id = $1::uuid`, id).Scan(&retained); err != nil || !retained {
			t.Fatalf("pending advisory raw %s retained = %t, %v", id, retained, err)
		}
	}
	if _, err := fixture.pool.Exec(ctx, `
		update app.advisory_collection_observations
		set state = 'processed', entry_count = 1,
			split_completed_at = $2, processed_at = $2
		where id = $1`, observationID, fixture.now); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.RawCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 3 {
		t.Fatalf("processed advisory evidence candidates = %+v, %v", candidates, err)
	}
}

func TestRawRetentionSerializesAdvisoryCaptureThroughObjectDelete(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	rawID, key := fixture.raw(t, "advisory-retention-capture", fixture.old)
	cutoff := fixture.now.Add(-retention.DefaultPolicy().RawSnapshots)
	objects := &blockingRetentionObjects{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-objects.release:
		default:
			close(objects.release)
		}
	}()
	type pruneResult struct {
		pruned bool
		err    error
	}
	pruned := make(chan pruneResult, 1)
	go func() {
		result, pruneError := store.PruneRaw(ctx,
			retention.ObjectCandidate{ID: rawID, Key: key}, cutoff, fixture.now, objects)
		pruned <- pruneResult{pruned: result, err: pruneError}
	}()
	select {
	case <-objects.entered:
	case <-ctx.Done():
		t.Fatal("raw retention did not reach object deletion")
	}
	acquired := make(chan struct{})
	captured := make(chan error, 1)
	restoredKey := strings.TrimSuffix(key, ".json") + "-restored.json"
	go func() {
		tx, beginErr := fixture.pool.Begin(ctx)
		if beginErr != nil {
			captured <- beginErr
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var lockedSourceID string
		if lockErr := tx.QueryRow(ctx, `
			select id from app.sources where id = $1 for update`, fixture.sourceID).
			Scan(&lockedSourceID); lockErr != nil {
			captured <- lockErr
			return
		}
		close(acquired)
		if _, updateErr := tx.Exec(ctx, `
			update app.raw_documents
			set object_key = $2, raw_pruned_at = null
			where id = $1::uuid`, rawID, restoredKey); updateErr != nil {
			captured <- updateErr
			return
		}
		if _, insertErr := tx.Exec(ctx, `
			insert into app.advisory_collection_observations (
				source_id, source_registry_id, source_fetch_id,
				parent_raw_document_id, observed_at
			) values ($1, $2,
				nextval(pg_get_serial_sequence('app.source_fetches', 'id')),
				$3::uuid, $4)`, fixture.sourceID, fixture.sourceID+"-advisories",
			rawID, fixture.now); insertErr != nil {
			captured <- insertErr
			return
		}
		captured <- tx.Commit(ctx)
	}()
	select {
	case <-acquired:
		t.Fatal("advisory capture acquired the source while retention was deleting its object")
	case err := <-captured:
		t.Fatalf("advisory capture ended before retention released its source lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(objects.release)
	select {
	case result := <-pruned:
		if result.err != nil || !result.pruned {
			t.Fatalf("raw retention = %t, %v", result.pruned, result.err)
		}
	case <-ctx.Done():
		t.Fatal("raw retention did not finish object deletion")
	}
	select {
	case err := <-captured:
		if err != nil {
			t.Fatalf("advisory capture after raw deletion: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("advisory capture did not resume after raw deletion")
	}
	var storedKey string
	if err := fixture.pool.QueryRow(ctx, `
		select object_key from app.raw_documents where id = $1::uuid`, rawID).Scan(&storedKey); err != nil || storedKey != restoredKey {
		t.Fatalf("restored advisory raw key = %q, %v", storedKey, err)
	}
	candidates, err := store.RawCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("new pending observation raw candidates = %+v, %v", candidates, err)
	}
}

func TestNormalizedRetentionChecksEverySharedReferenceAndDeletesOnce(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, _ := fixture.raw(t, "shared-first", fixture.old)
	secondRaw, _ := fixture.raw(t, "shared-second", fixture.now.Add(-24*time.Hour))
	digest := sha256.Sum256([]byte("shared normalized text"))
	key := "normalized/" + fixture.sourceID + "/" + fmtDigest(digest) + ".txt"
	firstRevision := fixture.revision(t, firstRaw, key, fixture.old)
	secondRevision := fixture.revision(t, secondRaw, key, fixture.now.Add(-24*time.Hour))
	cutoff := fixture.now.Add(-retention.DefaultPolicy().NormalizedRevision)
	candidates, err := store.NormalizedCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("shared key with young revision was eligible: %+v, %v", candidates, err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.content_revisions set observed_at = $2 where id = $1::uuid`, secondRevision, fixture.old); err != nil {
		t.Fatal(err)
	}
	itemID := fixture.item(t, secondRevision, "published")
	candidates, err = store.NormalizedCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("shared key with published revision was eligible: %+v, %v", candidates, err)
	}
	if _, err := fixture.pool.Exec(ctx, `update app.items set lifecycle_state = 'ready' where id = $1::uuid`, itemID); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.NormalizedCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 1 || candidates[0].Key != key {
		t.Fatalf("shared key eligible group = %+v, %v", candidates, err)
	}
	objects := &retentionObjects{}
	pruned, err := store.PruneNormalized(ctx, candidates[0], cutoff, fixture.now, objects)
	if err != nil || !pruned || len(objects.deleted) != 1 || objects.deleted[0] != key {
		t.Fatalf("shared normalized prune = %t, %v, deleted=%v", pruned, err, objects.deleted)
	}
	var remaining int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from app.content_revisions
		where id = any($1::uuid[]) and normalized_text_object_key is not null`,
		[]string{firstRevision, secondRevision}).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("shared normalized references remaining = %d, %v", remaining, err)
	}
}

func TestNormalizedRetentionRechecksLatePublication(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	rawID, _ := fixture.raw(t, "normalized-late", fixture.old)
	digest := sha256.Sum256([]byte("late normalized text"))
	key := "normalized/" + fixture.sourceID + "/" + fmtDigest(digest) + ".txt"
	revisionID := fixture.revision(t, rawID, key, fixture.old)
	cutoff := fixture.now.Add(-retention.DefaultPolicy().NormalizedRevision)
	candidates, err := store.NormalizedCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("initial normalized candidates = %+v, %v", candidates, err)
	}
	fixture.item(t, revisionID, "published")
	objects := &retentionObjects{}
	pruned, err := store.PruneNormalized(ctx, candidates[0], cutoff, fixture.now, objects)
	if err != nil || pruned || len(objects.deleted) != 0 {
		t.Fatalf("late published normalized prune = %t, %v, deleted=%v", pruned, err, objects.deleted)
	}
	var retainedKey string
	if err := fixture.pool.QueryRow(ctx, `select normalized_text_object_key from app.content_revisions where id = $1::uuid`, revisionID).Scan(&retainedKey); err != nil || retainedKey != key {
		t.Fatalf("published normalized evidence key = %q, %v", retainedKey, err)
	}
}

func TestRetentionKeepsReferencesPrunedAfterAmbiguousObjectDelete(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	store, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	rawID, rawKey := fixture.raw(t, "delete-failure", fixture.old)
	normalizedKey := "normalized/" + fixture.sourceID + "/delete-failure.txt"
	revisionID := fixture.revision(t, rawID, normalizedKey, fixture.old)
	objects := &retentionObjects{fail: true}
	rawCutoff := fixture.now.Add(-retention.DefaultPolicy().RawSnapshots)
	pruned, err := store.PruneRaw(ctx, retention.ObjectCandidate{ID: rawID, Key: rawKey}, rawCutoff, fixture.now, objects)
	if err == nil || pruned {
		t.Fatalf("raw ambiguous delete = %t, %v", pruned, err)
	}
	var rawReferencePruned bool
	if err := fixture.pool.QueryRow(ctx, `select object_key is null and raw_pruned_at is not null from app.raw_documents where id = $1::uuid`, rawID).Scan(&rawReferencePruned); err != nil || !rawReferencePruned {
		t.Fatalf("raw reference pruned = %t, %v", rawReferencePruned, err)
	}
	normalizedCutoff := fixture.now.Add(-retention.DefaultPolicy().NormalizedRevision)
	pruned, err = store.PruneNormalized(ctx, retention.ObjectCandidate{ID: revisionID, Key: normalizedKey}, normalizedCutoff, fixture.now, objects)
	if err == nil || pruned {
		t.Fatalf("normalized ambiguous delete = %t, %v", pruned, err)
	}
	var normalizedReferencePruned bool
	if err := fixture.pool.QueryRow(ctx, `select normalized_text_object_key is null and normalized_text_pruned_at is not null from app.content_revisions where id = $1::uuid`, revisionID).Scan(&normalizedReferencePruned); err != nil || !normalizedReferencePruned {
		t.Fatalf("normalized reference pruned = %t, %v", normalizedReferencePruned, err)
	}
	if len(objects.deleted) != 2 || objects.deleted[0] != rawKey || objects.deleted[1] != normalizedKey {
		t.Fatalf("ambiguous deletes attempted = %v", objects.deleted)
	}
}

func TestNormalizedRetentionSerializesParserCommitAfterDeletion(t *testing.T) {
	fixture := newRetentionFixture(t)
	ctx := context.Background()
	retentionStore, err := retentionstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	parserStore, err := parsingstore.New(fixture.pool)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parsing.New().Parse(ctx, parsing.Request{
		Connector:   sources.ConnectorStructuredAPI,
		URL:         "https://example.test/status",
		ContentType: "application/json",
		Body:        strings.NewReader(`{"status":"operational","services":[{"id":"api","name":"API","status":"operational"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := storage.NormalizedObjectKey(fixture.sourceID, parsed.NormalizedSHA256)
	if err != nil {
		t.Fatal(err)
	}
	oldRawID, _ := fixture.raw(t, "retention-lock-old", fixture.old)
	fixture.revision(t, oldRawID, key, fixture.old)
	newRawID, _ := fixture.raw(t, "retention-lock-new", fixture.now)
	cutoff := fixture.now.Add(-retention.DefaultPolicy().NormalizedRevision)
	candidates, err := retentionStore.NormalizedCandidates(ctx, cutoff, 10)
	if err != nil || len(candidates) != 1 || candidates[0].Key != key {
		t.Fatalf("normalized candidate = %+v, %v", candidates, err)
	}
	objects := &blockingRetentionObjects{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-objects.release:
		default:
			close(objects.release)
		}
	}()
	type pruneResult struct {
		pruned bool
		err    error
	}
	pruned := make(chan pruneResult, 1)
	go func() {
		result, pruneError := retentionStore.PruneNormalized(ctx, candidates[0], cutoff, fixture.now, objects)
		pruned <- pruneResult{pruned: result, err: pruneError}
	}()
	select {
	case <-objects.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("retention did not reach object deletion")
	}
	committed := make(chan struct{}, 1)
	parserDone := make(chan error, 1)
	go func() {
		_, recordError := parserStore.RecordSuccessWithCommit(ctx, parsing.RecordRequest{
			RawDocumentID: newRawID,
			ObjectKey:     key,
			Result:        parsed,
			AttemptedAt:   fixture.now,
			CompletedAt:   fixture.now.Add(time.Second),
		}, func(context.Context) error {
			committed <- struct{}{}
			return nil
		})
		parserDone <- recordError
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := fixture.pool.QueryRow(ctx, `
			select exists (
				select 1 from pg_stat_activity
				where pid <> pg_backend_pid() and datname = current_database()
					and wait_event_type = 'Lock' and wait_event = 'advisory'
					and query like '%relantern:normalized:%'
			)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("parser did not wait for retention's normalized key lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-committed:
		t.Fatal("parser committed its object while retention was deleting the same key")
	default:
	}
	close(objects.release)
	select {
	case result := <-pruned:
		if result.err != nil || !result.pruned {
			t.Fatalf("retention prune = %t, %v", result.pruned, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retention did not finish deletion")
	}
	select {
	case err := <-parserDone:
		if err != nil {
			t.Fatalf("parser record after deletion: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parser did not resume after deletion")
	}
	select {
	case <-committed:
	default:
		t.Fatal("parser did not commit its object after retention released the lock")
	}
	var newReferenceCount int
	if err := fixture.pool.QueryRow(ctx, `
		select count(*) from app.content_revisions
		where raw_document_id = $1::uuid and normalized_text_object_key = $2`, newRawID, key).Scan(&newReferenceCount); err != nil || newReferenceCount != 1 {
		t.Fatalf("new normalized object reference = %d, %v", newReferenceCount, err)
	}
}
