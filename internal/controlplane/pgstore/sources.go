package pgstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/controlplane"
)

func (store *Store) Sources(
	ctx context.Context,
	userID string,
	now time.Time,
) (controlplane.SourcesSnapshot, error) {
	rows, err := store.pool.Query(ctx, `
		select
			source.id,
			source.name,
			source.trust_tier,
			source.owner,
			source.origin,
			source.validation_state,
			source.homepage_url,
			source.content_policy,
			source.enabled,
			source.enabled and coalesce(runtime.polling_enabled, true),
			source.topics,
			source.reviewed_at,
			coalesce(preference.muted, false),
			coalesce(preference.exclude_from_digest, false),
			coalesce(preference.relevance_adjustment, 0)::double precision,
			coalesce(preference.version, 0)
		from app.sources source
		left join app.source_runtime_overrides runtime on runtime.source_id = source.id
		left join app.user_source_preferences preference
			on preference.source_id = source.id and preference.user_id = $1::uuid
		order by
			case source.trust_tier when 'T0' then 0 when 'T1' then 1 when 'T2' then 2 else 3 end,
			source.name,
			source.id`, userID)
	if err != nil {
		return controlplane.SourcesSnapshot{}, fmt.Errorf("list control-plane sources: %w", err)
	}
	defer rows.Close()

	sources := make([]controlplane.ManagedSource, 0)
	for rows.Next() {
		var source controlplane.ManagedSource
		if err := rows.Scan(
			&source.ID,
			&source.Name,
			&source.TrustTier,
			&source.Owner,
			&source.Origin,
			&source.ValidationState,
			&source.HomepageURL,
			&source.ContentPolicy,
			&source.Enabled,
			&source.PollingEnabled,
			&source.Topics,
			&source.ReviewedAt,
			&source.Preference.Muted,
			&source.Preference.ExcludeFromDigest,
			&source.Preference.RelevanceAdjustment,
			&source.Preference.Version,
		); err != nil {
			return controlplane.SourcesSnapshot{}, fmt.Errorf("scan control-plane source: %w", err)
		}
		source.Endpoints = make([]controlplane.SourceEndpoint, 0)
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return controlplane.SourcesSnapshot{}, fmt.Errorf("iterate control-plane sources: %w", err)
	}
	byID := make(map[string]*controlplane.ManagedSource, len(sources))
	for index := range sources {
		byID[sources[index].ID] = &sources[index]
	}
	if err := store.loadSourceEndpoints(ctx, byID); err != nil {
		return controlplane.SourcesSnapshot{}, err
	}
	if err := store.loadSourceMetrics(ctx, byID, now); err != nil {
		return controlplane.SourcesSnapshot{}, err
	}
	if err := store.loadSourceValidations(ctx, byID); err != nil {
		return controlplane.SourcesSnapshot{}, err
	}
	return controlplane.SourcesSnapshot{GeneratedAt: now.UTC(), Sources: sources}, nil
}

