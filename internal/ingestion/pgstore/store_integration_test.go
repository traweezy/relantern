package pgstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

type entryObjects struct {
	mu        sync.Mutex
	staged    map[string][]byte
	committed map[string][]byte
	stages    int
}

func newEntryObjects() *entryObjects {
	return &entryObjects{staged: make(map[string][]byte), committed: make(map[string][]byte)}
}

func (objects *entryObjects) Stage(_ context.Context, contentType string, reader io.Reader) (storage.StagedObject, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return storage.StagedObject{}, err
	}
	staged := storage.StagedObject{TemporaryKey: uuid.NewString(), SHA256: sha256.Sum256(body), Bytes: int64(len(body)), ContentType: contentType}
	objects.mu.Lock()
	objects.staged[staged.TemporaryKey] = body
	objects.stages++
	objects.mu.Unlock()
	return staged, nil
}

func (objects *entryObjects) Commit(_ context.Context, staged storage.StagedObject, key string) error {
	objects.mu.Lock()
	defer objects.mu.Unlock()
	body, ok := objects.staged[staged.TemporaryKey]
	if !ok {
		return errors.New("staged source entry is missing")
	}
	objects.committed[key] = body
	delete(objects.staged, staged.TemporaryKey)
	return nil
}

func (objects *entryObjects) Abort(_ context.Context, staged storage.StagedObject) error {
	objects.mu.Lock()
	delete(objects.staged, staged.TemporaryKey)
	objects.mu.Unlock()
	return nil
}

func (objects *entryObjects) Delete(_ context.Context, key string) error {
	objects.mu.Lock()
	delete(objects.committed, key)
	objects.mu.Unlock()
	return nil
}

