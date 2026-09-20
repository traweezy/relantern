package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strconv"
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
	"github.com/traweezy/relantern/internal/parsing"
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
			where queue = $1 and kind = $2
				and (args ->> 'eventId')::bigint in (
					select event.id from app.advisory_entry_observations event
					join app.advisory_collection_observations observation
						on observation.id = event.collection_observation_id
					join app.raw_documents raw
						on raw.id = observation.parent_raw_document_id
					where observation.source_id = $3 and raw.canonical_url = $4
				)`, queue, jobqueue.AssessAdvisoryObservationKind,
			sourceID, pageURL); err != nil {
			t.Errorf("delete advisory observation assessment jobs: %v", err)
		}
		if _, err := pool.Exec(ctx, `delete from river.river_job
			where queue = $1 and kind = $2
				and (args ->> 'observationId')::bigint in (
					select observation.id
					from app.advisory_collection_observations observation
					join app.raw_documents raw
						on raw.id = observation.parent_raw_document_id
					where observation.source_id = $3 and raw.canonical_url = $4
				)`, queue, jobqueue.SplitAdvisoryObservationKind,
			sourceID, pageURL); err != nil {
			t.Errorf("delete advisory split jobs: %v", err)
		}
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
	uniqueAdvisoryID := strings.ReplaceAll(uuid.NewString(), "-", "")
	body = strings.ReplaceAll(body, "GHSA-abcd-1234-efgh",
		"GHSA-"+uniqueAdvisoryID[:4]+"-"+uniqueAdvisoryID[4:8]+"-"+uniqueAdvisoryID[8:12])

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
	if parseJobs != 0 {
		t.Fatalf("managed continuation page parse jobs = %d, want 0", parseJobs)
	}

	// An identical later body must still be a new observation. A 304 is only
	// a conditional checkpoint, so it cannot claim another observation.
	secondAt := now.Add(time.Minute)
	secondStaged, err := objects.Stage(ctx, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := storage.RawFetchObjectKey(sourceID, registryID, pageURL,
		secondAt, secondStaged.SHA256, contentType)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Commit(ctx, secondStaged, secondKey); err != nil {
		t.Fatal(err)
	}
	secondResult := result
	secondResult.ObjectKey = secondKey
	secondResult.Attempts = []fetcher.Attempt{{AttemptedAt: secondAt,
		CompletedAt: secondAt, StatusCode: 200, FinalURL: pageURL,
		ContentType: contentType, Bytes: secondStaged.Bytes}}
	if err := store.RecordFetch(ctx, pageEndpoint, secondResult, nil); err != nil {
		t.Fatalf("RecordFetch(repeated bytes): %v", err)
	}
	thirdAt := secondAt.Add(time.Minute)
	if err := store.RecordFetch(ctx, pageEndpoint, fetcher.Result{
		Outcome: fetcher.OutcomeNotModified,
		Checkpoint: fetcher.Checkpoint{Cursor: nextURL, ETag: rootETag,
			ProviderState: map[string]any{"advisoryScanPages": 2}},
		Attempts: []fetcher.Attempt{{AttemptedAt: thirdAt,
			CompletedAt: thirdAt, StatusCode: 304, FinalURL: pageURL}},
	}, nil); err != nil {
		t.Fatalf("RecordFetch(not modified): %v", err)
	}
	type observation struct {
		id        int64
		fetchID   int64
		parentID  string
		observed  time.Time
		outcome   string
		fetchTime time.Time
	}
	rows, err := pool.Query(ctx, `
		select observation.id, observation.source_fetch_id,
			observation.parent_raw_document_id::text, observation.observed_at,
			source_fetch.outcome, source_fetch.completed_at
		from app.advisory_collection_observations observation
		join app.source_fetches source_fetch
			on source_fetch.id = observation.source_fetch_id
		where observation.source_id = $1 and observation.source_registry_id = $2
			and observation.parent_raw_document_id = $3::uuid
		order by observation.id`, sourceID, registryID, rawID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	observations := make([]observation, 0, 2)
	for rows.Next() {
		var selected observation
		if err := rows.Scan(&selected.id, &selected.fetchID, &selected.parentID,
			&selected.observed, &selected.outcome, &selected.fetchTime); err != nil {
			t.Fatal(err)
		}
		observations = append(observations, selected)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0].id >= observations[1].id ||
		observations[0].fetchID >= observations[1].fetchID ||
		observations[0].parentID != rawID || observations[1].parentID != rawID ||
		observations[0].outcome != "stored" || observations[1].outcome != "stored" ||
		!observations[0].observed.Equal(now) ||
		!observations[1].observed.Equal(secondAt) ||
		!observations[0].fetchTime.Equal(now) ||
		!observations[1].fetchTime.Equal(secondAt) {
		t.Fatalf("repeated stored collection observations = %+v, want two ordered fetches for one parent", observations)
	}
	var rawCount, notModifiedCount int
	if err := pool.QueryRow(ctx, `select count(*) from app.raw_documents
		where source_id = $1 and canonical_url = $2 and raw_sha256 = $3`,
		sourceID, pageURL, staged.SHA256[:]).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from app.source_fetches
		where endpoint_id = $1::uuid and final_url = $2 and outcome = 'not_modified'
			and completed_at = $3`, endpointID, pageURL, thirdAt).Scan(&notModifiedCount); err != nil {
		t.Fatal(err)
	}
	if rawCount != 1 || notModifiedCount != 1 {
		t.Fatalf("repeated collection raw documents = %d, conditional fetches = %d", rawCount, notModifiedCount)
	}
	entries, err := parsing.SplitEntries(ctx, sources.ConnectorGitHubAdvisories,
		pageURL, []byte(body))
	if err != nil || len(entries) != 1 {
		t.Fatalf("SplitEntries() = %d, %v", len(entries), err)
	}
	firstObservation, err := store.LoadAdvisoryObservation(ctx, observations[0].id)
	if err != nil || firstObservation == nil {
		t.Fatalf("LoadAdvisoryObservation(first) = %v, %v", firstObservation, err)
	}
	if count, err := store.RecordAdvisoryObservationEntries(ctx, *firstObservation, entries); err != nil || count != 1 {
		t.Fatalf("RecordAdvisoryObservationEntries(first) = %d, %v", count, err)
	}
	var firstEventID, secondEventID int64
	var childRawID string
	var firstSplitTime time.Time
	if err := pool.QueryRow(ctx, `select event.id, event.child_raw_document_id::text,
		observation.split_completed_at
		from app.advisory_entry_observations event
		join app.advisory_collection_observations observation
			on observation.id = event.collection_observation_id
		where observation.id = $1`, observations[0].id).Scan(
		&firstEventID, &childRawID, &firstSplitTime); err != nil {
		t.Fatal(err)
	}
	if firstSplitTime.IsZero() {
		t.Fatal("first split completion time is missing")
	}
	revisionDigest := sha256.Sum256([]byte("advisory observation reused child revision"))
	var revisionID string
	if err := pool.QueryRow(ctx, `insert into app.content_revisions (
		raw_document_id, normalized_sha256, normalized_text_object_key,
		parser_name, parser_version, language, normalized_bytes,
		outline, offset_map, warnings, change_kind, change_reason,
		material_change, observed_at
	) values ($1::uuid, $2, $3, 'advisory-observation-fixture', '1',
		'en', 1, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
		'initial', 'fixture parsed child', false, $4)
	returning id::text`, childRawID, revisionDigest[:],
		"normalized/test/"+uuid.NewString(), firstSplitTime).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, revisionID); err != nil {
			t.Errorf("delete synthetic advisory revision: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null
		where id = $1::uuid`, childRawID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update app.advisory_entry_observations
		set state = 'processed', processed_at = $2 where id = $1`,
		firstEventID, firstSplitTime); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update app.advisory_collection_observations
		set state = 'processed', processed_at = $2 where id = $1`,
		observations[0].id, firstSplitTime); err != nil {
		t.Fatal(err)
	}
	secondObservation, err := store.LoadAdvisoryObservation(ctx, observations[1].id)
	if err != nil || secondObservation == nil {
		t.Fatalf("LoadAdvisoryObservation(second) = %v, %v", secondObservation, err)
	}
	if count, err := store.RecordAdvisoryObservationEntries(ctx, *secondObservation, entries); err != nil || count != 0 {
		t.Fatalf("RecordAdvisoryObservationEntries(reused child) = %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `select id from app.advisory_entry_observations
		where collection_observation_id = $1`, observations[1].id).Scan(&secondEventID); err != nil {
		t.Fatal(err)
	}
	var eventCount, distinctChildCount, completedSplits int
	if err := pool.QueryRow(ctx, `select count(*), count(distinct child_raw_document_id)
		from app.advisory_entry_observations
		where collection_observation_id in ($1, $2)`,
		observations[0].id, observations[1].id).Scan(&eventCount, &distinctChildCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)
		from app.advisory_collection_observations
		where id in ($1, $2) and entry_count = 1 and split_completed_at is not null`,
		observations[0].id, observations[1].id).Scan(&completedSplits); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || distinctChildCount != 1 || completedSplits != 2 {
		t.Fatalf("observation events=%d distinct children=%d completed splits=%d, want 2, 1, 2",
			eventCount, distinctChildCount, completedSplits)
	}
	nextEvent, err := store.NextPendingAdvisoryEntryEvent(ctx, sourceID)
	if err != nil || nextEvent == nil ||
		nextEvent.CollectionObservationID != observations[1].id ||
		nextEvent.ID != secondEventID || nextEvent.RevisionID != revisionID {
		t.Fatalf("NextPendingAdvisoryEntryEvent() = %+v, %v, want reused second observation",
			nextEvent, err)
	}
	var assessJobs int
	if err := pool.QueryRow(ctx, `select count(*) from river.river_job
		where queue = $1 and kind = $2
			and args ->> 'eventId' = $3 and args ->> 'revisionId' = $4`,
		queue, jobqueue.AssessAdvisoryObservationKind,
		strconv.FormatInt(secondEventID, 10), revisionID).Scan(&assessJobs); err != nil {
		t.Fatal(err)
	}
	if assessJobs != 1 {
		t.Fatalf("reused child assessment jobs = %d, want 1", assessJobs)
	}
	if _, err := store.RecordEntries(ctx, loaded, registryID, entries); err == nil {
		t.Fatal("legacy RecordEntries accepted a managed advisory parent")
	}

	emptyAt := thirdAt.Add(time.Minute)
	emptyStaged, err := objects.Stage(ctx, contentType, strings.NewReader("[]"))
	if err != nil {
		t.Fatal(err)
	}
	emptyKey, err := storage.RawFetchObjectKey(sourceID, registryID, pageURL,
		emptyAt, emptyStaged.SHA256, contentType)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Commit(ctx, emptyStaged, emptyKey); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFetch(ctx, pageEndpoint, fetcher.Result{
		Outcome: fetcher.OutcomeStored, ObjectKey: emptyKey,
		SHA256: emptyStaged.SHA256, Bytes: emptyStaged.Bytes,
		Attempts: []fetcher.Attempt{{AttemptedAt: emptyAt, CompletedAt: emptyAt,
			StatusCode: 200, FinalURL: pageURL, ContentType: contentType,
			Bytes: emptyStaged.Bytes}},
	}, nil); err != nil {
		t.Fatalf("RecordFetch(empty collection): %v", err)
	}
	var emptyObservationID int64
	if err := pool.QueryRow(ctx, `select observation.id
		from app.advisory_collection_observations observation
		join app.source_fetches source_fetch
			on source_fetch.id = observation.source_fetch_id
		where observation.source_id = $1 and source_fetch.completed_at = $2`,
		sourceID, emptyAt).Scan(&emptyObservationID); err != nil {
		t.Fatal(err)
	}
	emptyObservation, err := store.LoadAdvisoryObservation(ctx, emptyObservationID)
	if err != nil || emptyObservation == nil {
		t.Fatalf("LoadAdvisoryObservation(empty) = %v, %v", emptyObservation, err)
	}
	if count, err := store.RecordAdvisoryObservationEntries(ctx, *emptyObservation,
		[]parsing.Entry{}); err != nil || count != 0 {
		t.Fatalf("RecordAdvisoryObservationEntries(empty) = %d, %v", count, err)
	}
	var emptyState string
	var emptyCount int
	if err := pool.QueryRow(ctx, `select state, entry_count
		from app.advisory_collection_observations where id = $1`,
		emptyObservationID).Scan(&emptyState, &emptyCount); err != nil {
		t.Fatal(err)
	}
	if emptyState != "processed" || emptyCount != 0 {
		t.Fatalf("empty advisory observation state=%q count=%d, want processed/0",
			emptyState, emptyCount)
	}
	if _, err := store.ReconcileAdvisoryObservations(ctx, 10); err != nil {
		t.Fatalf("ReconcileAdvisoryObservations() error = %v", err)
	}
}
