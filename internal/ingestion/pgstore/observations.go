package pgstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

func (store *Store) ManagedAdvisoryParent(ctx context.Context, rawDocumentID string) (bool, error) {
	if _, err := uuid.Parse(rawDocumentID); err != nil {
		return false, errors.New("managed advisory parent requires a raw document UUID")
	}
	var managed bool
	if err := store.pool.QueryRow(ctx, `select exists (
		select 1 from app.advisory_collection_observations
		where parent_raw_document_id = $1::uuid
	)`, rawDocumentID).Scan(&managed); err != nil {
		return false, fmt.Errorf("check managed advisory parent: %w", err)
	}
	return managed, nil
}

func (store *Store) LoadAdvisoryObservation(ctx context.Context, observationID int64) (*ingestion.AdvisoryCollectionObservation, error) {
	if observationID <= 0 {
		return nil, errors.New("advisory observation ID must be positive")
	}
	var selected ingestion.AdvisoryCollectionObservation
	var parentRawID string
	var parentRegistryID *string
	var splitCompletedAt *time.Time
	err := store.pool.QueryRow(ctx, `
		select observation.source_id, observation.source_registry_id,
			observation.parent_raw_document_id::text, raw.source_registry_id,
			observation.split_completed_at
		from app.advisory_collection_observations observation
		join app.raw_documents raw on raw.id = observation.parent_raw_document_id
		where observation.id = $1 and observation.state = 'pending'`, observationID).Scan(
		&selected.SourceID, &selected.RegistryID, &parentRawID,
		&parentRegistryID, &splitCompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load advisory observation %d: %w", observationID, err)
	}
	if splitCompletedAt != nil {
		return nil, nil
	}
	if parentRegistryID == nil {
		return nil, errors.New("advisory observation parent has no proven registry")
	}
	parent, err := store.LoadRawDocument(ctx, *parentRegistryID, parentRawID)
	if err != nil {
		return nil, fmt.Errorf("load advisory observation parent: %w", err)
	}
	if parent.SourceID != selected.SourceID || parent.Connector != sources.ConnectorGitHubAdvisories ||
		parent.ParentRawID != "" || parent.SourceEntryID != "" || parent.ObjectKey == "" {
		return nil, errors.New("advisory observation parent provenance is invalid")
	}
	selected.ID = observationID
	selected.ParentRegistryID = *parentRegistryID
	selected.Parent = parent
	return &selected, nil
}

