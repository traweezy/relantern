package pgstore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func seedAdvisoryObservation(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	fixture assessmentFixture, parentID, childID string, observedAt time.Time,
) int64 {
	collectionID := seedAdvisoryCollectionObservation(t, ctx, pool, fixture,
		parentID, observedAt)
	return seedAdvisoryEntryObservation(t, ctx, pool, fixture, collectionID, childID)
}

func seedAdvisoryCollectionObservation(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	fixture assessmentFixture, parentID string, observedAt time.Time,
) int64 {
	t.Helper()
	var fetchID int64
	if err := pool.QueryRow(ctx, `
		select nextval(pg_get_serial_sequence('app.source_fetches', 'id'))`).Scan(&fetchID); err != nil {
		t.Fatal(err)
	}
	var collectionID int64
	if err := pool.QueryRow(ctx, `
		insert into app.advisory_collection_observations (
			source_id, source_registry_id, source_fetch_id,
			parent_raw_document_id, observed_at, entry_count, split_completed_at
		) values ($1, $2, $3, $4::uuid, $5, 1, $5)
		returning id`, fixture.sourceID, fixture.registryID, fetchID,
		parentID, observedAt).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	return collectionID
}

func seedAdvisoryEntryObservation(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	fixture assessmentFixture, collectionID int64, childID string,
) int64 {
	t.Helper()
	var eventID int64
	if err := pool.QueryRow(ctx, `
		insert into app.advisory_entry_observations (
			collection_observation_id, entry_ordinal,
			source_entry_id, child_raw_document_id
		) values ($1, 0, $2::uuid, $3::uuid)
		returning id`, collectionID, fixture.entryID, childID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

func TestAdvisoryCorrectionUsesCollectionOrderAfterReverseSplit(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	correctionChildID, correctionRevisionID, correctionKey, correctionPayload :=
		seedCorrectionRevision(t, ctx, fixture, "high", "", false)
	var correctionParentID, firstKey string
	if err := pool.QueryRow(ctx, `select parent_raw_document_id::text
		from app.raw_documents where id = $1::uuid`, correctionChildID).
		Scan(&correctionParentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where id = $1::uuid`, fixture.childID).Scan(&firstKey); err != nil {
		t.Fatal(err)
	}
	firstCollection := seedAdvisoryCollectionObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.now)
	secondCollection := seedAdvisoryCollectionObservation(t, ctx, pool, fixture,
		correctionParentID, fixture.now.Add(time.Minute))
	// Independent split jobs can allocate entry IDs in the opposite order to
	// their source-serialized collection IDs.
	correctionEvent := seedAdvisoryEntryObservation(t, ctx, pool, fixture,
		secondCollection, correctionChildID)
	openingEvent := seedAdvisoryEntryObservation(t, ctx, pool, fixture,
		firstCollection, fixture.childID)
	if firstCollection >= secondCollection || openingEvent <= correctionEvent {
		t.Fatalf("fixture order: collections %d,%d; events %d,%d",
			firstCollection, secondCollection, openingEvent, correctionEvent)
	}
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `delete from river.river_job
			where queue = 'test_alert_assessment'
				and kind = 'assess_advisory_observation'
				and (args->>'eventId')::bigint = any($1::bigint[])`,
			[]int64{openingEvent, correctionEvent}); err != nil {
			t.Errorf("clean up reverse-split observation jobs: %v", err)
		}
	})
	reader := &fixtureReader{byObjectKey: map[string][]byte{
		firstKey:      bytes.Clone(fixture.payload),
		correctionKey: bytes.Clone(correctionPayload),
	}}
	store, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AssessObservation(ctx, openingEvent, fixture.revisionID); err != nil {
		t.Fatalf("assess earlier collection: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	if err := store.AssessObservation(ctx, correctionEvent, correctionRevisionID); err != nil {
		t.Fatalf("assess later correction: %v", err)
	}
	var recordedCorrectionID *int64
	var correctedAt *time.Time
	if err := pool.QueryRow(ctx, `select correction_observation_id, corrected_at
		from app.critical_alerts where user_id = $1::uuid`, fixture.userID).
		Scan(&recordedCorrectionID, &correctedAt); err != nil {
		t.Fatal(err)
	}
	if recordedCorrectionID == nil || *recordedCorrectionID != correctionEvent || correctedAt == nil {
		t.Fatalf("later correction lost: event=%v corrected_at=%v",
			recordedCorrectionID, correctedAt)
	}
	var activeDeliveries int
	if err := pool.QueryRow(ctx, `select count(*) from app.critical_alert_deliveries delivery
		join app.critical_alerts alert on alert.id = delivery.alert_id
		where alert.user_id = $1::uuid and delivery.state in ('pending', 'failed')`,
		fixture.userID).Scan(&activeDeliveries); err != nil {
		t.Fatal(err)
	}
	if activeDeliveries != 0 {
		t.Fatalf("correction left %d external deliveries active", activeDeliveries)
	}
}

func TestOrderedAdvisoryObservationReactivationAcrossCutover(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	correctionChildID, correctionRevisionID, correctionKey, correctionPayload :=
		seedCorrectionRevision(t, ctx, fixture, "high", "", false)
	reader := &fixtureReader{byObjectKey: map[string][]byte{
		correctionKey: bytes.Clone(correctionPayload),
	}}
	var firstKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents where id = $1::uuid`,
		fixture.childID).Scan(&firstKey); err != nil {
		t.Fatal(err)
	}
	reader.byObjectKey[firstKey] = bytes.Clone(fixture.payload)
	firstEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	secondParentID := ""
	if err := pool.QueryRow(ctx, `
		select parent_raw_document_id::text from app.raw_documents where id = $1::uuid`,
		correctionChildID).Scan(&secondParentID); err != nil {
		t.Fatal(err)
	}
	secondEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		secondParentID, correctionChildID, fixture.now.Add(-2*time.Minute))
	// The third fetch has exactly A's bytes and therefore reuses the first
	// child raw row and revision. Skewed fetch timestamps cannot change the
	// source-locked collection order or permit an early external send.
	thirdEvent := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now.Add(-4*time.Minute))
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_observation")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `
			delete from river.river_job
			where queue = 'test_alert_observation'
				and kind = 'assess_advisory_observation'
				and (args->>'eventId')::bigint = any($1::bigint[])`,
			[]int64{firstEvent, secondEvent, thirdEvent}); err != nil {
			t.Errorf("clean up advisory observation jobs: %v", err)
		}
	})
	first, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AssessObservation(ctx, thirdEvent, fixture.revisionID); err == nil {
		t.Fatal("later byte-identical event bypassed earlier pending observations")
	}
	if err := first.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	if err := first.AssessObservation(ctx, firstEvent, fixture.revisionID); err != nil {
		t.Fatalf("assess first observation: %v", err)
	}
	var nextJobs int
	if err := pool.QueryRow(ctx, `
		select count(*) from river.river_job
		where queue = 'test_alert_observation'
			and kind = 'assess_advisory_observation'
			and (args->>'eventId')::bigint = $1`, secondEvent).Scan(&nextJobs); err != nil {
		t.Fatal(err)
	}
	if nextJobs != 1 {
		t.Fatalf("next observation jobs = %d, want 1", nextJobs)
	}
	if err := first.AssessObservation(ctx, firstEvent, fixture.revisionID); err != nil {
		t.Fatalf("replay first observation: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	if err := pool.QueryRow(ctx, `
		select opening_observation_id from app.critical_alerts
		where user_id = $1::uuid`, fixture.userID).Scan(&openingID); err != nil {
		t.Fatal(err)
	}
	if openingID != firstEvent {
		t.Fatalf("opening observation = %d, want %d", openingID, firstEvent)
	}
	var deliveryID string
	var deliveryDue time.Time
	if err := pool.QueryRow(ctx, `
		select delivery.id::text, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts admitted on admitted.id = delivery.alert_id
		where admitted.user_id = $1::uuid
		order by delivery.channel limit 1`, fixture.userID).
		Scan(&deliveryID, &deliveryDue); err != nil {
		t.Fatal(err)
	}
	if request, err := first.BeginDelivery(ctx, deliveryID, deliveryDue); err != nil || request != nil {
		t.Fatalf("delivery with newer observation pending = %+v, %v", request, err)
	}
	second, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(3*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.AssessObservation(ctx, secondEvent, correctionRevisionID); err != nil {
		t.Fatalf("assess correction observation: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*) from river.river_job
		where queue = 'test_alert_observation'
			and kind = 'assess_advisory_observation'
			and (args->>'eventId')::bigint = $1`, thirdEvent).Scan(&nextJobs); err != nil {
		t.Fatal(err)
	}
	if nextJobs != 1 {
		t.Fatalf("reactivation observation jobs = %d, want 1", nextJobs)
	}
	var correctionID int64
	if err := pool.QueryRow(ctx, `
		select correction_observation_id from app.critical_alerts
		where user_id = $1::uuid`, fixture.userID).Scan(&correctionID); err != nil {
		t.Fatal(err)
	}
	if correctionID != secondEvent {
		t.Fatalf("correction observation = %d, want %d", correctionID, secondEvent)
	}
	third, err := New(pool, jobs, reader, clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	var legacyUniqueExists bool
	if err := pool.QueryRow(ctx, `select exists (
		select 1 from pg_constraint
		where conrelid = 'app.critical_alerts'::regclass
			and conname = 'critical_alerts_user_advisory_package_key'
	)`).Scan(&legacyUniqueExists); err != nil {
		t.Fatal(err)
	}
	if legacyUniqueExists {
		if err := third.AssessObservation(ctx, thirdEvent, fixture.revisionID); !errors.Is(err, ErrEpisodeCutoverPending) {
			t.Fatalf("reactivation before uniqueness cutover = %v", err)
		}
	} else if err := third.AssessObservation(ctx, thirdEvent, fixture.revisionID); err != nil {
		t.Fatalf("reactivation after uniqueness cutover: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `
		select state from app.advisory_entry_observations where id = $1`, thirdEvent).
		Scan(&state); err != nil {
		t.Fatal(err)
	}
	if legacyUniqueExists {
		if state != "pending" {
			t.Fatalf("blocked reactivation state = %q, want pending", state)
		}
		assertAlertCount(t, ctx, pool, fixture.userID, 1)
		return
	}
	if state != "processed" {
		t.Fatalf("reactivated observation state = %q, want processed", state)
	}
	if err := third.AssessObservation(ctx, thirdEvent, fixture.revisionID); err != nil {
		t.Fatalf("replay reactivation observation: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 2)
	rows, err := pool.Query(ctx, `
		select alert.id::text, alert.episode_number, alert.correction_reason,
			alert.opening_observation_id, delivery.idempotency_key
		from app.critical_alerts alert
		join app.critical_alert_deliveries delivery on delivery.alert_id = alert.id
		where alert.user_id = $1::uuid and delivery.channel = 'discord'
		order by alert.episode_number`, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	type episode struct {
		alertID   string
		number    int
		corrected *string
		openingID *int64
		key       string
	}
	var episodes []episode
	for rows.Next() {
		var item episode
		if err := rows.Scan(&item.alertID, &item.number, &item.corrected,
			&item.openingID, &item.key); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		episodes = append(episodes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(episodes) != 2 || episodes[0].number != 1 || episodes[1].number != 2 ||
		episodes[0].corrected == nil || episodes[1].corrected != nil ||
		episodes[1].openingID == nil || *episodes[1].openingID != thirdEvent ||
		episodes[0].alertID == episodes[1].alertID || episodes[0].key == episodes[1].key {
		t.Fatalf("reopened advisory alert episodes = %+v", episodes)
	}
}

func TestCrossSourceCriticalAfterCorrectionDoesNotBlockObservations(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	global := seedAssessment(t, ctx, pool, true)
	repository := seedAssessment(t, ctx, pool)
	repository.exec(t, ctx, `update app.watched_technologies set status = 'planned'
		where id = $1::uuid`, repository.watchID)
	correctionChildID, correctionRevisionID, correctionKey, correctionPayload :=
		seedCorrectionRevision(t, ctx, global, "high", "", false)
	var correctionParentID, globalKey, repositoryKey string
	if err := pool.QueryRow(ctx, `select parent_raw_document_id::text
		from app.raw_documents where id = $1::uuid`, correctionChildID).
		Scan(&correctionParentID); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ rawID, key *string }{
		{&global.childID, &globalKey}, {&repository.childID, &repositoryKey},
	} {
		if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
			where id = $1::uuid`, *pair.rawID).Scan(pair.key); err != nil {
			t.Fatal(err)
		}
	}
	globalEvent := seedAdvisoryObservation(t, ctx, pool, global,
		global.parentID, global.childID, global.now)
	correctionEvent := seedAdvisoryObservation(t, ctx, pool, global,
		correctionParentID, correctionChildID, global.now.Add(2*time.Minute))
	repositoryEvent := seedAdvisoryObservation(t, ctx, pool, repository,
		repository.parentID, repository.childID, repository.now.Add(4*time.Minute))
	nextRepositoryEvent := seedAdvisoryObservation(t, ctx, pool, repository,
		repository.parentID, repository.childID, repository.now.Add(5*time.Minute))
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `delete from river.river_job
			where queue = 'test_alert_assessment'
				and kind = 'assess_advisory_observation'
				and (args->>'eventId')::bigint = any($1::bigint[])`,
			[]int64{globalEvent, correctionEvent, repositoryEvent, nextRepositoryEvent}); err != nil {
			t.Errorf("clean up cross-source observation jobs: %v", err)
		}
	})
	store, err := New(pool, jobs, &fixtureReader{byObjectKey: map[string][]byte{
		globalKey:     bytes.Clone(global.payload),
		correctionKey: bytes.Clone(correctionPayload),
		repositoryKey: bytes.Clone(repository.payload),
	}}, clock.NewFixed(global.now.Add(6*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		id       int64
		revision string
	}{
		{globalEvent, global.revisionID},
		{correctionEvent, correctionRevisionID},
		{repositoryEvent, repository.revisionID},
		{nextRepositoryEvent, repository.revisionID},
	} {
		if err := store.AssessObservation(ctx, event.id, event.revision); err != nil {
			t.Fatalf("assess event %d: %v", event.id, err)
		}
	}
	assertAlertCount(t, ctx, pool, global.userID, 1)
	assertAlertCount(t, ctx, pool, repository.userID, 0)
	var correctedCount int
	if err := pool.QueryRow(ctx, `select count(*) from app.critical_alerts
		where user_id = $1::uuid and corrected_at is not null`,
		global.userID).Scan(&correctedCount); err != nil {
		t.Fatal(err)
	}
	if correctedCount != 1 {
		t.Fatalf("corrected global alert count = %d, want 1", correctedCount)
	}
	for _, eventID := range []int64{repositoryEvent, nextRepositoryEvent} {
		var state string
		if err := pool.QueryRow(ctx, `select state from app.advisory_entry_observations
			where id = $1`, eventID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "processed" {
			t.Fatalf("repository event %d state = %q, want processed", eventID, state)
		}
	}
}

func TestAdvisoryObservationKeepsTamperedEvidencePending(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	eventID := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_observation_tamper")
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Clone(fixture.payload)
	corrupt[0] ^= 1
	store, err := New(pool, jobs, &fixtureReader{payload: corrupt},
		clock.NewFixed(fixture.now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err == nil {
		t.Fatal("tampered advisory observation was assessed")
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	var state string
	if err := pool.QueryRow(ctx, `
		select state from app.advisory_entry_observations where id = $1`, eventID).
		Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("tampered advisory event state = %q, want pending", state)
	}
}

func TestObservedEntryRejectsUnobservedNewerRawAssessment(t *testing.T) {
	pool := openAssessmentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := seedAssessment(t, ctx, pool)
	newerChildID, newerRevisionID, newerKey, newerPayload :=
		seedCorrectionRevision(t, ctx, fixture, "critical", "", false)
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = null
		where id = $1::uuid`, fixture.watchID)
	eventID := seedAdvisoryObservation(t, ctx, pool, fixture,
		fixture.parentID, fixture.childID, fixture.now.Add(4*time.Minute))
	var firstKey string
	if err := pool.QueryRow(ctx, `select object_key from app.raw_documents
		where id = $1::uuid`, fixture.childID).Scan(&firstKey); err != nil {
		t.Fatal(err)
	}
	jobs, err := jobqueue.NewIsolatedTestInserter("test_alert_assessment")
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, jobs, &fixtureReader{byObjectKey: map[string][]byte{
		firstKey: bytes.Clone(fixture.payload), newerKey: bytes.Clone(newerPayload),
	}}, clock.NewFixed(fixture.now.Add(5*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AssessObservation(ctx, eventID, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	fixture.exec(t, ctx, `update app.watched_technologies set ecosystem = 'go'
		where id = $1::uuid`, fixture.watchID)
	if err := store.Assess(ctx, newerChildID, newerRevisionID); err != nil {
		t.Fatalf("reject stale unobserved raw assessment: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 0)
	if err := store.Assess(ctx, fixture.childID, fixture.revisionID); err != nil {
		t.Fatalf("replay latest observed entry: %v", err)
	}
	assertAlertCount(t, ctx, pool, fixture.userID, 1)
	var openingID int64
	if err := pool.QueryRow(ctx, `select opening_observation_id from app.critical_alerts
		where user_id = $1::uuid`, fixture.userID).Scan(&openingID); err != nil {
		t.Fatal(err)
	}
	if openingID != eventID {
		t.Fatalf("alert opened from event %d, want %d", openingID, eventID)
	}
}
