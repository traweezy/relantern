package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
)

// NextPendingAdvisoryEntryEvent exposes the source's earliest unacknowledged
// entry only after its collection split is durable. A missing revision leaves
// RevisionID empty until child parsing and handoff complete.
func (store *Store) NextPendingAdvisoryEntryEvent(ctx context.Context, sourceID string) (*ingestion.AdvisoryEntryEvent, error) {
	if sourceID == "" {
		return nil, errors.New("advisory event source ID is required")
	}
	var selected ingestion.AdvisoryEntryEvent
	err := store.pool.QueryRow(ctx, `
		select event.id, observation.id, child.id::text,
			case when child.ingestion_error_code is null
				then coalesce(revision.id::text, '') else '' end
		from app.advisory_collection_observations observation
		join app.advisory_entry_observations event
			on event.collection_observation_id = observation.id
		join app.raw_documents child on child.id = event.child_raw_document_id
		left join lateral (
			select revision.id from app.content_revisions revision
			where revision.raw_document_id = child.id
			order by revision.observed_at desc, revision.id desc limit 1
		) revision on true
		where observation.source_id = $1 and observation.state = 'pending'
			and observation.split_completed_at is not null
			and observation.id = (
				select min(earlier.id)
				from app.advisory_collection_observations earlier
				where earlier.source_id = $1 and earlier.state = 'pending'
			)
			and event.state = 'pending'
		order by event.entry_ordinal, event.id
		limit 1`, sourceID).Scan(&selected.ID, &selected.CollectionObservationID,
		&selected.RawDocumentID, &selected.RevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load next advisory entry event: %w", err)
	}
	return &selected, nil
}

func (store *Store) enqueueFirstReadyAdvisoryEvent(ctx context.Context, tx pgx.Tx, sourceID string, childRawID string) (bool, error) {
	var eventID int64
	var revisionID string
	err := tx.QueryRow(ctx, `
		select event.id, revision.id::text
		from app.advisory_collection_observations observation
		join app.advisory_entry_observations event
			on event.collection_observation_id = observation.id
		join app.raw_documents child on child.id = event.child_raw_document_id
		join app.sources source on source.id = observation.source_id
		join lateral (
			select revision.id from app.content_revisions revision
			where revision.raw_document_id = child.id
			order by revision.observed_at desc, revision.id desc limit 1
		) revision on true
		where observation.source_id = $1 and observation.state = 'pending'
			and observation.split_completed_at is not null
			and source.trust_tier in ('T0', 'T1')
			and event.state = 'pending' and child.ingestion_error_code is null
			and ($2 = '' or child.id = nullif($2, '')::uuid)
			and observation.id = (
				select min(earlier.id)
				from app.advisory_collection_observations earlier
				where earlier.source_id = $1 and earlier.state = 'pending'
			)
			and event.entry_ordinal = (
				select min(earlier_event.entry_ordinal)
				from app.advisory_entry_observations earlier_event
				where earlier_event.collection_observation_id = observation.id
					and earlier_event.state = 'pending'
			)
		order by event.id
		limit 1`, sourceID, childRawID).Scan(&eventID, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select ready advisory observation event: %w", err)
	}
	_, inserted, err := store.jobs.EnqueueAssessAdvisoryObservation(ctx, tx,
		jobqueue.AssessAdvisoryObservationArgs{EventID: eventID, RevisionID: revisionID})
	return inserted, err
}

// ReconcileAdvisoryObservations restores split, parse, and assessment jobs
// from durable observations. Only the earliest pending event of each source
// can be queued for assessment; the alert store rechecks that order when it
// commits the event's effects.
func (store *Store) ReconcileAdvisoryObservations(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, errors.New("advisory observation batch size must be between 1 and 500")
	}
	splits, splitErr := store.reconcileAdvisorySplits(ctx, limit)
	events, eventErr := store.reconcileAdvisoryEvents(ctx, limit)
	return splits + events, errors.Join(splitErr, eventErr)
}

