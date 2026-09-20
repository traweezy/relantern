package pgstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/jobqueue"
)

// observedEvidence ties a parsed child to one particular collection fetch.
// The child raw row may be older than this event when A follows B and then A
// reappears with byte-identical content.
type observedEvidence struct {
	evidence
	eventID           int64
	collectionID      int64
	entryOrdinal      int
	rawDocumentID     string
	parentRawID       string
	observedEndpoint  string
	observedParentURL string
}

var ErrEpisodeCutoverPending = errors.New("critical alert episode uniqueness cutover is pending")

const observationEvidenceQuery = `
	select child.object_key, child.raw_sha256, child.source_id,
		child.canonical_url, entry.id::text, entry.external_id,
		entry.first_seen_at, endpoint.url, parent.canonical_url,
		revision.title, collection.observed_at, item.id::text, item.status,
		event.id, collection.id, event.entry_ordinal,
		child.id::text,
		observed_parent.id::text, observed_endpoint.url,
		observed_parent.canonical_url
	from app.advisory_entry_observations event
	join app.advisory_collection_observations collection
		on collection.id = event.collection_observation_id
	join app.raw_documents observed_parent
		on observed_parent.id = collection.parent_raw_document_id
	join app.source_endpoints observed_endpoint
		on observed_endpoint.registry_id = collection.source_registry_id
	join app.raw_documents child on child.id = event.child_raw_document_id
	join app.raw_documents parent on parent.id = child.parent_raw_document_id
	join app.source_entries entry on entry.id = event.source_entry_id
	join app.sources source on source.id = collection.source_id
	join app.source_endpoints endpoint on endpoint.registry_id = child.source_registry_id
	join app.content_revisions revision on revision.raw_document_id = child.id
	join app.item_sources item_source on item_source.revision_id = revision.id
	join app.items item on item.id = item_source.item_id
	where event.id = $1 and revision.id = $2::uuid
		and event.state = $3 and collection.state = $3
		and collection.split_completed_at is not null
		and event.entry_ordinal < collection.entry_count
		and observed_endpoint.source_id = collection.source_id
		and observed_endpoint.connector = 'github_advisories'
		and observed_parent.source_id = collection.source_id
		and ($3 = 'processed' or (observed_parent.object_key is not null
			and observed_parent.raw_pruned_at is null))
		and child.source_id = collection.source_id
		and child.source_entry_id = event.source_entry_id
		and parent.source_id = child.source_id
		and entry.source_id = child.source_id
		and endpoint.source_id = child.source_id
		and endpoint.connector = 'github_advisories'
		and child.source_connector = 'source_entry'
		and child.content_policy = 'link-and-excerpt'
		and child.object_key is not null and child.raw_pruned_at is null
		and child.ingestion_error_code is null
		and source.enabled and source.validation_state = 'active'
		and source.trust_tier in ('T0', 'T1')
		and item_source.source_tier = source.trust_tier
		and item_source.canonical_url = child.canonical_url`

func loadObservationEvidence(
	ctx context.Context, query rowQuerier, eventID int64, revisionID string, lock bool,
) (observedEvidence, bool, error) {
	return loadObservationEvidenceState(ctx, query, eventID, revisionID, "pending", lock)
}

func loadObservationEvidenceState(
	ctx context.Context, query rowQuerier, eventID int64, revisionID, state string, lock bool,
) (observedEvidence, bool, error) {
	statement := observationEvidenceQuery
	if lock {
		statement += ` for update of event, collection
			for share of child, parent, entry, source, endpoint,
				observed_parent, observed_endpoint, revision, item_source, item`
	}
	var found observedEvidence
	err := query.QueryRow(ctx, statement, eventID, revisionID, state).Scan(
		&found.objectKey, &found.rawSHA256, &found.sourceID,
		&found.canonicalURL, &found.entryRowID, &found.entryID,
		&found.entryFirstSeenAt, &found.endpointURL, &found.parentURL,
		&found.title, &found.observedAt, &found.itemID, &found.itemStatus,
		&found.eventID, &found.collectionID, &found.entryOrdinal,
		&found.rawDocumentID,
		&found.parentRawID, &found.observedEndpoint, &found.observedParentURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return observedEvidence{}, false, nil
	}
	if err != nil {
		return observedEvidence{}, false, fmt.Errorf("load advisory observation evidence: %w", err)
	}
	if !officialAdvisoryParent(found.endpointURL, found.parentURL) ||
		!officialAdvisoryParent(found.observedEndpoint, found.observedParentURL) ||
		!officialAdvisoryEndpoint(found.observedEndpoint) {
		return observedEvidence{}, false, errors.New("advisory observation parent is not official")
	}
	return found, true, nil
}

