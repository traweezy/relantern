package pgstore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestCatchUpReplaysLatestProcessedObservationForLateWatch(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	eventID := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)},
		clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	cleanupObservedBackfillJobs(t, fixture)
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err != nil {
		t.Fatalf("process observation before watch edit: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)

	var pendingCollectionID, fetchID int64
	if err := pool.QueryRow(ctx, `
		select nextval(pg_get_serial_sequence('app.source_fetches', 'id'))`).Scan(&fetchID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at
		) values ($1, $2, $3, $4::uuid, $5)
		returning id`, fixture.sourceID, fixture.registryID, fetchID,
		fixture.parentID, fixture.now.Add(2*time.Minute)).Scan(&pendingCollectionID); err != nil {
		t.Fatal(err)
	}
	args := jobqueue.ReassessCurrentAdvisoriesArgs{UserID: fixture.userID, SettingsVersion: 1}
	if err := store.CatchUp(ctx, args); !errors.Is(err, ErrAdvisoryObservationBackfillPending) {
		t.Fatalf("catch-up across pending source = %v, want snooze", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); !errors.Is(err, ErrAdvisoryObservationBackfillPending) {
		t.Fatalf("assessment across pending source = %v, want snooze", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `
		update app.advisory_collection_observations
		set entry_count = 0, split_completed_at = observed_at,
			state = 'processed', processed_at = observed_at
		where id = $1`, pendingCollectionID)
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatalf("catch up after empty page completes: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 1)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("replay current observation for late watch: %v", err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("repeat late-watch replay: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	var episode int
	if err := pool.QueryRow(ctx, `
		select opening_observation_id, episode_number
		from app.critical_alerts where user_id = $1::uuid`, fixture.userID).
		Scan(&openingID, &episode); err != nil {
		t.Fatal(err)
	}
	if openingID != eventID || episode != 1 {
		t.Fatalf("late-watch alert evidence = event %d, episode %d; want %d, 1",
			openingID, episode, eventID)
	}
	var eventState string
	if err := pool.QueryRow(ctx, `select state from app.advisory_entry_observations
		where id = $1`, eventID).Scan(&eventState); err != nil || eventState != "processed" {
		t.Fatalf("replayed event state = %q, %v", eventState, err)
	}
}

func TestCatchUpReplaysProcessedObservationAfterParentRetention(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	eventID := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)},
		clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	cleanupObservedBackfillJobs(t, fixture)
	var parentKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where id = $1::uuid`, fixture.parentID).Scan(&parentKey); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `update app.raw_documents
		set object_key = null, raw_pruned_at = $2
		where id = $1::uuid`, fixture.parentID, fixture.now.Add(time.Minute))
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err == nil {
		t.Fatal("pending observation accepted a pruned parent")
	}
	fixture.exec(t, ctx, `update app.raw_documents
		set object_key = $2, raw_pruned_at = null
		where id = $1::uuid`, fixture.parentID, parentKey)
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err != nil {
		t.Fatalf("process observation before retention: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.raw_documents
		set object_key = null, raw_pruned_at = $2
		where id = $1::uuid`, fixture.parentID, fixture.now.Add(2*time.Minute))
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	if err := store.CatchUp(ctx, jobqueue.ReassessCurrentAdvisoriesArgs{
		UserID: fixture.userID, SettingsVersion: 1,
	}); err != nil {
		t.Fatalf("catch up after parent retention: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 1)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("replay current observation after parent retention: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	if err := pool.QueryRow(ctx, `select opening_observation_id
		from app.critical_alerts where user_id = $1::uuid`, fixture.userID).
		Scan(&openingID); err != nil {
		t.Fatal(err)
	}
	if openingID != eventID {
		t.Fatalf("late-watch alert evidence = event %d, want %d", openingID, eventID)
	}
}

func TestObservedBackfillReportsChildPrunedAfterCatchUp(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	eventID := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)},
		clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	cleanupObservedBackfillJobs(t, fixture)
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err != nil {
		t.Fatalf("process observation before watch edit: %v", err)
	}
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	if err := store.CatchUp(ctx, jobqueue.ReassessCurrentAdvisoriesArgs{
		UserID: fixture.userID, SettingsVersion: 1,
	}); err != nil {
		t.Fatalf("enqueue late-watch assessment: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 1)
	var childKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where id = $1::uuid`, fixture.childID).Scan(&childKey); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `update app.raw_documents
		set object_key = null, raw_pruned_at = $2
		where id = $1::uuid`, fixture.childID, fixture.now.Add(2*time.Minute))
	var pendingCollectionID, fetchID int64
	if err := pool.QueryRow(ctx, `select nextval(pg_get_serial_sequence(
		'app.source_fetches', 'id'))`).Scan(&fetchID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into app.advisory_collection_observations
		(source_id, source_registry_id, source_fetch_id, parent_raw_document_id, observed_at)
		values ($1, $2, $3, $4::uuid, $5) returning id`, fixture.sourceID,
		fixture.registryID, fetchID, fixture.parentID,
		fixture.now.Add(3*time.Minute)).Scan(&pendingCollectionID); err != nil {
		t.Fatal(err)
	}
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); !errors.Is(err, ErrAdvisoryObservationBackfillPending) {
		t.Fatalf("missing child with pending source = %v, want snooze", err)
	}
	fixture.exec(t, ctx, `update app.advisory_collection_observations
		set entry_count = 0, split_completed_at = observed_at,
			state = 'processed', processed_at = observed_at
		where id = $1`, pendingCollectionID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err == nil {
		t.Fatal("pruned current child silently completed its late-watch backfill")
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.sources set enabled = false where id = $1`, fixture.sourceID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("disabled source should skip stale backfill: %v", err)
	}
	fixture.exec(t, ctx, `update app.sources set enabled = true where id = $1`, fixture.sourceID)
	fixture.exec(t, ctx, `update app.raw_documents
		set object_key = $2, raw_pruned_at = null
		where id = $1::uuid`, fixture.childID, childKey)
	fixture.exec(t, ctx, `update app.raw_documents
		set ingestion_error_code = 'invalid_evidence', ingestion_failed_at = $2
		where id = $1::uuid`, fixture.childID, fixture.now.Add(3*time.Minute))
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err == nil {
		t.Fatal("invalid current child silently completed its late-watch backfill")
	}
	fixture.exec(t, ctx, `update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null
		where id = $1::uuid`, fixture.childID)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("retry backfill after child restoration: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
}

