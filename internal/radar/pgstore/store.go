package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/radar"
)

type Store struct {
	pool *pgxpool.Pool
	jobs *jobqueue.Inserter
}

func New(pool *pgxpool.Pool, jobs *jobqueue.Inserter) (*Store, error) {
	if pool == nil || jobs == nil {
		return nil, errors.New("Radar PostgreSQL store requires database and queue dependencies")
	}
	return &Store{pool: pool, jobs: jobs}, nil
}

func (store *Store) Snapshot(ctx context.Context, userID string, now time.Time) (radar.Snapshot, error) {
	snapshot := radar.Snapshot{
		GeneratedAt: now.UTC(),
		States:      []radar.State{radar.StateAdopt, radar.StateTrial, radar.StateAssess, radar.StateHold, radar.StateReject},
		Candidates:  make([]radar.Candidate, 0),
		Runs:        make([]radar.DiscoveryRun, 0),
	}
	rows, err := store.pool.Query(ctx, `
		select
			candidate.id::text,
			candidate.ecosystem,
			candidate.package_name,
			candidate.repository_url,
			candidate.discovered_at,
			candidate.discovery_source,
			candidate.current_status,
			candidate.incumbent_package,
			candidate.review_at,
			candidate.version,
			metric.id::text,
			metric.observed_at,
			metric.release_version,
			metric.license,
			metric.contributor_count,
			metric.runtime_compatibility,
			metric.types_supported,
			metric.bundle_size_bytes,
			metric.maintenance_metrics,
			metric.security_metrics,
			metric.popularity_metrics,
			metric.provenance_metrics,
			comparison.id::text,
			comparison.suggested_state,
			comparison.confidence::double precision,
			comparison.misleading,
			comparison.dimensions,
			comparison.evidence_links,
			comparison.assessed_at
		from app.package_candidates candidate
		left join lateral (
			select snapshot.*
			from app.package_metrics snapshot
			where snapshot.candidate_id = candidate.id
			order by snapshot.observed_at desc, snapshot.id desc
			limit 1
		) metric on true
		left join app.radar_comparisons comparison on comparison.metric_id = metric.id
		where candidate.user_id = $1::uuid
		order by
			case candidate.current_status
				when 'adopt' then 0 when 'trial' then 1 when 'assess' then 2
				when 'hold' then 3 else 4 end,
			candidate.review_at,
			candidate.package_name`, userID)
	if err != nil {
		return radar.Snapshot{}, fmt.Errorf("list Radar candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		candidate, scanErr := scanCandidate(rows)
		if scanErr != nil {
			return radar.Snapshot{}, scanErr
		}
		decisions, decisionErr := store.decisions(ctx, candidate.ID)
		if decisionErr != nil {
			return radar.Snapshot{}, decisionErr
		}
		candidate.Decisions = decisions
		if !candidate.ReviewAt.After(now) {
			snapshot.ReviewDue++
		}
		snapshot.Candidates = append(snapshot.Candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return radar.Snapshot{}, fmt.Errorf("iterate Radar candidates: %w", err)
	}
	runs, err := store.discoveryRuns(ctx, userID)
	if err != nil {
		return radar.Snapshot{}, err
	}
	snapshot.Runs = runs
	return snapshot, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanCandidate(row rowScanner) (radar.Candidate, error) {
	var candidate radar.Candidate
	var metricID, comparisonID *string
	var observedAt, assessedAt *time.Time
	var releaseVersion, license *string
	var contributorCount *int
	var runtimeCompatibility []string
	var typesSupported *bool
	var bundleSizeBytes *int64
	var maintenance, security, popularity, provenance []byte
	var suggestedState *radar.State
	var confidence *float64
	var misleading *bool
	var dimensions, evidenceLinks []byte
	if err := row.Scan(
		&candidate.ID, &candidate.Ecosystem, &candidate.PackageName, &candidate.RepositoryURL,
		&candidate.DiscoveredAt, &candidate.DiscoverySource, &candidate.CurrentState,
		&candidate.IncumbentPackage, &candidate.ReviewAt, &candidate.Version,
		&metricID, &observedAt, &releaseVersion, &license, &contributorCount,
		&runtimeCompatibility, &typesSupported, &bundleSizeBytes, &maintenance, &security,
		&popularity, &provenance, &comparisonID, &suggestedState, &confidence,
		&misleading, &dimensions, &evidenceLinks, &assessedAt,
	); err != nil {
		return radar.Candidate{}, fmt.Errorf("scan Radar candidate: %w", err)
	}
	if metricID != nil && observedAt != nil && releaseVersion != nil && license != nil && contributorCount != nil && typesSupported != nil {
		metric := &radar.MetricSnapshot{
			ID: *metricID, ObservedAt: observedAt.UTC(), ReleaseVersion: *releaseVersion,
			License: *license, ContributorCount: *contributorCount,
			RuntimeCompatibility: runtimeCompatibility, TypesSupported: *typesSupported,
			BundleSizeBytes: bundleSizeBytes,
		}
		if err := decodeObject(maintenance, &metric.Maintenance); err != nil {
			return radar.Candidate{}, err
		}
		if err := decodeObject(security, &metric.Security); err != nil {
			return radar.Candidate{}, err
		}
		if err := decodeObject(popularity, &metric.Popularity); err != nil {
			return radar.Candidate{}, err
		}
		if err := decodeObject(provenance, &metric.Provenance); err != nil {
			return radar.Candidate{}, err
		}
		candidate.LatestMetric = metric
	}
	if comparisonID != nil && suggestedState != nil && confidence != nil && misleading != nil && assessedAt != nil {
		comparison := &radar.Comparison{
			ID: *comparisonID, SuggestedState: *suggestedState, Confidence: *confidence,
			Misleading: *misleading, AssessedAt: assessedAt.UTC(),
		}
		if err := json.Unmarshal(dimensions, &comparison.Dimensions); err != nil {
			return radar.Candidate{}, fmt.Errorf("decode Radar comparison dimensions: %w", err)
		}
		if err := json.Unmarshal(evidenceLinks, &comparison.Evidence); err != nil {
			return radar.Candidate{}, fmt.Errorf("decode Radar comparison evidence: %w", err)
		}
		candidate.LatestComparison = comparison
	}
	return candidate, nil
}

func decodeObject(encoded []byte, target *map[string]any) error {
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode Radar metric object: %w", err)
	}
	return nil
}

func (store *Store) decisions(ctx context.Context, candidateID string) ([]radar.Decision, error) {
	rows, err := store.pool.Query(ctx, `
		select
			id::text, state, decision_source, rationale, evidence, decided_at, review_at,
			applicable_project_types, compatibility_requirements, exit_conditions
		from app.radar_decisions
		where candidate_id = $1::uuid
		order by decided_at desc, id desc
		limit 50`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("list Radar decision history: %w", err)
	}
	defer rows.Close()
	decisions := make([]radar.Decision, 0)
	for rows.Next() {
		var decision radar.Decision
		var evidence []byte
		if err := rows.Scan(
			&decision.ID, &decision.State, &decision.DecisionSource, &decision.Rationale,
			&evidence, &decision.DecidedAt, &decision.ReviewAt,
			&decision.ApplicableProjectTypes, &decision.CompatibilityRequirements,
			&decision.ExitConditions,
		); err != nil {
			return nil, fmt.Errorf("scan Radar decision history: %w", err)
		}
		if err := decodeObject(evidence, &decision.Evidence); err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Radar decision history: %w", err)
	}
	return decisions, nil
}

func (store *Store) discoveryRuns(ctx context.Context, userID string) ([]radar.DiscoveryRun, error) {
	rows, err := store.pool.Query(ctx, `
		select id::text, trigger_type, state, evidence_count, candidate_count,
			misleading_count, coalesce(error_code, ''), requested_at, started_at, completed_at
		from app.radar_discovery_runs
		where user_id = $1::uuid
		order by requested_at desc, id desc
		limit 20`, userID)
	if err != nil {
		return nil, fmt.Errorf("list Radar discovery runs: %w", err)
	}
	defer rows.Close()
	runs := make([]radar.DiscoveryRun, 0)
	for rows.Next() {
		var run radar.DiscoveryRun
		if err := rows.Scan(
			&run.ID, &run.TriggerType, &run.State, &run.EvidenceCount,
			&run.CandidateCount, &run.MisleadingCount, &run.ErrorCode,
			&run.RequestedAt, &run.StartedAt, &run.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan Radar discovery run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Radar discovery runs: %w", err)
	}
	return runs, nil
}

func (store *Store) QueueDiscovery(
	ctx context.Context,
	request radar.QueueDiscoveryRequest,
	now time.Time,
) (radar.DiscoveryRun, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return radar.DiscoveryRun{}, fmt.Errorf("begin Radar discovery request: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var run radar.DiscoveryRun
	var existingJobID *int64
	err = transaction.QueryRow(ctx, `
		insert into app.radar_discovery_runs (
			user_id, trigger_type, state, idempotency_key, requested_at
		) values ($1::uuid, $2, 'queued', $3, $4)
		on conflict (user_id, idempotency_key) do update
		set idempotency_key = excluded.idempotency_key
		returning id::text, trigger_type, state, evidence_count, candidate_count,
			misleading_count, coalesce(error_code, ''), requested_at, started_at, completed_at,
			river_job_id`,
		request.UserID, request.TriggerType, request.IdempotencyKey, now,
	).Scan(
		&run.ID, &run.TriggerType, &run.State, &run.EvidenceCount, &run.CandidateCount,
		&run.MisleadingCount, &run.ErrorCode, &run.RequestedAt, &run.StartedAt, &run.CompletedAt,
		&existingJobID,
	)
	if err != nil {
		return radar.DiscoveryRun{}, fmt.Errorf("insert Radar discovery run: %w", err)
	}
	if run.State == "queued" && existingJobID == nil {
		jobID, _, enqueueErr := store.jobs.EnqueueRadarDiscovery(ctx, transaction, jobqueue.RunWeeklyRadarDiscoveryArgs{
			RunID: run.ID, UserID: request.UserID,
		})
		if enqueueErr != nil {
			return radar.DiscoveryRun{}, enqueueErr
		}
		if _, err := transaction.Exec(ctx, `
			update app.radar_discovery_runs
			set river_job_id = coalesce(river_job_id, $2)
			where id = $1::uuid`, run.ID, jobID); err != nil {
			return radar.DiscoveryRun{}, fmt.Errorf("link Radar discovery job: %w", err)
		}
		metadata := map[string]any{"triggerType": request.TriggerType}
		if request.TriggerType == "owner" {
			if err := recordMutation(ctx, transaction, request.UserID, "radar_discovery_requested", "radar_discovery_run", run.ID, metadata); err != nil {
				return radar.DiscoveryRun{}, err
			}
		} else if err := recordSystemMutation(ctx, transaction, request.UserID, "radar_discovery_scheduled", "radar_discovery_run", run.ID, metadata); err != nil {
			return radar.DiscoveryRun{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return radar.DiscoveryRun{}, fmt.Errorf("commit Radar discovery request: %w", err)
	}
	return run, nil
}

func (store *Store) Decide(
	ctx context.Context,
	request radar.DecisionRequest,
	now time.Time,
) (radar.Candidate, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return radar.Candidate{}, fmt.Errorf("begin Radar decision: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var currentVersion int64
	if err := transaction.QueryRow(ctx, `
		select version
		from app.package_candidates
		where id = $1::uuid and user_id = $2::uuid
		for update`, request.CandidateID, request.UserID).Scan(&currentVersion); errors.Is(err, pgx.ErrNoRows) {
		return radar.Candidate{}, radar.ErrNotFound
	} else if err != nil {
		return radar.Candidate{}, fmt.Errorf("lock Radar candidate: %w", err)
	}
	if currentVersion != request.ExpectedVersion {
		return radar.Candidate{}, radar.ErrConflict
	}
	evidence, err := json.Marshal(request.Evidence)
	if err != nil {
		return radar.Candidate{}, fmt.Errorf("encode Radar decision evidence: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.radar_decisions (
			candidate_id, owner_user_id, state, decision_source, rationale, evidence,
			decided_at, review_at, applicable_project_types,
			compatibility_requirements, exit_conditions
		) values ($1::uuid, $2::uuid, $3, 'owner', $4, $5::jsonb, $6, $7, $8, $9, $10)`,
		request.CandidateID, request.UserID, request.State, request.Rationale, evidence,
		now, request.ReviewAt.UTC(), request.ApplicableProjectTypes,
		request.CompatibilityRequirements, request.ExitConditions,
	); err != nil {
		return radar.Candidate{}, fmt.Errorf("insert owner Radar decision: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.package_candidates
		set current_status = $3, review_at = $4, version = version + 1, updated_at = $5
		where id = $1::uuid and user_id = $2::uuid`,
		request.CandidateID, request.UserID, request.State, request.ReviewAt.UTC(), now,
	); err != nil {
		return radar.Candidate{}, fmt.Errorf("update Radar candidate state: %w", err)
	}
	if err := recordMutation(ctx, transaction, request.UserID, "radar_decision_recorded", "package_candidate", request.CandidateID, map[string]any{
		"state": request.State, "version": currentVersion + 1, "reviewAt": request.ReviewAt.UTC(),
	}); err != nil {
		return radar.Candidate{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return radar.Candidate{}, fmt.Errorf("commit Radar decision: %w", err)
	}
	snapshot, err := store.Snapshot(ctx, request.UserID, now)
	if err != nil {
		return radar.Candidate{}, err
	}
	for _, candidate := range snapshot.Candidates {
		if candidate.ID == request.CandidateID {
			return candidate, nil
		}
	}
	return radar.Candidate{}, radar.ErrNotFound
}

func recordMutation(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	action string,
	targetType string,
	targetID string,
	metadata map[string]any,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode Radar mutation metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.audit_events (
			actor_type, actor_id, action, target_type, target_id, metadata
		) values ('owner', $1, $2, $3, $4, $5::jsonb)`,
		userID, action, targetType, targetID, encoded); err != nil {
		return fmt.Errorf("record Radar audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.outbox_events (
			event_type, aggregate_type, aggregate_id, payload
		) values ($1, $2, $3::uuid, $4::jsonb)`,
		action, targetType, userID, encoded); err != nil {
		return fmt.Errorf("record Radar outbox event: %w", err)
	}
	return nil
}

func recordSystemMutation(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	action string,
	targetType string,
	targetID string,
	metadata map[string]any,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode Radar system mutation metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.audit_events (
			actor_type, actor_id, action, target_type, target_id, metadata
		) values ('system', 'weekly-radar-schedule', $1, $2, $3, $4::jsonb)`,
		action, targetType, targetID, encoded); err != nil {
		return fmt.Errorf("record Radar system audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.outbox_events (
			event_type, aggregate_type, aggregate_id, payload
		) values ($1, $2, $3::uuid, $4::jsonb)`,
		action, targetType, userID, encoded); err != nil {
		return fmt.Errorf("record Radar system outbox event: %w", err)
	}
	return nil
}

func joinReasons(reasons []string) string {
	return strings.Join(reasons, " ")
}