func sameObservationEvidence(first, second observedEvidence) bool {
	return first.objectKey == second.objectKey &&
		equalDigest(first.rawSHA256, second.rawSHA256) &&
		first.sourceID == second.sourceID &&
		first.canonicalURL == second.canonicalURL &&
		first.entryRowID == second.entryRowID &&
		first.entryID == second.entryID &&
		first.entryFirstSeenAt.Equal(second.entryFirstSeenAt) &&
		first.endpointURL == second.endpointURL &&
		first.parentURL == second.parentURL &&
		first.title == second.title &&
		first.observedAt.Equal(second.observedAt) &&
		first.itemID == second.itemID &&
		first.itemStatus == second.itemStatus &&
		first.eventID == second.eventID &&
		first.collectionID == second.collectionID &&
		first.entryOrdinal == second.entryOrdinal &&
		first.rawDocumentID == second.rawDocumentID &&
		first.parentRawID == second.parentRawID &&
		first.observedEndpoint == second.observedEndpoint &&
		first.observedParentURL == second.observedParentURL
}

func observationProcessed(ctx context.Context, query rowQuerier, eventID int64) (bool, error) {
	var state string
	err := query.QueryRow(ctx, `
		select state from app.advisory_entry_observations where id = $1`, eventID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("advisory observation %d does not exist", eventID)
	}
	if err != nil {
		return false, fmt.Errorf("inspect advisory observation state: %w", err)
	}
	return state == "processed", nil
}