func TestCatchUpReusesOlderRawOnlyWhenLatestObservationReopensIt(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	correctionChildID, correctionRevisionID, correctionKey, correctionPayload :=
		seedCorrectionRevision(t, ctx, fixture, "high", "", false)
	var firstKey, correctionParentID string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where id = $1::uuid`, fixture.childID).Scan(&firstKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select parent_raw_document_id::text
		from app.raw_documents where id = $1::uuid`, correctionChildID).
		Scan(&correctionParentID); err != nil {
		t.Fatal(err)
	}
	firstEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	secondEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		correctionParentID, correctionChildID, fixture.now.Add(2*time.Minute))
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	reader := &fixtureReader{byObjectKey: map[string][]byte{
		firstKey: bytes.Clone(fixture.payload), correctionKey: bytes.Clone(correctionPayload),
	}}
	store, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	cleanupObservedBackfillJobs(t, fixture, correctionChildID)
	for _, event := range []struct {
		id       int64
		revision string
	}{{firstEvent, fixture.revisionID}, {secondEvent, correctionRevisionID}} {
		if err := store.AssessObservation(ctx, event.id, event.revision); err != nil {
			t.Fatalf("process observation %d: %v", event.id, err)
		}
	}
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	args := jobqueue.ReassessCurrentAdvisoriesArgs{UserID: fixture.userID, SettingsVersion: 1}
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatal(err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("stale first event replay: %v", err)
	}
	if err := store.Assess(ctx, correctionChildID, correctionRevisionID); err != nil {
		t.Fatalf("latest correction replay: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	thirdEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now.Add(4*time.Minute))
	if err := store.AssessObservation(ctx, thirdEvent, fixture.revisionID); err != nil {
		t.Fatalf("process byte-identical reopening: %v", err)
	}
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	if err := store.CatchUp(ctx, args); err != nil {
		t.Fatalf("catch up latest reused raw: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 1)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("replay latest reused raw: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	if err := pool.QueryRow(ctx, `select opening_observation_id from app.critical_alerts
		where user_id = $1::uuid`, fixture.userID).Scan(&openingID); err != nil {
		t.Fatal(err)
	}
	if openingID != thirdEvent {
		t.Fatalf("reopened raw attributed to observation %d, want %d",
			openingID, thirdEvent)
	}
}

func TestCatchUpLeavesPendingLatestEventToOrderedAssessment(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	firstEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{payload: bytes.Clone(fixture.payload)},
		clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	cleanupObservedBackfillJobs(t, fixture)
	if err := store.AssessObservation(ctx, firstEvent, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	latestEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now.Add(2*time.Minute))
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	if err := store.CatchUp(ctx, jobqueue.ReassessCurrentAdvisoriesArgs{
		UserID: fixture.userID, SettingsVersion: 1,
	}); err != nil {
		t.Fatalf("catch up with latest event pending: %v", err)
	}
	assertCatchUpJobCount(t, ctx, fixture.childID, pool, 0)
	if err := store.AssessObservation(ctx, latestEvent, fixture.revisionID); err != nil {
		t.Fatalf("ordered assessment after watch activation: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	if err := pool.QueryRow(ctx, `select opening_observation_id from app.critical_alerts
		where user_id = $1::uuid`, fixture.userID).Scan(&openingID); err != nil {
		t.Fatal(err)
	}
	if openingID != latestEvent {
		t.Fatalf("pending event admitted late watch via %d, want %d",
			openingID, latestEvent)
	}
}

func cleanupObservedBackfillJobs(t *testing.T, fixture assessmentFixture, otherRawIDs ...string) {
	t.Helper()
	rawIDs := append([]string{fixture.childID}, otherRawIDs...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fixture.pool.Exec(ctx, `
			delete from river.river_job
			where queue = 'test_alert_assessment'
				and ((kind = 'assess_critical_advisory' and args->>'rawDocumentId' = any($1::text[]))
					or (kind = 'reassess_current_advisories' and args->>'userId' = $2)
					or (kind = 'assess_advisory_observation' and
						(args->>'eventId')::bigint in (
							select event.id from app.advisory_entry_observations event
							join app.advisory_collection_observations collection
								on collection.id = event.collection_observation_id
							where collection.source_id = $3)))`,
			rawIDs, fixture.userID, fixture.sourceID); err != nil {
			t.Errorf("clean up observed catch-up jobs: %v", err)
		}
	})
}