func (store *Store) RecordAdvisoryObservationEntries(ctx context.Context, observation ingestion.AdvisoryCollectionObservation, entries []parsing.Entry) (int, error) {
	if store.objects == nil || store.clock == nil {
		return 0, errors.New("advisory observation storage is not configured")
	}
	if observation.ID <= 0 || observation.SourceID == "" || observation.RegistryID == "" ||
		observation.ParentRegistryID == "" || observation.Parent.ID == "" ||
		observation.Parent.SourceID != observation.SourceID ||
		observation.Parent.Connector != sources.ConnectorGitHubAdvisories ||
		observation.Parent.ParentRawID != "" || observation.Parent.SourceEntryID != "" ||
		observation.Parent.ContentPolicy != "link-and-excerpt" || len(entries) > 500 {
		return 0, errors.New("advisory observation entry batch is invalid")
	}
	seen := make(map[string][sha256.Size]byte, len(entries))
	for _, entry := range entries {
		if err := parsing.ValidateEntry(observation.Parent.URL, entry); err != nil {
			return 0, fmt.Errorf("validate advisory observation entry: %w", err)
		}
		digest := sha256.Sum256(entry.Payload)
		if prior, exists := seen[entry.ExternalID]; exists && prior != digest {
			return 0, errors.New("advisory observation repeats an external ID with conflicting content")
		}
		seen[entry.ExternalID] = digest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin advisory observation split: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sourceID, observingRegistryID, parentID, parentRegistryID, state string
	var splitCompletedAt *time.Time
	var observedAt time.Time
	var priorCount *int
	err = tx.QueryRow(ctx, `
		select observation.source_id, observation.source_registry_id,
			observation.parent_raw_document_id::text, raw.source_registry_id,
			observation.state, observation.split_completed_at, observation.entry_count,
			observation.observed_at
		from app.advisory_collection_observations observation
		join app.raw_documents raw on raw.id = observation.parent_raw_document_id
		where observation.id = $1
		for no key update of observation`, observation.ID).Scan(
		&sourceID, &observingRegistryID, &parentID, &parentRegistryID,
		&state, &splitCompletedAt, &priorCount, &observedAt)
	if err != nil {
		return 0, fmt.Errorf("lock advisory observation %d: %w", observation.ID, err)
	}
	if sourceID != observation.SourceID || observingRegistryID != observation.RegistryID ||
		parentID != observation.Parent.ID || parentRegistryID != observation.ParentRegistryID {
		return 0, errors.New("advisory observation identities changed before splitting")
	}
	if splitCompletedAt != nil {
		if priorCount == nil || *priorCount != len(entries) {
			return 0, errors.New("advisory observation split count changed")
		}
		return 0, nil
	}
	if state != "pending" {
		return 0, errors.New("advisory observation is not pending")
	}
	created := 0
	for ordinal, entry := range entries {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		inserted, err := store.recordEntry(ctx, observation.Parent,
			observation.ParentRegistryID, entry, observation.ID, ordinal)
		if err != nil {
			return created, err
		}
		if inserted {
			created++
		}
	}
	var eventCount int
	if err := tx.QueryRow(ctx, `select count(*)
		from app.advisory_entry_observations
		where collection_observation_id = $1`, observation.ID).Scan(&eventCount); err != nil {
		return created, fmt.Errorf("count durable advisory entry observations: %w", err)
	}
	if eventCount != len(entries) {
		return created, fmt.Errorf("advisory observation %d recorded %d of %d entries",
			observation.ID, eventCount, len(entries))
	}
	completedAt := store.clock.Now().UTC()
	if completedAt.Before(observedAt) {
		completedAt = observedAt
	}
	nextState := "pending"
	var processedAt *time.Time
	if len(entries) == 0 {
		nextState = "processed"
		processedAt = &completedAt
	}
	if _, err := tx.Exec(ctx, `update app.advisory_collection_observations
		set entry_count = $2, split_completed_at = $3,
			state = $4, processed_at = $5
		where id = $1 and state = 'pending' and split_completed_at is null`,
		observation.ID, len(entries), completedAt, nextState, processedAt); err != nil {
		return created, fmt.Errorf("complete advisory observation split: %w", err)
	}
	if _, err := tx.Exec(ctx, `update app.raw_documents
		set ingestion_error_code = null, ingestion_failed_at = null
		where id = $1::uuid and source_registry_id = $2
			and ingestion_error_code = 'pending_entries'`,
		observation.Parent.ID, observation.ParentRegistryID); err != nil {
		return created, fmt.Errorf("clear advisory collection replay marker: %w", err)
	}
	if _, err := store.enqueueFirstReadyAdvisoryEvent(ctx, tx, observation.SourceID, ""); err != nil {
		return created, err
	}
	if err := tx.Commit(ctx); err != nil {
		return created, fmt.Errorf("commit advisory observation split: %w", err)
	}
	return created, nil
}

func recordAdvisoryEntryObservation(ctx context.Context, tx pgx.Tx, observationID int64, ordinal int, sourceEntryID string, rawID string) error {
	if observationID == 0 {
		return nil
	}
	if observationID < 0 || ordinal < 0 || ordinal >= 500 {
		return errors.New("advisory entry observation identity is invalid")
	}
	result, err := tx.Exec(ctx, `insert into app.advisory_entry_observations (
		collection_observation_id, entry_ordinal, source_entry_id, child_raw_document_id
	) values ($1, $2, $3::uuid, $4::uuid)
		on conflict (collection_observation_id, entry_ordinal) do nothing`,
		observationID, ordinal, sourceEntryID, rawID)
	if err != nil {
		return fmt.Errorf("record advisory entry observation: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var recordedSourceEntryID, recordedRawID string
	if err := tx.QueryRow(ctx, `select source_entry_id::text, child_raw_document_id::text
		from app.advisory_entry_observations
		where collection_observation_id = $1 and entry_ordinal = $2`,
		observationID, ordinal).Scan(&recordedSourceEntryID, &recordedRawID); err != nil {
		return fmt.Errorf("verify repeated advisory entry observation: %w", err)
	}
	if recordedSourceEntryID != sourceEntryID || recordedRawID != rawID {
		return errors.New("advisory entry observation ordinal changed identity")
	}
	return nil
}