func (store *Store) loadSourceEndpoints(
	ctx context.Context,
	sources map[string]*controlplane.ManagedSource,
) error {
	rows, err := store.pool.Query(ctx, `
		select
			endpoint.source_id,
			endpoint.id::text,
			endpoint.connector,
			endpoint.url,
			extract(epoch from endpoint.poll_interval)::bigint,
			endpoint.priority,
			endpoint.health_state,
			endpoint.next_poll_at,
			endpoint.last_attempt_at,
			endpoint.last_success_at,
			latest.status_code,
			coalesce(latest.error_code, '')
		from app.source_endpoints endpoint
		left join lateral (
			select fetch_record.status_code, fetch_record.error_code
			from app.source_fetches fetch_record
			where fetch_record.endpoint_id = endpoint.id
			order by fetch_record.attempted_at desc, fetch_record.id desc
			limit 1
		) latest on true
		order by endpoint.source_id, endpoint.priority, endpoint.registry_id`)
	if err != nil {
		return fmt.Errorf("list source endpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID string
		var endpoint controlplane.SourceEndpoint
		if err := rows.Scan(
			&sourceID,
			&endpoint.ID,
			&endpoint.Connector,
			&endpoint.URL,
			&endpoint.PollIntervalSeconds,
			&endpoint.Priority,
			&endpoint.HealthState,
			&endpoint.NextPollAt,
			&endpoint.LastAttemptAt,
			&endpoint.LastSuccessAt,
			&endpoint.LatestStatusCode,
			&endpoint.LatestErrorCode,
		); err != nil {
			return fmt.Errorf("scan source endpoint: %w", err)
		}
		if source := sources[sourceID]; source != nil {
			source.Endpoints = append(source.Endpoints, endpoint)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source endpoints: %w", err)
	}
	return nil
}

func (store *Store) loadSourceMetrics(
	ctx context.Context,
	sources map[string]*controlplane.ManagedSource,
	now time.Time,
) error {
	rows, err := store.pool.Query(ctx, `
		with content as (
			select document.source_id, count(*)::bigint as content_count
			from app.raw_documents document
			group by document.source_id
		), cluster_sizes as (
			select member.cluster_id, count(*)::bigint as member_count
			from app.cluster_members member
			group by member.cluster_id
		), duplicates as (
			select
				document.source_id,
				count(item.id)::bigint as item_count,
				count(item.id) filter (where size.member_count > 1)::bigint as duplicate_count
			from app.raw_documents document
			left join app.content_revisions revision on revision.raw_document_id = document.id
			left join app.items item on item.current_revision_id = revision.id
			left join app.cluster_members member on member.item_id = item.id
			left join cluster_sizes size on size.cluster_id = member.cluster_id
			group by document.source_id
		), fetches as (
			select
				endpoint.source_id,
				count(*) filter (where fetch_record.outcome <> 'failed')::bigint as success_count,
				count(*) filter (where fetch_record.outcome = 'failed')::bigint as failure_count
			from app.source_endpoints endpoint
			join app.source_fetches fetch_record on fetch_record.endpoint_id = endpoint.id
			where fetch_record.attempted_at >= $1::timestamptz - interval '24 hours'
			group by endpoint.source_id
		)
		select
			source.id,
			coalesce(content.content_count, 0),
			coalesce(duplicates.item_count, 0),
			coalesce(duplicates.duplicate_count, 0),
			coalesce(fetches.success_count, 0),
			coalesce(fetches.failure_count, 0)
		from app.sources source
		left join content on content.source_id = source.id
		left join duplicates on duplicates.source_id = source.id
		left join fetches on fetches.source_id = source.id`, now.UTC())
	if err != nil {
		return fmt.Errorf("load source metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID string
		var contentCount, itemCount, duplicateCount, successCount, failureCount int64
		if err := rows.Scan(
			&sourceID,
			&contentCount,
			&itemCount,
			&duplicateCount,
			&successCount,
			&failureCount,
		); err != nil {
			return fmt.Errorf("scan source metrics: %w", err)
		}
		if source := sources[sourceID]; source != nil {
			source.ContentCount = contentCount
			if itemCount > 0 {
				source.DuplicateRate = math.Round(float64(duplicateCount)/float64(itemCount)*10_000) / 10_000
			}
			source.SuccessCount24h = successCount
			source.FailureCount24h = failureCount
			source.ErrorBudgetState = errorBudgetState(successCount, failureCount)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source metrics: %w", err)
	}
	return nil
}

func (store *Store) loadSourceValidations(
	ctx context.Context,
	sources map[string]*controlplane.ManagedSource,
) error {
	rows, err := store.pool.Query(ctx, `
		select distinct on (run.source_id)
			run.source_id,
			run.id::text,
			run.state,
			run.check_count,
			run.failed_check_count,
			run.explanation,
			run.completed_at
		from app.source_validation_runs run
		order by run.source_id, run.completed_at desc, run.id desc`)
	if err != nil {
		return fmt.Errorf("load source validations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID string
		validation := &controlplane.SourceValidation{}
		if err := rows.Scan(
			&sourceID,
			&validation.ID,
			&validation.State,
			&validation.CheckCount,
			&validation.FailedCheckCount,
			&validation.Explanation,
			&validation.CompletedAt,
		); err != nil {
			return fmt.Errorf("scan source validation: %w", err)
		}
		if source := sources[sourceID]; source != nil {
			source.LatestValidation = validation
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source validations: %w", err)
	}
	return nil
}

func (store *Store) UpdateSourcePreference(
	ctx context.Context,
	request controlplane.UpdateSourcePreferenceRequest,
	now time.Time,
) (controlplane.SourcePreference, error) {
	return retrySerializableValue(ctx, func() (controlplane.SourcePreference, error) {
		return store.updateSourcePreference(ctx, request, now)
	})
}

func (store *Store) updateSourcePreference(
	ctx context.Context,
	request controlplane.UpdateSourcePreferenceRequest,
	now time.Time,
) (controlplane.SourcePreference, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return controlplane.SourcePreference{}, fmt.Errorf("begin source preference update: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var sourceExists bool
	if err := transaction.QueryRow(ctx, `
		select exists(select 1 from app.sources where id = $1)`, request.SourceID).Scan(&sourceExists); err != nil {
		return controlplane.SourcePreference{}, fmt.Errorf("check source preference target: %w", err)
	}
	if !sourceExists {
		return controlplane.SourcePreference{}, controlplane.ErrNotFound
	}
	var currentVersion int64
	err = transaction.QueryRow(ctx, `
		select version
		from app.user_source_preferences
		where user_id = $1::uuid and source_id = $2
		for update`, request.UserID, request.SourceID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		currentVersion = 0
	} else if err != nil {
		return controlplane.SourcePreference{}, fmt.Errorf("lock source preference: %w", err)
	}
	if currentVersion != request.ExpectedVersion {
		return controlplane.SourcePreference{}, controlplane.ErrConflict
	}
	result := controlplane.SourcePreference{
		Muted: request.Muted, ExcludeFromDigest: request.ExcludeFromDigest,
		RelevanceAdjustment: request.RelevanceAdjustment, Version: currentVersion + 1,
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.user_source_preferences (
			user_id, source_id, muted, exclude_from_digest,
			relevance_adjustment, version, updated_at
		) values ($1::uuid, $2, $3, $4, $5, $6, $7)
		on conflict (user_id, source_id) do update set
			muted = excluded.muted,
			exclude_from_digest = excluded.exclude_from_digest,
			relevance_adjustment = excluded.relevance_adjustment,
			version = excluded.version,
			updated_at = excluded.updated_at`,
		request.UserID, request.SourceID, result.Muted, result.ExcludeFromDigest,
		result.RelevanceAdjustment, result.Version, now); err != nil {
		return controlplane.SourcePreference{}, fmt.Errorf("update source preference: %w", err)
	}
	if err := recordMutation(ctx, transaction, request.UserID, "source_preference_updated", "source", request.SourceID, map[string]any{
		"excludeFromDigest":   result.ExcludeFromDigest,
		"muted":               result.Muted,
		"relevanceAdjustment": result.RelevanceAdjustment,
		"version":             result.Version,
	}); err != nil {
		return controlplane.SourcePreference{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return controlplane.SourcePreference{}, fmt.Errorf("commit source preference update: %w", err)
	}
	return result, nil
}

func (store *Store) ActOnSource(
	ctx context.Context,
	request controlplane.SourceActionRequest,
	now time.Time,
) (controlplane.ManagedSource, error) {
	return retrySerializableValue(ctx, func() (controlplane.ManagedSource, error) {
		return store.actOnSource(ctx, request, now)
	})
}

func (store *Store) actOnSource(
	ctx context.Context,
	request controlplane.SourceActionRequest,
	now time.Time,
) (controlplane.ManagedSource, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return controlplane.ManagedSource{}, fmt.Errorf("begin source action: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var origin, state string
	if err := transaction.QueryRow(ctx, `
		select origin, validation_state
		from app.sources
		where id = $1
		for update`, request.SourceID).Scan(&origin, &state); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.ManagedSource{}, controlplane.ErrNotFound
	} else if err != nil {
		return controlplane.ManagedSource{}, fmt.Errorf("lock source action target: %w", err)
	}
	switch request.Action {
	case "test":
		if err := testSourceConfiguration(ctx, transaction, request, now); err != nil {
			return controlplane.ManagedSource{}, err
		}
	case "approve":
		if origin != "owner" || (state != "pending" && state != "degraded") {
			return controlplane.ManagedSource{}, controlplane.ErrConflict
		}
		var passed bool
		if err := transaction.QueryRow(ctx, `
			select exists (
				select 1
				from app.source_validation_runs
				where source_id = $1
					and state = 'passed'
					and completed_at >= $2::timestamptz - interval '30 minutes'
			)`, request.SourceID, now).Scan(&passed); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("check source validation: %w", err)
		}
		if !passed {
			return controlplane.ManagedSource{}, controlplane.ErrConflict
		}
		if _, err := transaction.Exec(ctx, `
			update app.sources
			set validation_state = 'active', reviewed_at = $2, updated_at = $2
			where id = $1`, request.SourceID, now); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("approve owner source: %w", err)
		}
	case "reject":
		if origin != "owner" {
			return controlplane.ManagedSource{}, controlplane.ErrConflict
		}
		if _, err := transaction.Exec(ctx, `
			update app.sources
			set validation_state = 'rejected', enabled = false, reviewed_at = $2, updated_at = $2
			where id = $1`, request.SourceID, now); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("reject owner source: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			insert into app.source_runtime_overrides (
				source_id, polling_enabled, reason, updated_by, updated_at
			) values ($1, false, $2, $3::uuid, $4)
			on conflict (source_id) do update set
				polling_enabled = false, reason = excluded.reason,
				updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
			request.SourceID, request.Reason, request.UserID, now); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("reject owner source: %w", err)
		}
	case "pause":
		if _, err := transaction.Exec(ctx, `
			insert into app.source_runtime_overrides (
				source_id, polling_enabled, reason, updated_by, updated_at
			) values ($1, false, $2, $3::uuid, $4)
			on conflict (source_id) do update set
				polling_enabled = false, reason = excluded.reason,
				updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
			request.SourceID, request.Reason, request.UserID, now); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("pause source polling: %w", err)
		}
	case "resume":
		if state == "pending" || state == "rejected" {
			return controlplane.ManagedSource{}, controlplane.ErrConflict
		}
		if _, err := transaction.Exec(ctx, `
			insert into app.source_runtime_overrides (
				source_id, polling_enabled, reason, updated_by, updated_at
			) values ($1, true, $2, $3::uuid, $4)
			on conflict (source_id) do update set
				polling_enabled = true, reason = excluded.reason,
				updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
			request.SourceID, request.Reason, request.UserID, now); err != nil {
			return controlplane.ManagedSource{}, fmt.Errorf("resume source polling: %w", err)
		}
	}
	if err := recordMutation(ctx, transaction, request.UserID, "source_"+request.Action, "source", request.SourceID, map[string]any{
		"reason": request.Reason,
	}); err != nil {
		return controlplane.ManagedSource{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return controlplane.ManagedSource{}, fmt.Errorf("commit source action: %w", err)
	}
	snapshot, err := store.Sources(ctx, request.UserID, now)
	if err != nil {
		return controlplane.ManagedSource{}, err
	}
	for _, source := range snapshot.Sources {
		if source.ID == request.SourceID {
			return source, nil
		}
	}
	return controlplane.ManagedSource{}, controlplane.ErrNotFound
}

func testSourceConfiguration(
	ctx context.Context,
	transaction pgx.Tx,
	request controlplane.SourceActionRequest,
	now time.Time,
) error {
	var endpointCount, failedChecks int
	if err := transaction.QueryRow(ctx, `
		select
			count(*)::integer,
			count(*) filter (where
				length(registry_id) = 0
				or length(url) < 9
				or cardinality(expected_content_types) = 0
				or max_response_bytes <= 0
				or length(fixture_suite) = 0
			)::integer
		from app.source_endpoints
		where source_id = $1`, request.SourceID).Scan(&endpointCount, &failedChecks); err != nil {
		return fmt.Errorf("test source configuration: %w", err)
	}
	checkCount := endpointCount * 5
	if checkCount == 0 {
		checkCount = 1
		failedChecks = 1
	}
	state := "passed"
	explanation := "Endpoint configuration, bounded-fetch policy, connector metadata, and fixture references passed without making a network request."
	if failedChecks > 0 {
		state = "failed"
		explanation = "One or more required endpoint configuration checks failed; polling remains unchanged."
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.source_validation_runs (
			source_id, requested_by, state, check_count,
			failed_check_count, explanation, started_at, completed_at
		) values ($1, $2::uuid, $3, $4, $5, $6, $7, $7)`,
		request.SourceID, request.UserID, state, checkCount, failedChecks, explanation, now); err != nil {
		return fmt.Errorf("record source validation: %w", err)
	}
	return nil
}

func errorBudgetState(successes int64, failures int64) string {
	total := successes + failures
	if total == 0 {
		return "unknown"
	}
	failureRate := float64(failures) / float64(total)
	switch {
	case failureRate <= 0.05:
		return "healthy"
	case failureRate <= 0.20:
		return "warning"
	default:
		return "exhausted"
	}
}