func (store *Store) reconcileAdvisorySplits(ctx context.Context, limit int) (int, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin advisory split reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select observation.id
		from app.advisory_collection_observations observation
		join app.raw_documents raw on raw.id = observation.parent_raw_document_id
		join app.source_endpoints endpoint on endpoint.registry_id = raw.source_registry_id
		join app.sources source on source.id = observation.source_id
		where observation.state = 'pending' and observation.split_completed_at is null
			and raw.object_key is not null
			and source.enabled and source.validation_state = 'active'
			and endpoint.health_state not in ('paused', 'failed')
		order by observation.id
		limit $1
		for no key update of observation skip locked`, limit)
	if err != nil {
		return 0, fmt.Errorf("select unsplit advisory observations: %w", err)
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan unsplit advisory observation: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate unsplit advisory observations: %w", err)
	}
	rows.Close()
	inserted := 0
	for _, id := range ids {
		_, newJob, err := store.jobs.EnqueueSplitAdvisoryObservation(ctx, tx,
			jobqueue.SplitAdvisoryObservationArgs{ObservationID: id})
		if err != nil {
			return 0, err
		}
		if newJob {
			inserted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit advisory split reconciliation: %w", err)
	}
	return inserted, nil
}

type pendingAdvisoryEvent struct {
	id             int64
	rawID          string
	registryID     *string
	objectKey      *string
	marker         *string
	revisionID     *string
	sourceEnabled  bool
	sourceState    string
	endpointHealth *string
}

func (store *Store) reconcileAdvisoryEvents(ctx context.Context, limit int) (int, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin advisory event reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select event.id, child.id::text, child.source_registry_id,
			child.object_key, child.ingestion_error_code, revision.id::text,
			source.enabled, source.validation_state, endpoint.health_state
		from app.advisory_collection_observations observation
		join app.advisory_entry_observations event
			on event.collection_observation_id = observation.id
		join app.raw_documents child on child.id = event.child_raw_document_id
		join app.sources source on source.id = observation.source_id
		left join app.source_endpoints endpoint
			on endpoint.registry_id = child.source_registry_id
		left join lateral (
			select revision.id from app.content_revisions revision
			where revision.raw_document_id = child.id
			order by revision.observed_at desc, revision.id desc limit 1
		) revision on true
		where observation.state = 'pending'
			and observation.split_completed_at is not null
			and event.state = 'pending'
			and not exists (
				select 1 from app.advisory_collection_observations earlier
				where earlier.source_id = observation.source_id
					and earlier.id < observation.id and earlier.state = 'pending'
			)
			and not exists (
				select 1 from app.advisory_entry_observations earlier_event
				where earlier_event.collection_observation_id = observation.id
					and earlier_event.entry_ordinal < event.entry_ordinal
					and earlier_event.state = 'pending'
			)
		order by observation.source_id, observation.id, event.entry_ordinal
		limit $1
		for no key update of event skip locked`, limit)
	if err != nil {
		return 0, fmt.Errorf("select pending advisory events: %w", err)
	}
	candidates := make([]pendingAdvisoryEvent, 0, limit)
	for rows.Next() {
		var candidate pendingAdvisoryEvent
		if err := rows.Scan(&candidate.id, &candidate.rawID,
			&candidate.registryID, &candidate.objectKey, &candidate.marker,
			&candidate.revisionID, &candidate.sourceEnabled,
			&candidate.sourceState, &candidate.endpointHealth); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan pending advisory event: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate pending advisory events: %w", err)
	}
	rows.Close()
	inserted := 0
	var blocked []error
	for _, candidate := range candidates {
		if candidate.revisionID != nil && candidate.marker == nil {
			_, newJob, err := store.jobs.EnqueueAssessAdvisoryObservation(ctx, tx,
				jobqueue.AssessAdvisoryObservationArgs{
					EventID: candidate.id, RevisionID: *candidate.revisionID,
				})
			if err != nil {
				return 0, err
			}
			if newJob {
				inserted++
			}
			continue
		}
		if candidate.registryID == nil || candidate.objectKey == nil {
			blocked = append(blocked, fmt.Errorf("advisory event %d has no replayable raw evidence", candidate.id))
			continue
		}
		if !candidate.sourceEnabled || candidate.sourceState != "active" ||
			candidate.endpointHealth == nil || *candidate.endpointHealth == "paused" ||
			*candidate.endpointHealth == "failed" {
			continue
		}
		if candidate.marker == nil {
			if _, err := tx.Exec(ctx, `update app.raw_documents
				set ingestion_error_code = 'pending_parse', ingestion_failed_at = now()
				where id = $1::uuid and ingestion_error_code is null`,
				candidate.rawID); err != nil {
				return 0, fmt.Errorf("restore advisory child parse marker: %w", err)
			}
		}
		_, newJob, err := store.jobs.EnqueueParseRawDocument(ctx, tx,
			jobqueue.ParseRawDocumentArgs{
				RawDocumentID: candidate.rawID, RegistryID: *candidate.registryID,
			})
		if err != nil {
			return 0, err
		}
		if newJob {
			inserted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit advisory event reconciliation: %w", err)
	}
	return inserted, errors.Join(blocked...)
}
