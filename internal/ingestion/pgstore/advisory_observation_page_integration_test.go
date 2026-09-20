package pgstore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

func TestHundredAdvisoryEntryPageKeepsRepeatedPositions(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("isolated database required")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sourceID := "test-observation-scale-" + uuid.NewString()
	queue := "test_observation_scale_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `delete from river.river_job where queue = $1`, queue); err != nil {
			t.Errorf("clean test jobs: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `delete from app.sources where id = $1`, sourceID); err != nil {
			t.Errorf("clean test source: %v", err)
		}
	}()

	items := make([]string, 100)
	for index := range items {
		advisoryID := fmt.Sprintf("GHSA-abcd-1234-%04d", index)
		items[index] = fmt.Sprintf(`{"ghsa_id":%q,"html_url":%q,"summary":"Reviewed widget advisory","type":"reviewed","severity":"critical","published_at":"2024-01-01T00:00:00Z","github_reviewed_at":"2024-01-01T01:00:00Z"}`,
			advisoryID, "https://github.com/advisories/"+advisoryID)
	}
	items[len(items)-1] = items[0]
	pageURL := sources.GlobalReviewedAdvisoriesURL
	body := []byte("[" + strings.Join(items, ",") + "]")
	parseStart := time.Now()
	entries, err := parsing.SplitAdvisoryObservationEntries(ctx, pageURL, body)
	if err != nil || len(entries) != 100 {
		t.Fatalf("parse 100 advisories = %d, %v", len(entries), err)
	}
	t.Logf("parse 100 entries: %s", time.Since(parseStart))

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256(body)
	key := "raw/test/" + uuid.NewString()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Scale fixture', 'T1', 'system', 'system', 'active',
		'https://github.com/advisories', 'link-and-excerpt', true,
		array['security'], $2)`, sourceID, now); err != nil {
		t.Fatal(err)
	}
	var endpointID, parentID string
	if err := tx.QueryRow(ctx, `insert into app.source_endpoints (
		registry_id, source_id, connector, url, poll_interval, priority,
		robots_policy, expected_content_types, max_response_bytes, fixture_suite
	) values ($1, $1, 'github_advisories', $2, interval '5 minutes',
		'critical', 'api', array['application/vnd.github+json'], 10485760,
		'github-global-advisories-v1') returning id::text`, sourceID, pageURL).Scan(&endpointID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, source_registry_id, source_connector,
		source_content_type, ingestion_error_code, ingestion_failed_at
	) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $1,
		'github_advisories', 'application/vnd.github+json', 'pending_entries', $5)
		returning id::text`, sourceID, pageURL, key, digest[:], now).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := jobqueue.NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewWithEntries(pool, jobs, newEntryObjects(), clock.NewFixed(now))
	if err != nil {
		t.Fatal(err)
	}
	var firstObservationID int64
	for repeat := range 2 {
		completedAt := now.Add(time.Duration(repeat) * time.Minute)
		var observationID int64
		if err := pool.QueryRow(ctx, `with recorded_fetch as (
			insert into app.source_fetches (
				endpoint_id, attempted_at, completed_at, outcome, status_code,
				final_url, content_type, bytes, duration_ms, raw_sha256, object_key
			) values ($1::uuid, $2, $2, 'stored', 200, $3,
				'application/vnd.github+json', $4, 1, $5, $6) returning id
		) insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at
		) select $7, $7, recorded_fetch.id, $8::uuid, $2 from recorded_fetch returning id`,
			endpointID, completedAt, pageURL, len(body), digest[:], key,
			sourceID, parentID).Scan(&observationID); err != nil {
			t.Fatal(err)
		}
		if repeat == 0 {
			firstObservationID = observationID
		}
		observation, err := store.LoadAdvisoryObservation(ctx, observationID)
		if err != nil || observation == nil {
			t.Fatalf("load observation %d: %+v, %v", repeat, observation, err)
		}
		started := time.Now()
		created, err := store.RecordAdvisoryObservationEntries(ctx, *observation, entries)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("split observation %d after %s: %v", repeat, elapsed, err)
		}
		wantCreated := 99
		if repeat == 1 {
			wantCreated = 0
		}
		if created != wantCreated {
			t.Fatalf("split %d created %d children; want %d", repeat, created, wantCreated)
		}
		t.Logf("split 100 entries (repeat=%t, newChildren=%d): %s", repeat == 1, created, elapsed)
	}
	var events, children int
	if err := pool.QueryRow(ctx, `select count(*), count(distinct child_raw_document_id)
		from app.advisory_entry_observations event
		join app.advisory_collection_observations observation
			on observation.id = event.collection_observation_id
		where observation.source_id = $1`, sourceID).Scan(&events, &children); err != nil {
		t.Fatal(err)
	}
	if events != 200 || children != 99 {
		t.Fatalf("event count=%d distinct children=%d; want 200 and 99", events, children)
	}
	var firstChildID, lastChildID string
	if err := pool.QueryRow(ctx, `select first.child_raw_document_id::text,
		last.child_raw_document_id::text
		from app.advisory_entry_observations first
		join app.advisory_entry_observations last
			on last.collection_observation_id = first.collection_observation_id
		where first.collection_observation_id = $1
			and first.entry_ordinal = 0 and last.entry_ordinal = 99`,
		firstObservationID).Scan(&firstChildID, &lastChildID); err != nil {
		t.Fatal(err)
	}
	if firstChildID != lastChildID {
		t.Fatalf("repeated page positions use different child evidence: %s != %s", firstChildID, lastChildID)
	}
}