// AssessObservation commits the alert decision and ordered event
// acknowledgement together. A retry of a completed River job is harmless.
func (store *Store) AssessObservation(ctx context.Context, eventID int64, revisionID string) error {
	if eventID <= 0 {
		return errors.New("advisory observation ID must be positive")
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return errors.New("advisory observation revision ID is invalid")
	}
	preflight, ok, err := loadObservationEvidence(ctx, store.pool, eventID, revisionID, false)
	if err != nil {
		return err
	}
	if !ok {
		processed, stateErr := observationProcessed(ctx, store.pool, eventID)
		if stateErr != nil {
			return stateErr
		}
		if processed {
			return nil
		}
		return fmt.Errorf("advisory observation %d lacks validated parsed evidence", eventID)
	}
	advisory, sourceURL, err := store.readAdvisory(ctx, preflight.evidence)
	if err != nil {
		return err
	}
	now := store.clock.Now().UTC()
	if now.IsZero() {
		return errors.New("critical advisory assessment clock is zero")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin advisory observation assessment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedSourceID string
	if err := tx.QueryRow(ctx, `select id from app.sources where id = $1 for update`,
		preflight.sourceID).Scan(&lockedSourceID); err != nil {
		return fmt.Errorf("lock advisory observation source: %w", err)
	}
	current, ok, err := loadObservationEvidence(ctx, tx, eventID, revisionID, true)
	if err != nil {
		return err
	}
	if !ok {
		processed, stateErr := observationProcessed(ctx, tx, eventID)
		if stateErr != nil {
			return stateErr
		}
		if processed {
			return nil
		}
		return fmt.Errorf("advisory observation %d lost validated evidence", eventID)
	}
	if !sameObservationEvidence(preflight, current) || lockedSourceID != current.sourceID {
		return errors.New("advisory observation evidence changed during assessment")
	}
	if err := requireEarliestObservation(ctx, tx, current); err != nil {
		return err
	}
	if current.title == "" || len(current.title) > 500 ||
		sourceURL == "" || len(sourceURL) > 4096 {
		return errors.New("advisory title or source URL exceeds alert bounds")
	}
	if reason := advisoryCorrectionReason(advisory); reason != "" {
		if err := store.correctObservedAlerts(ctx, tx, current, revisionID,
			advisory.ID, reason, now); err != nil {
			return err
		}
	} else {
		if err := store.admitObservedAlerts(ctx, tx, current, revisionID,
			advisory, sourceURL, now); err != nil {
			return err
		}
	}
	if err := store.acknowledgeObservation(ctx, tx, current, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit advisory observation assessment: %w", err)
	}
	return nil
}

func requireEarliestObservation(ctx context.Context, tx pgx.Tx, current observedEvidence) error {
	var earlier bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from app.advisory_collection_observations older
			where older.source_id = $1 and older.id < $2 and older.state = 'pending'
		) or exists (
			select 1 from app.advisory_entry_observations older
			where older.collection_observation_id = $2
				and older.entry_ordinal < $3 and older.state = 'pending'
		)`, current.sourceID, current.collectionID, current.entryOrdinal).Scan(&earlier); err != nil {
		return fmt.Errorf("check advisory observation order: %w", err)
	}
	if earlier {
		return fmt.Errorf("advisory observation %d is behind an earlier pending event", current.eventID)
	}
	return nil
}

func (store *Store) acknowledgeObservation(ctx context.Context, tx pgx.Tx, current observedEvidence, now time.Time) error {
	result, err := tx.Exec(ctx, `
		update app.advisory_entry_observations
		set state = 'processed', processed_at = $2
		where id = $1 and state = 'pending'`, current.eventID, now)
	if err != nil {
		return fmt.Errorf("acknowledge advisory entry observation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("advisory entry observation was already acknowledged")
	}
	if _, err := tx.Exec(ctx, `
		update app.advisory_collection_observations collection
		set state = 'processed', processed_at = greatest($2::timestamptz, collection.split_completed_at)
		where collection.id = $1 and collection.state = 'pending'
			and collection.split_completed_at is not null
			and collection.entry_count = (
				select count(*) from app.advisory_entry_observations event
				where event.collection_observation_id = collection.id
			)
			and not exists (
				select 1 from app.advisory_entry_observations event
				where event.collection_observation_id = collection.id
					and event.state = 'pending'
			)`, current.collectionID, now); err != nil {
		return fmt.Errorf("complete advisory collection observation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update app.raw_documents raw
		set ingestion_error_code = null, ingestion_failed_at = null
		where raw.id = $1::uuid and raw.ingestion_error_code = 'pending_observation'
			and not exists (
				select 1 from app.advisory_collection_observations collection
				where collection.parent_raw_document_id = raw.id
					and collection.state = 'pending'
			)`, current.parentRawID); err != nil {
		return fmt.Errorf("clear completed advisory parent marker: %w", err)
	}
	return store.enqueueNextObservation(ctx, tx, current.sourceID)
}

