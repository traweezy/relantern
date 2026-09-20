package pgstore

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
)

func TestAdvisoryCollectionObservationSchema(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for observation schema integration")
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	sourceID := "test-advisory-observation-" + uuid.NewString()
	registryID := sourceID
	url := "https://api.github.com/advisories?test=" + uuid.NewString()
	key := "raw/test/" + uuid.NewString()
	digest := sha256.Sum256([]byte("[{\"ghsa_id\":\"GHSA-aaaa-bbbb-cccc\"}]"))
	observedAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Advisory observation fixture', 'T1', 'system', 'system',
		'active', 'https://github.com/advisories', 'link-and-excerpt', true,
		array['security'], $2)`, sourceID, observedAt); err != nil {
		t.Fatal(err)
	}
	var endpointID, parentRawID string
	if err := tx.QueryRow(ctx, `insert into app.source_endpoints (
		registry_id, source_id, connector, url, poll_interval, priority,
		robots_policy, expected_content_types, max_response_bytes, fixture_suite
	) values ($1, $1, 'github_advisories', $2, interval '5 minutes',
		'critical', 'api', array['application/json'], 10485760,
		'github-global-advisories-v1') returning id::text`, registryID, url).Scan(&endpointID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, source_registry_id, source_connector,
		source_content_type
	) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt', $1,
		'github_advisories', 'application/json') returning id::text`,
		sourceID, url, key, digest[:], observedAt).Scan(&parentRawID); err != nil {
		t.Fatal(err)
	}

	var fetchIDs, observationIDs [2]int64
	for index := range fetchIDs {
		completedAt := observedAt.Add(time.Duration(index) * time.Second)
		if err := tx.QueryRow(ctx, `insert into app.source_fetches (
			endpoint_id, attempted_at, completed_at, outcome, status_code,
			final_url, content_type, bytes, duration_ms, raw_sha256, object_key
		) values ($1::uuid, $2, $2, 'stored', 200, $3, 'application/json',
			33, 1, $4, $5) returning id`, endpointID, completedAt, url,
			digest[:], key).Scan(&fetchIDs[index]); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at
		) values ($1, $2, $3, $4::uuid, $5) returning id`, sourceID,
			registryID, fetchIDs[index], parentRawID,
			completedAt).Scan(&observationIDs[index]); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := tx.Query(ctx, `select source_fetch_id, state, entry_count,
		split_completed_at, processed_at
		from app.advisory_collection_observations
		where source_id = $1 and state = 'pending' order by id`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	var pending []int64
	for rows.Next() {
		var fetchID int64
		var state string
		var entryCount *int32
		var splitCompletedAt, processedAt *time.Time
		if err := rows.Scan(&fetchID, &state, &entryCount,
			&splitCompletedAt, &processedAt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if state != "pending" || entryCount != nil ||
			splitCompletedAt != nil || processedAt != nil {
			rows.Close()
			t.Fatalf("unexpected pending observation state: %s %v %v %v",
				state, entryCount, splitCompletedAt, processedAt)
		}
		pending = append(pending, fetchID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(pending) != 2 || pending[0] != fetchIDs[0] || pending[1] != fetchIDs[1] {
		t.Fatalf("pending fetch order = %v, want %v", pending, fetchIDs)
	}
	var sourceEntryID, childRawID string
	if err := tx.QueryRow(ctx, `insert into app.source_entries (
		source_id, external_id, first_seen_at, last_seen_at
	) values ($1, 'GHSA-aaaa-bbbb-cccc', $2, $2) returning id::text`,
		sourceID, observedAt).Scan(&sourceEntryID); err != nil {
		t.Fatal(err)
	}
	childDigest := sha256.Sum256([]byte("{\"ghsa_id\":\"GHSA-aaaa-bbbb-cccc\"}"))
	if err := tx.QueryRow(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, parent_raw_document_id,
		source_entry_id, source_registry_id, source_connector,
		source_content_type
	) values ($1, 'https://github.com/advisories/GHSA-aaaa-bbbb-cccc', $2,
		$3, $4, $4, 'link-and-excerpt', $5::uuid, $6::uuid, $1,
		'source_entry', 'application/json') returning id::text`,
		sourceID, "raw/test/"+uuid.NewString(), childDigest[:], observedAt,
		parentRawID, sourceEntryID).Scan(&childRawID); err != nil {
		t.Fatal(err)
	}
	for ordinal := range 2 {
		if _, err := tx.Exec(ctx, `insert into app.advisory_entry_observations (
			collection_observation_id, entry_ordinal, source_entry_id,
			child_raw_document_id
		) values ($1, $2, $3::uuid, $4::uuid)`, observationIDs[0],
			ordinal, sourceEntryID, childRawID); err != nil {
			t.Fatalf("record repeated advisory encounter %d: %v", ordinal, err)
		}
	}
	var eventCount int
	if err := tx.QueryRow(ctx, `select count(*) from app.advisory_entry_observations
		where collection_observation_id = $1 and source_entry_id = $2::uuid`,
		observationIDs[0], sourceEntryID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("repeated entry observations = %d, want 2", eventCount)
	}

	// Invalid transitions and duplicate capture must be rejected without
	// aborting the outer fixture transaction.
	for _, invalid := range []struct {
		query string
		args  []any
	}{
		{`insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at
		) values ($1, $2, $3, $4::uuid, $5)`,
			[]any{sourceID, registryID, fetchIDs[0], parentRawID, observedAt}},
		{`update app.advisory_collection_observations set state = 'processed'
			where source_fetch_id = $1`, []any{fetchIDs[0]}},
		{`update app.advisory_collection_observations set entry_count = 0
			where source_fetch_id = $1`, []any{fetchIDs[0]}},
		{`update app.advisory_collection_observations
			set entry_count = 501, split_completed_at = $2
			where source_fetch_id = $1`, []any{fetchIDs[0], observedAt}},
		{`insert into app.advisory_entry_observations (
			collection_observation_id, entry_ordinal, source_entry_id,
			child_raw_document_id
		) values ($1, 1, $2::uuid, $3::uuid)`,
			[]any{observationIDs[0], sourceEntryID, childRawID}},
		{`update app.advisory_entry_observations set state = 'processed'
			where collection_observation_id = $1`, []any{observationIDs[0]}},
	} {
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, rejected := savepoint.Exec(ctx, invalid.query, invalid.args...)
		if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		if rejected == nil {
			t.Fatalf("observation constraint accepted invalid statement: %s", invalid.query)
		}
	}
	splitCompletedAt := observedAt.Add(time.Second)
	if _, err := tx.Exec(ctx, `update app.advisory_collection_observations
		set entry_count = 2, split_completed_at = $2
		where source_fetch_id = $1`, fetchIDs[0], splitCompletedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update app.advisory_entry_observations
		set state = 'processed', processed_at = $2
		where collection_observation_id = $1`, observationIDs[0],
		splitCompletedAt); err != nil {
		t.Fatal(err)
	}
	processedAt := observedAt.Add(2 * time.Second)
	if _, err := tx.Exec(ctx, `update app.advisory_collection_observations
		set state = 'processed', processed_at = $2
		where source_fetch_id = $1`, fetchIDs[0], processedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `delete from app.source_fetches where id = $1`, fetchIDs[0]); err != nil {
		t.Fatalf("90-day fetch retention must not delete observation: %v", err)
	}
	var state string
	if err := tx.QueryRow(ctx, `select state from app.advisory_collection_observations
		where source_fetch_id = $1`, fetchIDs[0]).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "processed" {
		t.Fatalf("retained observation state = %q, want processed", state)
	}
	if _, err := tx.Exec(ctx, `update app.advisory_collection_observations
		set entry_count = 0, split_completed_at = $2
		where id = $1`, observationIDs[1], processedAt); err != nil {
		t.Fatalf("record valid empty collection split: %v", err)
	}
	var pendingCount int
	if err := tx.QueryRow(ctx, `select count(*) from app.advisory_collection_observations
		where source_id = $1 and state = 'pending'`, sourceID).Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending observations = %d, want 1", pendingCount)
	}
	var episodeIndexValid bool
	if err := tx.QueryRow(ctx, `select state.indisunique and state.indisvalid
		from pg_index state
		join pg_class index_name on index_name.oid = state.indexrelid
		where index_name.relname = 'critical_alerts_user_advisory_package_episode_key'`).Scan(
		&episodeIndexValid); err != nil {
		t.Fatal(err)
	}
	if !episodeIndexValid {
		t.Fatal("episode unique index is not valid")
	}
}