func TestRepositoryAdvisoryEndpointUsesEventContentPolicy(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for source endpoint integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	sourceID := "test-advisory-policy-" + uuid.NewString()
	defer func() { _, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID) }()
	_, err = pool.Exec(ctx, `insert into app.sources (id, name, trust_tier, owner, origin,
		validation_state, homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Test advisory policy', 'T1', 'system', 'owner', 'active',
		'https://github.com/example/project', 'metadata-only', true, array['go'], now())`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct {
		registryID string
		connector  string
		url        string
		config     string
		wantPolicy string
	}{
		{sourceID + "-releases", "github_releases", "https://api.github.com/repos/example/project/releases", `{"event":"releases","contentPolicy":"metadata-only"}`, "metadata-only"},
		{sourceID + "-advisories", "github_advisories", "https://api.github.com/repos/example/project/security-advisories", `{"event":"security_advisories","contentPolicy":"link-and-excerpt"}`, "link-and-excerpt"},
		{sourceID + "-unreviewed", "github_advisories", "https://api.github.com/repos/example/project/security-advisories?state=published", `{"event":"security_advisories"}`, "metadata-only"},
	} {
		_, err = pool.Exec(ctx, `insert into app.source_endpoints (registry_id, source_id,
			connector, url, poll_interval, priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite, config)
			values ($1, $2, $3, $4, interval '5 minutes', 'critical', 'api',
			array['application/json'], 1048576, 'github-advisories-v1', $5::jsonb)`,
			endpoint.registryID, sourceID, endpoint.connector, endpoint.url, endpoint.config)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := (&Store{pool: pool}).LoadEndpoint(ctx, endpoint.registryID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded == nil || loaded.ContentPolicy != endpoint.wantPolicy {
			t.Fatalf("LoadEndpoint(%s) = %+v, want policy %q", endpoint.registryID, loaded, endpoint.wantPolicy)
		}
	}
}

func TestSourcePollingLedgerAndPause(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for source polling integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const queue = "test_source_ingestion"
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatal(err)
	}
	objects := newEntryObjects()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	store, err := NewWithEntries(pool, jobs, objects, clock.NewFixed(now))
	if err != nil {
		t.Fatal(err)
	}
	sourceID := "test-ingestion-" + uuid.NewString()
	registryID := sourceID + "-rss"
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job where queue = $1 and args ->> 'registryId' = $2`, queue, registryID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
	})
	_, err = pool.Exec(ctx, `
		insert into app.sources (id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Test source', 'T0', 'system', 'owner', 'active',
			'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		insert into app.source_endpoints (registry_id, source_id, connector, url,
			poll_interval, priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite)
		values ($1, $2, 'rss', 'https://example.com/feed.xml',
			interval '5 minutes', 'critical', 'feed', array['application/rss+xml'],
			1048576, 'rss-v1')`, registryID, sourceID)
	if err != nil {
		t.Fatal(err)
	}

	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 1 {
		t.Fatalf("ScheduleDue(first) = %d, %v", count, err)
	}
	var next time.Time
	if err := pool.QueryRow(ctx, `select next_poll_at from app.source_endpoints where registry_id = $1`, registryID).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next.Before(now.Add(5*time.Minute)) || next.After(now.Add(5*time.Minute+30*time.Second)) {
		t.Fatalf("next poll = %s", next)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 0 {
		t.Fatalf("ScheduleDue(second) = %d, %v", count, err)
	}
	_, err = pool.Exec(ctx, `insert into app.source_runtime_overrides (source_id, polling_enabled, reason)
		values ($1, false, 'integration pause')`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint, err := store.LoadEndpoint(ctx, registryID); err != nil || endpoint != nil {
		t.Fatalf("LoadEndpoint(paused) = %+v, %v", endpoint, err)
	}
	_, err = pool.Exec(ctx, `update app.source_runtime_overrides set polling_enabled = true where source_id = $1`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := store.LoadEndpoint(ctx, registryID)
	if err != nil || endpoint == nil {
		t.Fatalf("LoadEndpoint(active) = %+v, %v", endpoint, err)
	}

	modified := "Sat, 19 Sep 2026 11:00:00 GMT"
	if err := store.RecordFetch(ctx, *endpoint, fetcher.Result{
		Outcome:    fetcher.OutcomeNotModified,
		Checkpoint: fetcher.Checkpoint{ETag: `"revision-1"`, LastModified: modified},
		Attempts:   []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 304}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	endpoint, err = store.LoadEndpoint(ctx, registryID)
	if err != nil || endpoint.Checkpoint.ETag != `"revision-1"` {
		t.Fatalf("conditional checkpoint = %+v, %v", endpoint, err)
	}
	retryAfter := now.Add(20 * time.Minute)
	if err := store.RecordFetch(ctx, *endpoint, fetcher.Result{
		Outcome: fetcher.OutcomeFailed,
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now,
			StatusCode: 429, ErrorCode: fetcher.ErrorUnexpectedStatus, RetryAfter: retryAfter}},
	}, &fetcher.FetchError{Code: fetcher.ErrorUnexpectedStatus, Retryable: true, Err: errors.New("rate limited")}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select next_poll_at from app.source_endpoints where registry_id = $1`, registryID).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next.Before(retryAfter) {
		t.Fatalf("rate limit next poll = %s, want >= %s", next, retryAfter)
	}

	digest := sha256.Sum256([]byte("<rss>recorded fixture</rss>"))
	objectKey := fmt.Sprintf("raw/%s/2026/09/19/%x.xml", sourceID, digest)
	if err := store.RecordFetch(ctx, *endpoint, fetcher.Result{
		Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: objectKey, Bytes: 27,
		Checkpoint: fetcher.Checkpoint{ETag: `"revision-2"`},
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now,
			StatusCode: 200, FinalURL: "https://example.com/feed.xml", ContentType: "application/rss+xml", Bytes: 27}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	var rawID string
	if err := pool.QueryRow(ctx, `select id::text from app.raw_documents where source_id = $1`, sourceID).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	var parseJobs int
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2 and args ->> 'rawDocumentId' = $3`,
		queue, jobqueue.ParseRawDocumentKind, rawID).Scan(&parseJobs); err != nil {
		t.Fatal(err)
	}
	if parseJobs != 1 {
		t.Fatalf("parse jobs = %d, want 1", parseJobs)
	}
	var pendingCode string
	if err := pool.QueryRow(ctx, `select ingestion_error_code from app.raw_documents where id = $1::uuid`, rawID).Scan(&pendingCode); err != nil {
		t.Fatal(err)
	}
	if pendingCode != "pending_entries" {
		t.Fatalf("stored collection replay marker = %q", pendingCode)
	}
	// Simulate River exhausting its first job while the fetch checkpoint stays
	// advanced. Reconciliation must create a new parse job from the raw marker.
	_, err = pool.Exec(ctx, `delete from river.river_job where queue = $1 and kind = $2
		and args ->> 'rawDocumentId' = $3`, queue, jobqueue.ParseRawDocumentKind, rawID)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 1 {
		t.Fatalf("pending collection did not replay after lost job: %d, %v", count, err)
	}
	loaded, err := store.LoadRawDocument(ctx, registryID, rawID)
	if err != nil || loaded.ObjectKey != objectKey || loaded.ContentType != "application/rss+xml" {
		t.Fatalf("LoadRawDocument() = %+v, %v", loaded, err)
	}

	feed := []byte(`<rss version="2.0"><channel><title>Fixture</title><link>https://example.com/feed.xml</link><description>Fixture</description><item><guid>one</guid><title>First</title><link>https://example.com/story</link><description>Same story</description></item><item><guid>two</guid><title>Second</title><link>https://example.com/story</link><description>Same story</description></item></channel></rss>`)
	entries, err := parsing.SplitEntries(ctx, sources.ConnectorRSS, loaded.URL, feed)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	results := make(chan int, 2)
	errorsFromWorkers := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			count, recordErr := store.RecordEntries(ctx, loaded, registryID, entries)
			results <- count
			errorsFromWorkers <- recordErr
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFromWorkers)
	for recordErr := range errorsFromWorkers {
		if recordErr != nil {
			t.Fatal(recordErr)
		}
	}
	created := 0
	for count := range results {
		created += count
	}
	if created != 2 {
		t.Fatalf("concurrent RecordEntries created %d documents, want 2", created)
	}
	var pendingAfter int
	if err := pool.QueryRow(ctx, `select count(*) from app.raw_documents
		where id = $1::uuid and ingestion_error_code is not null`, rawID).Scan(&pendingAfter); err != nil {
		t.Fatal(err)
	}
	if pendingAfter != 0 {
		t.Fatal("completed collection retained its replay marker")
	}
	if count, err := store.RecordEntries(ctx, loaded, registryID, entries); err != nil || count != 0 {
		t.Fatalf("idempotent RecordEntries = %d, %v", count, err)
	}
	var keys []string
	rows, err := pool.Query(ctx, `select object_key from app.raw_documents where parent_raw_document_id = $1::uuid order by object_key`, rawID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("child object keys = %+v", keys)
	}
	var childID string
	if err := pool.QueryRow(ctx, `select id::text from app.raw_documents
		where parent_raw_document_id = $1::uuid order by id limit 1`, rawID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	child, err := store.LoadRawDocument(ctx, registryID, childID)
	if err != nil || child.ParentRawID != rawID || child.SourceEntryID == "" ||
		child.Connector != sources.ConnectorSourceEntry || child.ContentType != "application/json" {
		t.Fatalf("LoadRawDocument(child) = %+v, %v", child, err)
	}
	var childPending string
	if err := pool.QueryRow(ctx, `select ingestion_error_code from app.raw_documents where id = $1::uuid`, childID).Scan(&childPending); err != nil {
		t.Fatal(err)
	}
	if childPending != "pending_parse" {
		t.Fatalf("new child replay marker = %q", childPending)
	}
	_, err = pool.Exec(ctx, `delete from river.river_job where queue = $1 and kind = $2
		and args ->> 'rawDocumentId' = $3`, queue, jobqueue.ParseRawDocumentKind, childID)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 1 {
		t.Fatalf("lost child parse job did not replay: %d, %v", count, err)
	}
	// Operational fetch attempts expire before retained raw evidence does.
	_, err = pool.Exec(ctx, `delete from app.source_fetches where object_key = $1`, objectKey)
	if err != nil {
		t.Fatal(err)
	}
	if afterRetention, err := store.LoadRawDocument(ctx, registryID, rawID); err != nil || afterRetention.ObjectKey != objectKey {
		t.Fatalf("parent lookup after fetch retention = %+v, %v", afterRetention, err)
	}
	if afterRetention, err := store.LoadRawDocument(ctx, registryID, childID); err != nil || afterRetention.ObjectKey != child.ObjectKey {
		t.Fatalf("child lookup after fetch retention = %+v, %v", afterRetention, err)
	}
	objects.mu.Lock()
	if objects.stages != 2 || len(objects.committed) != 2 || len(objects.staged) != 0 {
		t.Fatalf("entry objects: stages=%d committed=%d staged=%d", objects.stages, len(objects.committed), len(objects.staged))
	}
	objects.mu.Unlock()
	later, err := NewWithEntries(pool, jobs, objects, clock.NewFixed(now.Add(48*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	changedFeed := []byte(strings.Replace(string(feed), "<title>First</title>", "<title>First, corrected</title>", 1))
	changedEntries, err := parsing.SplitEntries(ctx, sources.ConnectorRSS, loaded.URL, changedFeed)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := later.RecordEntries(ctx, loaded, registryID, changedEntries[:1]); err != nil || count != 1 {
		t.Fatalf("changed source entry revision = %d, %v", count, err)
	}
	var changedKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where source_entry_id = (select id from app.source_entries where source_id = $1 and external_id = $2)
		order by first_seen_at desc limit 1`, sourceID, changedEntries[0].ExternalID).Scan(&changedKey); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changedKey, "/2026/09/19/") {
		t.Fatalf("changed revision key %q did not use durable first-seen date", changedKey)
	}
	if count, err := later.RecordEntries(ctx, loaded, registryID, changedEntries[:1]); err != nil || count != 0 {
		t.Fatalf("replayed changed source entry = %d, %v", count, err)
	}
	// Simulate successful child parse and dedupe before testing endpoint recovery.
	childRows, err := pool.Query(ctx, `select id::text from app.raw_documents where parent_raw_document_id = $1::uuid`, rawID)
	if err != nil {
		t.Fatal(err)
	}
	var childIDs []string
	for childRows.Next() {
		var id string
		if err := childRows.Scan(&id); err != nil {
			childRows.Close()
			t.Fatal(err)
		}
		childIDs = append(childIDs, id)
	}
	if err := childRows.Err(); err != nil {
		childRows.Close()
		t.Fatal(err)
	}
	childRows.Close()
	for _, id := range childIDs {
		document, err := store.LoadRawDocument(ctx, registryID, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.ClearIngestionFailure(ctx, document, registryID); err != nil {
			t.Fatal(err)
		}
	}
	badParent := loaded
	badParent.ID = uuid.NewString()
	failedEntry := []byte(strings.Replace(string(feed), "<guid>one</guid>", "<guid>uncommitted</guid>", 1))
	failedEntries, err := parsing.SplitEntries(ctx, sources.ConnectorRSS, loaded.URL, failedEntry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEntries(ctx, badParent, registryID, failedEntries[:1]); err == nil {
		t.Fatal("invalid parent raw document unexpectedly committed a child")
	}
	objects.mu.Lock()
	if objects.stages != 4 || len(objects.committed) != 3 || len(objects.staged) != 0 {
		t.Fatalf("failed child left an object: stages=%d committed=%d staged=%d", objects.stages, len(objects.committed), len(objects.staged))
	}
	objects.mu.Unlock()
	_, err = pool.Exec(ctx, `delete from river.river_job where queue = $1 and kind = $2
		and args ->> 'rawDocumentId' = $3`, queue, jobqueue.ParseRawDocumentKind, rawID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordIngestionFailure(ctx, loaded, registryID, parsing.ErrorInvalidDocument); err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 0 {
		t.Fatalf("failed endpoint replayed before resume: %d, %v", count, err)
	}
	_, err = pool.Exec(ctx, `update app.source_endpoints
		set health_state = 'healthy', next_poll_at = $2 where registry_id = $1`, registryID, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 1 {
		t.Fatalf("resumed endpoint did not requeue failed split: %d, %v", count, err)
	}
	if count, err := store.RecordEntries(ctx, loaded, registryID, entries); err != nil || count != 0 {
		t.Fatalf("replayed source entries = %d, %v", count, err)
	}
	var unresolved int
	if err := pool.QueryRow(ctx, `select count(*) from app.raw_documents
		where id = $1::uuid and ingestion_error_code is not null`, rawID).Scan(&unresolved); err != nil {
		t.Fatal(err)
	}
	if unresolved != 0 {
		t.Fatal("successful replay did not clear durable split failure")
	}
	if err := store.RecordIngestionFailure(ctx, loaded, registryID, parsing.ErrorCode("source_entry_record_failed")); err != nil {
		t.Fatal(err)
	}
	if count, err := store.RecordEntries(ctx, loaded, registryID, entries); err != nil || count != 0 {
		t.Fatalf("successful retry after transient entry failure = %d, %v", count, err)
	}
	var recoveredHealth string
	var recoveredNext *time.Time
	if err := pool.QueryRow(ctx, `select health_state, next_poll_at from app.source_endpoints
		where registry_id = $1`, registryID).Scan(&recoveredHealth, &recoveredNext); err != nil {
		t.Fatal(err)
	}
	if recoveredHealth != "unverified" || recoveredNext != nil {
		t.Fatalf("transient retry did not re-arm polling: state=%q next=%v", recoveredHealth, recoveredNext)
	}
	if err := store.RecordIngestionFailure(ctx, loaded, registryID, parsing.ErrorCode("source_entry_record_failed")); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `update app.raw_documents
		set ingestion_error_code = 'pending_parse', ingestion_failed_at = $2
		where id = $1::uuid`, childID, now)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.RecordEntries(ctx, loaded, registryID, entries); err != nil || count != 0 {
		t.Fatalf("parent retry with pending child = %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `select health_state from app.source_endpoints where registry_id = $1`, registryID).Scan(&recoveredHealth); err != nil {
		t.Fatal(err)
	}
	if recoveredHealth != "unverified" {
		t.Fatalf("pending child blocked healthy parent recovery: %q", recoveredHealth)
	}
	if err := store.ClearIngestionFailure(ctx, child, registryID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordIngestionFailure(ctx, child, registryID, parsing.ErrorInvalidDocument); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordIngestionFailure(ctx, loaded, registryID, parsing.ErrorCode("source_entry_record_failed")); err != nil {
		t.Fatal(err)
	}
	if count, err := store.RecordEntries(ctx, loaded, registryID, entries); err != nil || count != 0 {
		t.Fatalf("parent retry with unresolved child failure = %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `select health_state from app.source_endpoints where registry_id = $1`, registryID).Scan(&recoveredHealth); err != nil {
		t.Fatal(err)
	}
	if recoveredHealth != "failed" {
		t.Fatalf("unresolved child failure was overridden: %q", recoveredHealth)
	}
	// Retention deletes by raw-document key; another entry's evidence survives.
	if err := objects.Delete(ctx, keys[0]); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	_, retained := objects.committed[keys[1]]
	objects.mu.Unlock()
	if !retained {
		t.Fatal("pruning one entry removed another entry's evidence")
	}
}

func TestRefetchReusesEvidenceAndRestoresPrunedEntries(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for source polling integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const queue = "test_source_refetch"
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatal(err)
	}
	objects := newEntryObjects()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	restoredAt := now.Add(200 * 24 * time.Hour)
	store, err := NewWithEntries(pool, jobs, objects, clock.NewFixed(now))
	if err != nil {
		t.Fatal(err)
	}
	sourceID := "test-refetch-" + uuid.NewString()
	registryID := sourceID + "-rss"
	url := "https://example.com/feed.xml"
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job where queue = $1 and args ->> 'registryId' = $2`, queue, registryID)
		_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID)
	})
	_, err = pool.Exec(ctx, `insert into app.sources (id, name, trust_tier, owner, origin,
		validation_state, homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Refetch source', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `insert into app.source_endpoints (registry_id, source_id, connector, url,
		poll_interval, priority, robots_policy, expected_content_types,
		max_response_bytes, fixture_suite, next_poll_at)
		values ($1, $2, 'rss', $3, interval '5 minutes', 'critical', 'feed',
		array['application/rss+xml'], 1048576, 'rss-v1', $4)`, registryID, sourceID, url, restoredAt.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := store.LoadEndpoint(ctx, registryID)
	if err != nil || endpoint == nil {
		t.Fatalf("LoadEndpoint() = %+v, %v", endpoint, err)
	}
	feed := []byte(`<rss version="2.0"><channel><title>Fixture</title><link>https://example.com/feed.xml</link><description>Fixture</description><item><guid>one</guid><title>First</title><link>https://example.com/story</link><description>Same story</description></item></channel></rss>`)
	digest := sha256.Sum256(feed)
	record := func(at time.Time) string {
		t.Helper()
		key, err := storage.RawFetchObjectKey(sourceID, registryID, url, at, digest, "application/rss+xml")
		if err != nil {
			t.Fatal(err)
		}
		objects.mu.Lock()
		objects.committed[key] = feed
		objects.mu.Unlock()
		if err := store.RecordFetch(ctx, *endpoint, fetcher.Result{
			Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: key, Bytes: int64(len(feed)),
			Attempts: []fetcher.Attempt{{AttemptedAt: at, CompletedAt: at, StatusCode: 200,
				FinalURL: url, ContentType: "application/rss+xml", Bytes: int64(len(feed))}},
		}, nil); err != nil {
			t.Fatal(err)
		}
		return key
	}
	firstKey := record(now)
	var parentID string
	if err := pool.QueryRow(ctx, `select id::text from app.raw_documents where source_id = $1`, sourceID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	redirectRegistryID := sourceID + "-redirect"
	_, err = pool.Exec(ctx, `insert into app.source_endpoints (registry_id, source_id, connector, url,
		poll_interval, priority, robots_policy, expected_content_types,
		max_response_bytes, fixture_suite, next_poll_at)
		values ($1, $2, 'rss', 'https://example.com/redirect.xml', interval '5 minutes',
		'normal', 'feed', array['application/rss+xml'], 1048576, 'rss-v1', $3)`,
		redirectRegistryID, sourceID, restoredAt.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	redirectEndpoint, err := store.LoadEndpoint(ctx, redirectRegistryID)
	if err != nil || redirectEndpoint == nil {
		t.Fatalf("LoadEndpoint(redirect) = %+v, %v", redirectEndpoint, err)
	}
	redirectKey, err := storage.RawFetchObjectKey(sourceID, redirectRegistryID, url, now, digest, "application/rss+xml")
	if err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	objects.committed[redirectKey] = feed
	objects.mu.Unlock()
	if err := store.RecordFetch(ctx, *redirectEndpoint, fetcher.Result{
		Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: redirectKey, Bytes: int64(len(feed)),
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 200,
			FinalURL: url, ContentType: "application/rss+xml", Bytes: int64(len(feed))}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	var redirectFetchKey string
	var wrongRegistryJobs int
	if err := pool.QueryRow(ctx, `select object_key from app.source_fetches
		where endpoint_id = (select id from app.source_endpoints where registry_id = $1)
		order by id desc limit 1`, redirectRegistryID).Scan(&redirectFetchKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2 and args ->> 'rawDocumentId' = $3
			and args ->> 'registryId' = $4`, queue, jobqueue.ParseRawDocumentKind,
		parentID, redirectRegistryID).Scan(&wrongRegistryJobs); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	_, redirectObjectExists := objects.committed[redirectKey]
	objects.mu.Unlock()
	if redirectFetchKey != firstKey || redirectObjectExists || wrongRegistryJobs != 0 {
		t.Fatalf("converged redirect: ledger=%q duplicate=%t wrong parse jobs=%d", redirectFetchKey, redirectObjectExists, wrongRegistryJobs)
	}
	duplicateKey := record(now.Add(24 * time.Hour))
	var storedKey, latestFetchKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents where id = $1::uuid`, parentID).Scan(&storedKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select object_key from app.source_fetches
		where endpoint_id = (select id from app.source_endpoints where registry_id = $1)
		order by id desc limit 1`, registryID).Scan(&latestFetchKey); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	_, duplicateExists := objects.committed[duplicateKey]
	objects.mu.Unlock()
	if storedKey != firstKey || latestFetchKey != firstKey || duplicateExists {
		t.Fatalf("repeat fetch retained raw=%q ledger=%q duplicate=%t, want %q", storedKey, latestFetchKey, duplicateExists, firstKey)
	}
	_, err = pool.Exec(ctx, `update app.raw_documents set object_key = null, raw_pruned_at = $2 where id = $1::uuid`, parentID, now.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Delete(ctx, firstKey); err != nil {
		t.Fatal(err)
	}
	restoredKey := record(restoredAt)
	var restoredID string
	var prunedAt *time.Time
	var firstSeenAt time.Time
	var parentPending string
	if err := pool.QueryRow(ctx, `select id::text, object_key, raw_pruned_at,
		first_seen_at, ingestion_error_code from app.raw_documents
		where source_id = $1 and canonical_url = $2 and raw_sha256 = $3`, sourceID, url, digest[:]).Scan(&restoredID, &storedKey, &prunedAt, &firstSeenAt, &parentPending); err != nil {
		t.Fatal(err)
	}
	if restoredID != parentID || storedKey != restoredKey || prunedAt != nil ||
		!firstSeenAt.Before(restoredAt.Add(-180*24*time.Hour)) || parentPending != "pending_entries" {
		t.Fatalf("old pruned parent restoration: id=%q key=%q pruned=%v first=%s pending=%q", restoredID, storedKey, prunedAt, firstSeenAt, parentPending)
	}
	parent, err := store.LoadRawDocument(ctx, registryID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parsing.SplitEntries(ctx, sources.ConnectorRSS, parent.URL, feed)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.RecordEntries(ctx, parent, registryID, entries); err != nil || count != 1 {
		t.Fatalf("RecordEntries() = %d, %v", count, err)
	}
	var childID, childKey string
	if err := pool.QueryRow(ctx, `select id::text, object_key from app.raw_documents
		where parent_raw_document_id = $1::uuid`, parentID).Scan(&childID, &childKey); err != nil {
		t.Fatal(err)
	}
	child, err := store.LoadRawDocument(ctx, registryID, childID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearIngestionFailure(ctx, child, registryID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `update app.raw_documents set object_key = null, raw_pruned_at = $2
		where id = $1::uuid`, childID, now.Add(96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Delete(ctx, childKey); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `delete from river.river_job where queue = $1 and kind = $2
		and args ->> 'rawDocumentId' = $3`, queue, jobqueue.ParseRawDocumentKind, childID)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.RecordEntries(ctx, parent, registryID, entries); err != nil || count != 0 {
		t.Fatalf("restore child = %d, %v", count, err)
	}
	var childPending string
	if err := pool.QueryRow(ctx, `select object_key, raw_pruned_at, ingestion_error_code
		from app.raw_documents where id = $1::uuid`, childID).Scan(&storedKey, &prunedAt, &childPending); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	_, childExists := objects.committed[childKey]
	objects.mu.Unlock()
	if storedKey != childKey || prunedAt != nil || childPending != "pending_parse" || !childExists {
		t.Fatalf("pruned child restoration: key=%q pruned=%v pending=%q object=%t", storedKey, prunedAt, childPending, childExists)
	}
	_, err = pool.Exec(ctx, `delete from river.river_job where queue = $1 and kind = $2
		and args ->> 'rawDocumentId' = $3`, queue, jobqueue.ParseRawDocumentKind, childID)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.ScheduleDue(ctx, now, 10); err != nil || count != 1 {
		t.Fatalf("replay restored child after lost job = %d, %v", count, err)
	}
}