// Progress the source immediately after each assessment. Periodic
// reconciliation remains the crash recovery path; it is not the steady-state
// clock for a page containing hundreds of entries.
func (store *Store) enqueueNextObservation(ctx context.Context, tx pgx.Tx, sourceID string) error {
	var collectionID int64
	var splitCompletedAt *time.Time
	err := tx.QueryRow(ctx, `
		select id, split_completed_at
		from app.advisory_collection_observations
		where source_id = $1 and state = 'pending'
		order by id limit 1`, sourceID).Scan(&collectionID, &splitCompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load next advisory collection observation: %w", err)
	}
	if splitCompletedAt == nil {
		_, _, err := store.jobs.EnqueueSplitAdvisoryObservation(ctx, tx,
			jobqueue.SplitAdvisoryObservationArgs{ObservationID: collectionID})
		return err
	}
	var eventID int64
	var rawDocumentID string
	var registryID, objectKey, marker, revisionID *string
	err = tx.QueryRow(ctx, `
		select event.id, child.id::text, child.source_registry_id,
			child.object_key, child.ingestion_error_code, revision.id::text
		from app.advisory_entry_observations event
		join app.raw_documents child on child.id = event.child_raw_document_id
		left join lateral (
			select id from app.content_revisions
			where raw_document_id = child.id
			order by observed_at desc, id desc limit 1
		) revision on true
		where event.collection_observation_id = $1 and event.state = 'pending'
		order by event.entry_ordinal, event.id limit 1`, collectionID).Scan(
		&eventID, &rawDocumentID, &registryID, &objectKey, &marker, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("pending advisory collection has no pending entry event")
	}
	if err != nil {
		return fmt.Errorf("load next advisory entry observation: %w", err)
	}
	if revisionID != nil && marker == nil {
		_, _, err := store.jobs.EnqueueAssessAdvisoryObservation(ctx, tx,
			jobqueue.AssessAdvisoryObservationArgs{EventID: eventID, RevisionID: *revisionID})
		return err
	}
	if registryID == nil || objectKey == nil {
		// Preserve the acknowledgement. The pending event remains visible to
		// operability until its evidence is repaired or restored.
		return nil
	}
	if marker == nil {
		if _, err := tx.Exec(ctx, `
			update app.raw_documents
			set ingestion_error_code = 'pending_parse', ingestion_failed_at = now()
			where id = $1::uuid and ingestion_error_code is null`, rawDocumentID); err != nil {
			return fmt.Errorf("restore next advisory child parse marker: %w", err)
		}
	}
	_, _, err = store.jobs.EnqueueParseRawDocument(ctx, tx, jobqueue.ParseRawDocumentArgs{
		RawDocumentID: rawDocumentID, RegistryID: *registryID,
	})
	return err
}

