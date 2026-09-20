package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

func TestReviewedGlobalAdvisoryContinuationRecordsRawAndCursor(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for advisory continuation integration")
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
	t.Cleanup(pool.Close)

	const registryID = "github-global-advisories"
	const queue = "test_global_advisory_page"
	const contentType = "application/vnd.github+json"
	const rootETag = `"root-advisories"`
	const sourceID = registryID
	pageURL := "https://api.github.com/advisories?after=" + uuid.NewString() +
		"&direction=desc&per_page=100&sort=updated&type=reviewed"
	nextURL := "https://api.github.com/advisories?after=" + uuid.NewString() +
		"&direction=desc&per_page=100&sort=updated&type=reviewed"
	if !sources.IsGlobalAdvisoryPageURL(pageURL) || !sources.IsGlobalAdvisoryPageURL(nextURL) {
		t.Fatal("test continuation URLs violate the reviewed page policy")
	}

	// Keep the reviewed identity available when a package test runs against a
	// migrations-only database. A seeded endpoint is left untouched after cleanup.
	insertedSource, err := pool.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Reviewed global advisory fixture', 'T1', 'system', 'system',
		'paused', 'https://github.com/advisories', 'link-and-excerpt', true,
		array['security'], now()) on conflict (id) do nothing`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if insertedSource.RowsAffected() > 0 {
			if _, err := pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID); err != nil {
				t.Errorf("delete advisory fixture source: %v", err)
			}
		}
	})
	insertedEndpoint, err := pool.Exec(ctx, `insert into app.source_endpoints (
		registry_id, source_id, connector, url, poll_interval, priority,
		robots_policy, expected_content_types, max_response_bytes, fixture_suite,
		health_state
	) values ($1, $1, 'github_advisories', $2, interval '5 minutes',
		'critical', 'api', array['application/vnd.github+json', 'application/json'],
		10485760, 'github-global-advisories-v1', 'paused')
		on conflict (registry_id) do nothing`, registryID, sources.GlobalReviewedAdvisoriesURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if insertedEndpoint.RowsAffected() > 0 {
			if _, err := pool.Exec(ctx, `delete from app.source_endpoints
				where registry_id = $1`, registryID); err != nil {
				t.Errorf("delete advisory fixture endpoint: %v", err)
			}
		}
	})

	var endpointID string
	var actualSourceID, connector, pinnedURL, policy, tier, fixtureSuite string
	var originalHealth string
	var originalAttempt, originalSuccess, originalNextPoll *time.Time
	var originalUpdated time.Time
	var expectedTypes []string
	var maximumBytes int64
	if err := pool.QueryRow(ctx, `select endpoint.id::text, endpoint.source_id,
		endpoint.connector, endpoint.url, source.content_policy, source.trust_tier,
		endpoint.fixture_suite, endpoint.expected_content_types, endpoint.max_response_bytes,
		endpoint.health_state, endpoint.last_attempt_at, endpoint.last_success_at,
		endpoint.next_poll_at, endpoint.updated_at
		from app.source_endpoints endpoint
		join app.sources source on source.id = endpoint.source_id
		where endpoint.registry_id = $1`, registryID).Scan(
		&endpointID, &actualSourceID, &connector, &pinnedURL, &policy, &tier,
		&fixtureSuite, &expectedTypes, &maximumBytes, &originalHealth,
		&originalAttempt, &originalSuccess, &originalNextPoll, &originalUpdated,
	); err != nil {
		t.Fatal(err)
	}
	if actualSourceID != sourceID || connector != string(sources.ConnectorGitHubAdvisories) ||
		pinnedURL != sources.GlobalReviewedAdvisoriesURL || policy != "link-and-excerpt" ||
		tier != "T1" || fixtureSuite != "github-global-advisories-v1" {
		t.Fatalf("reviewed endpoint changed: source=%q connector=%q url=%q policy=%q tier=%q fixture=%q",
			actualSourceID, connector, pinnedURL, policy, tier, fixtureSuite)
	}

	var originalCursor, originalETag, originalModified *string
	var originalProviderState []byte
	var originalCheckpointUpdated time.Time
	checkpointExisted := true
	err = pool.QueryRow(ctx, `select cursor, etag, last_modified, provider_state, updated_at
		from app.source_checkpoints where endpoint_id = $1::uuid`, endpointID).Scan(
		&originalCursor, &originalETag, &originalModified, &originalProviderState,
		&originalCheckpointUpdated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		checkpointExisted = false
	} else if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `delete from river.river_job
			where queue = $1 and args ->> 'registryId' = $2`, queue, registryID); err != nil {
			t.Errorf("delete advisory parse job: %v", err)
		}
		if _, err := pool.Exec(ctx, `delete from app.raw_documents
			where source_id = $1 and canonical_url = $2`, sourceID, pageURL); err != nil {
			t.Errorf("delete advisory raw document: %v", err)
		}
		if _, err := pool.Exec(ctx, `delete from app.source_fetches
			where endpoint_id = $1::uuid and final_url = $2`, endpointID, pageURL); err != nil {
			t.Errorf("delete advisory fetch record: %v", err)
		}
		if checkpointExisted {
			if _, err := pool.Exec(ctx, `update app.source_checkpoints
				set cursor = $2, etag = $3, last_modified = $4,
					provider_state = $5, updated_at = $6
				where endpoint_id = $1::uuid`, endpointID, originalCursor,
				originalETag, originalModified, originalProviderState,
				originalCheckpointUpdated); err != nil {
				t.Errorf("restore advisory checkpoint: %v", err)
			}
		} else if _, err := pool.Exec(ctx, `delete from app.source_checkpoints
			where endpoint_id = $1::uuid`, endpointID); err != nil {
			t.Errorf("delete advisory checkpoint: %v", err)
		}
		if insertedEndpoint.RowsAffected() == 0 {
			if _, err := pool.Exec(ctx, `update app.source_endpoints
				set health_state = $2, last_attempt_at = $3, last_success_at = $4,
					next_poll_at = $5, updated_at = $6 where id = $1::uuid`, endpointID,
				originalHealth, originalAttempt, originalSuccess, originalNextPoll,
				originalUpdated); err != nil {
				t.Errorf("restore advisory endpoint: %v", err)
			}
		}
	})

	catalog, err := sources.LoadFixtureCatalog("../../../sources/fixtures.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, suite := range catalog.Suites {
		if suite.ID != fixtureSuite {
			continue
		}
		if suite.ContentType != contentType || suite.Payloads["normal"].Body == nil {
			t.Fatalf("reviewed fixture %s has no JSON normal body", fixtureSuite)
		}
		body = *suite.Payloads["normal"].Body
		break
	}
	if body == "" {
		t.Fatalf("reviewed fixture %s is missing", fixtureSuite)
	}

	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatal(err)
	}
	objects := newEntryObjects()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	store, err := NewWithEntries(pool, jobs, objects, clock.NewFixed(now))
	if err != nil {
		t.Fatal(err)
	}
	staged, err := objects.Stage(ctx, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	objectKey, err := storage.RawFetchObjectKey(sourceID, registryID, pageURL, now,
		staged.SHA256, contentType)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Commit(ctx, staged, objectKey); err != nil {
		t.Fatal(err)
	}
	pageEndpoint := ingestion.Endpoint{
		RegistryID: registryID, SourceID: sourceID,
		Connector: sources.ConnectorGitHubAdvisories, URL: pageURL,
		ContentPolicy: policy, ExpectedContentTypes: expectedTypes,
		MaxResponseBytes: maximumBytes,
	}
	result := fetcher.Result{
		Outcome: fetcher.OutcomeStored, ObjectKey: objectKey,
		SHA256: staged.SHA256, Bytes: staged.Bytes, NextPageURL: nextURL,
		Checkpoint: fetcher.Checkpoint{Cursor: nextURL, ETag: rootETag,
			ProviderState: map[string]any{"advisoryScanPages": 2}},
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now,
			StatusCode: 200, FinalURL: pageURL, ContentType: contentType,
			Bytes: staged.Bytes}},
	}
	if err := store.RecordFetch(ctx, pageEndpoint, result, nil); err != nil {
		t.Fatalf("RecordFetch(continuation): %v", err)
	}

	var fetchURL, fetchKey, fetchOutcome, fetchType string
	var fetchDigest []byte
	if err := pool.QueryRow(ctx, `select final_url, object_key, outcome, content_type,
		raw_sha256 from app.source_fetches
		where endpoint_id = $1::uuid and final_url = $2 and raw_sha256 = $3`,
		endpointID, pageURL, staged.SHA256[:]).Scan(&fetchURL, &fetchKey,
		&fetchOutcome, &fetchType, &fetchDigest); err != nil {
		t.Fatal(err)
	}
	if fetchURL != pageURL || fetchKey != objectKey || fetchOutcome != "stored" ||
		fetchType != contentType || string(fetchDigest) != string(staged.SHA256[:]) {
		t.Fatalf("continuation fetch = url %q key %q outcome %q type %q digest %x",
			fetchURL, fetchKey, fetchOutcome, fetchType, fetchDigest)
	}

	var rawID, rawURL, rawRegistryID, rawConnector, rawType, rawKey, rawPolicy string
	var rawDigest []byte
	if err := pool.QueryRow(ctx, `select id::text, canonical_url, source_registry_id,
		source_connector, source_content_type, object_key, content_policy, raw_sha256
		from app.raw_documents where source_id = $1 and canonical_url = $2
			and raw_sha256 = $3`, sourceID, pageURL, staged.SHA256[:]).Scan(
		&rawID, &rawURL, &rawRegistryID, &rawConnector, &rawType, &rawKey,
		&rawPolicy, &rawDigest,
	); err != nil {
		t.Fatal(err)
	}
	if rawURL != pageURL || rawRegistryID != registryID ||
		rawConnector != string(sources.ConnectorGitHubAdvisories) ||
		rawType != contentType || rawKey != objectKey ||
		rawPolicy != "link-and-excerpt" || string(rawDigest) != string(staged.SHA256[:]) {
		t.Fatalf("continuation raw provenance = url %q registry %q connector %q type %q key %q policy %q digest %x",
			rawURL, rawRegistryID, rawConnector, rawType, rawKey, rawPolicy, rawDigest)
	}

	var cursor, etag, persistedURL, health string
	var providerState []byte
	if err := pool.QueryRow(ctx, `select checkpoint.cursor, checkpoint.etag,
		checkpoint.provider_state, endpoint.url, endpoint.health_state
		from app.source_checkpoints checkpoint
		join app.source_endpoints endpoint on endpoint.id = checkpoint.endpoint_id
		where endpoint.id = $1::uuid`, endpointID).Scan(
		&cursor, &etag, &providerState, &persistedURL, &health,
	); err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(providerState, &state); err != nil {
		t.Fatal(err)
	}
	if cursor != nextURL || etag != rootETag || state["advisoryScanPages"] != float64(2) ||
		persistedURL != sources.GlobalReviewedAdvisoriesURL || health != originalHealth {
		t.Fatalf("continuation checkpoint = cursor %q etag %q state %v pinned url %q health %q",
			cursor, etag, state, persistedURL, health)
	}

	loaded, err := store.LoadRawDocument(ctx, registryID, rawID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != rawID || loaded.SourceID != sourceID ||
		loaded.Connector != sources.ConnectorGitHubAdvisories || loaded.URL != pageURL ||
		loaded.ContentType != contentType || loaded.ContentPolicy != "link-and-excerpt" ||
		loaded.ObjectKey != objectKey || loaded.RawSHA256 != sha256.Sum256([]byte(body)) {
		t.Fatalf("LoadRawDocument(continuation) = %+v", loaded)
	}
	var parseJobs int
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2 and args ->> 'registryId' = $3
			and args ->> 'rawDocumentId' = $4`,
		queue, jobqueue.ParseRawDocumentKind, registryID, rawID).Scan(&parseJobs); err != nil {
		t.Fatal(err)
	}
	if parseJobs != 1 {
		t.Fatalf("continuation page parse jobs = %d, want 1", parseJobs)
	}
}