func (store *Store) admitObservedAlerts(
	ctx context.Context, tx pgx.Tx, current observedEvidence, revisionID string,
	advisory alert.Advisory, sourceURL string, now time.Time,
) error {
	owners, err := loadOwnerWatches(ctx, tx, advisory)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		matches := alert.Match(advisory, owner.watches)
		if len(matches) == 0 {
			continue
		}
		scheduledAt, err := alert.DeliveryAt(now, owner.quiet)
		if err != nil {
			return fmt.Errorf("schedule observed critical alert for owner %s: %w", owner.userID, err)
		}
		for _, match := range matches {
			episode, err := nextAlertEpisode(ctx, tx, owner.userID,
				advisory.ID, match.Vulnerability.Ecosystem,
				match.Vulnerability.PackageName, current.entryRowID)
			if err != nil {
				return err
			}
			if episode == 0 {
				continue
			}
			openingID := current.eventID
			if err := store.insertMatch(ctx, tx, current.rawDocumentID,
				revisionID, current.evidence, sourceURL, advisory.ID, owner.userID,
				match, owner.channels, scheduledAt, episode, &openingID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Zero means the latest episode is still active. A corrected episode can
// reopen only from its own official source entry; cross-source authority needs
// an explicit rule before any new external delivery is admitted.
func nextAlertEpisode(
	ctx context.Context, tx pgx.Tx, userID, advisoryID, ecosystem, packageName,
	entryRowID string,
) (int, error) {
	var episode int
	var originalEntryID string
	var correctedAt *time.Time
	err := tx.QueryRow(ctx, `
		select alert.episode_number, original.source_entry_id::text, alert.corrected_at
		from app.critical_alerts alert
		join app.raw_documents original on original.id = alert.raw_document_id
		where alert.user_id = $1::uuid and alert.advisory_id = $2
			and alert.ecosystem = $3 and alert.package_name = $4
		order by alert.episode_number desc
		limit 1 for update of alert`, userID, advisoryID, ecosystem, packageName).
		Scan(&episode, &originalEntryID, &correctedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect latest critical alert episode: %w", err)
	}
	if correctedAt == nil {
		return 0, nil
	}
	if originalEntryID != entryRowID {
		return 0, errors.New("critical alert reactivation needs cross-source authority review")
	}
	var legacyUniqueExists bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from pg_constraint
			where conrelid = 'app.critical_alerts'::regclass
				and conname = 'critical_alerts_user_advisory_package_key'
		)`).Scan(&legacyUniqueExists); err != nil {
		return 0, fmt.Errorf("inspect critical alert episode cutover: %w", err)
	}
	if legacyUniqueExists {
		return 0, ErrEpisodeCutoverPending
	}
	if episode >= math.MaxInt32 {
		return 0, errors.New("critical alert episode limit reached")
	}
	return episode + 1, nil
}

func (store *Store) correctObservedAlerts(
	ctx context.Context, tx pgx.Tx, current observedEvidence,
	revisionID, advisoryID, reason string, now time.Time,
) error {
	rows, err := tx.Query(ctx, `
		select alert.id::text, alert.user_id::text, alert.ecosystem,
			alert.package_name, alert.episode_number,
			coalesce((opening.collection_observation_id, opening.entry_ordinal)
				>= ($3::bigint, $4::integer), false),
			coalesce((correction.collection_observation_id, correction.entry_ordinal)
				>= ($3::bigint, $4::integer), false)
		from app.critical_alerts alert
		join app.raw_documents original on original.id = alert.raw_document_id
		left join app.advisory_entry_observations opening
			on opening.id = alert.opening_observation_id
		left join app.advisory_entry_observations correction
			on correction.id = alert.correction_observation_id
		where original.source_entry_id = $1::uuid and alert.advisory_id = $2
		order by alert.user_id, alert.ecosystem, alert.package_name,
			alert.episode_number desc
		for update of alert`, current.entryRowID, advisoryID,
		current.collectionID, current.entryOrdinal)
	if err != nil {
		return fmt.Errorf("lock observed critical alert episodes: %w", err)
	}
	type target struct {
		id                         string
		openingIsCurrentOrLater    bool
		correctionIsCurrentOrLater bool
	}
	targets := make([]target, 0)
	lastKey := ""
	for rows.Next() {
		var selected target
		var userID, ecosystem, packageName string
		var episode int
		if err := rows.Scan(&selected.id, &userID, &ecosystem, &packageName,
			&episode, &selected.openingIsCurrentOrLater,
			&selected.correctionIsCurrentOrLater); err != nil {
			rows.Close()
			return fmt.Errorf("scan observed critical alert episode: %w", err)
		}
		key := userID + "\x00" + ecosystem + "\x00" + packageName
		if key == lastKey {
			continue
		}
		lastKey = key
		if selected.openingIsCurrentOrLater || selected.correctionIsCurrentOrLater {
			continue
		}
		targets = append(targets, selected)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate observed critical alert episodes: %w", err)
	}
	rows.Close()
	if len(targets) == 0 {
		return nil
	}
	IDs := make([]string, 0, len(targets))
	for _, selected := range targets {
		IDs = append(IDs, selected.id)
	}
	if _, err := tx.Exec(ctx, `
		update app.critical_alerts alert
		set correction_reason = $2, correction_revision_id = $3::uuid,
			corrected_at = $4, correction_observation_id = $5
		where alert.id = any($1::uuid[])`, IDs, reason, revisionID, now, current.eventID); err != nil {
		return fmt.Errorf("record observed critical alert correction: %w", err)
	}
	deliveries, err := tx.Query(ctx, `
		select id::text from app.critical_alert_deliveries
		where alert_id = any($1::uuid[]) for update`, IDs)
	if err != nil {
		return fmt.Errorf("lock observed critical alert deliveries: %w", err)
	}
	for deliveries.Next() {
		var deliveryID string
		if err := deliveries.Scan(&deliveryID); err != nil {
			deliveries.Close()
			return fmt.Errorf("scan observed critical alert delivery: %w", err)
		}
	}
	if err := deliveries.Err(); err != nil {
		deliveries.Close()
		return fmt.Errorf("iterate observed critical alert deliveries: %w", err)
	}
	deliveries.Close()
	if _, err := tx.Exec(ctx, `
		update app.critical_alert_deliveries
		set state = 'suppressed', updated_at = $2
		where alert_id = any($1::uuid[]) and state in ('pending', 'failed')`, IDs, now); err != nil {
		return fmt.Errorf("suppress observed critical alert deliveries: %w", err)
	}
	return nil
}
