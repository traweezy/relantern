package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/radar"
)

func (store *Store) ProcessDiscovery(
	ctx context.Context,
	runID string,
	userID string,
	now time.Time,
) (radar.DiscoveryResult, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return radar.DiscoveryResult{}, fmt.Errorf("begin Radar discovery: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var state string
	if err := transaction.QueryRow(ctx, `
		select state
		from app.radar_discovery_runs
		where id = $1::uuid and user_id = $2::uuid
		for update`, runID, userID).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return radar.DiscoveryResult{}, radar.ErrNotFound
	} else if err != nil {
		return radar.DiscoveryResult{}, fmt.Errorf("lock Radar discovery run: %w", err)
	}
	if state == "completed" {
		var result radar.DiscoveryResult
		if err := transaction.QueryRow(ctx, `
			select evidence_count, candidate_count, misleading_count
			from app.radar_discovery_runs
			where id = $1::uuid`, runID).Scan(
			&result.EvidenceCount, &result.CandidateCount, &result.MisleadingCount,
		); err != nil {
			return radar.DiscoveryResult{}, fmt.Errorf("load completed Radar run: %w", err)
		}
		return result, transaction.Commit(ctx)
	}
	if _, err := transaction.Exec(ctx, `
		update app.radar_discovery_runs
		set state = 'running', started_at = coalesce(started_at, $2), error_code = null
		where id = $1::uuid`, runID, now); err != nil {
		return radar.DiscoveryResult{}, fmt.Errorf("start Radar discovery run: %w", err)
	}
	evidence, err := pendingEvidence(ctx, transaction, userID)
	if err != nil {
		return radar.DiscoveryResult{}, err
	}
	result := radar.DiscoveryResult{EvidenceCount: len(evidence)}
	for _, candidateEvidence := range evidence {
		assessment := radar.Assess(candidateEvidence)
		created, processErr := processEvidence(ctx, transaction, userID, candidateEvidence, assessment, now)
		if processErr != nil {
			return radar.DiscoveryResult{}, processErr
		}
		if created {
			result.CandidateCount++
		}
		if assessment.Misleading {
			result.MisleadingCount++
		}
	}
	if _, err := transaction.Exec(ctx, `
		update app.radar_discovery_runs
		set state = 'completed', evidence_count = $2, candidate_count = $3,
			misleading_count = $4, completed_at = $5
		where id = $1::uuid`, runID, result.EvidenceCount, result.CandidateCount,
		result.MisleadingCount, now); err != nil {
		return radar.DiscoveryResult{}, fmt.Errorf("complete Radar discovery run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return radar.DiscoveryResult{}, fmt.Errorf("commit Radar discovery run: %w", err)
	}
	return result, nil
}

func (store *Store) RefreshCandidate(
	ctx context.Context,
	candidateID string,
	userID string,
	now time.Time,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin Radar candidate refresh: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var exists bool
	if err := transaction.QueryRow(ctx, `
		select exists(
			select 1 from app.package_candidates
			where id = $1::uuid and user_id = $2::uuid
		)`, candidateID, userID).Scan(&exists); err != nil {
		return fmt.Errorf("check Radar refresh target: %w", err)
	}
	if !exists {
		return radar.ErrNotFound
	}
	rows, err := transaction.Query(ctx, `
		select `+evidenceColumns+`
		from app.package_candidate_evidence evidence
		join app.package_candidates candidate
			on candidate.user_id = evidence.user_id
			and candidate.ecosystem = evidence.ecosystem
			and candidate.package_name = evidence.package_name
		where candidate.id = $1::uuid and evidence.processed_at is null
		order by evidence.observed_at, evidence.id
		for update of evidence skip locked
		limit 20`, candidateID)
	if err != nil {
		return fmt.Errorf("load Radar refresh evidence: %w", err)
	}
	evidence, err := scanEvidenceRows(rows)
	rows.Close()
	if err != nil {
		return err
	}
	for _, value := range evidence {
		if _, err := processEvidence(ctx, transaction, userID, value, radar.Assess(value), now); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Radar candidate refresh: %w", err)
	}
	return nil
}

const evidenceColumns = `
		evidence.id::text, evidence.ecosystem, evidence.package_name,
		evidence.repository_url, evidence.discovery_source, evidence.incumbent_package,
		evidence.stable_release, evidence.license, evidence.contributor_count,
		evidence.release_cadence_days, evidence.issue_response_days,
		evidence.security_response_days, evidence.security_advisory_count,
		evidence.critical_advisory_count, evidence.scorecard_score::double precision,
		evidence.signed_releases, evidence.provenance_verified, evidence.types_supported,
		evidence.bundle_size_bytes, evidence.runtime_compatibility, evidence.project_types,
		evidence.compatibility_requirements, evidence.exit_conditions,
		evidence.maintenance_signals, evidence.security_signals,
		evidence.popularity_signals, evidence.evidence_links, evidence.observed_at`

func pendingEvidence(ctx context.Context, transaction pgx.Tx, userID string) ([]radar.CandidateEvidence, error) {
	rows, err := transaction.Query(ctx, `
		select `+evidenceColumns+`
		from app.package_candidate_evidence evidence
		where evidence.user_id = $1::uuid and evidence.processed_at is null
		order by evidence.observed_at, evidence.id
		for update skip locked
		limit 200`, userID)
	if err != nil {
		return nil, fmt.Errorf("load pending Radar evidence: %w", err)
	}
	defer rows.Close()
	return scanEvidenceRows(rows)
}

func scanEvidenceRows(rows pgx.Rows) ([]radar.CandidateEvidence, error) {
	result := make([]radar.CandidateEvidence, 0)
	for rows.Next() {
		var evidence radar.CandidateEvidence
		var maintenance, security, popularity, links []byte
		if err := rows.Scan(
			&evidence.ID, &evidence.Ecosystem, &evidence.PackageName,
			&evidence.RepositoryURL, &evidence.DiscoverySource, &evidence.IncumbentPackage,
			&evidence.StableRelease, &evidence.License, &evidence.ContributorCount,
			&evidence.ReleaseCadenceDays, &evidence.IssueResponseDays,
			&evidence.SecurityResponseDays, &evidence.SecurityAdvisoryCount,
			&evidence.CriticalAdvisoryCount, &evidence.ScorecardScore,
			&evidence.SignedReleases, &evidence.ProvenanceVerified, &evidence.TypesSupported,
			&evidence.BundleSizeBytes, &evidence.RuntimeCompatibility, &evidence.ProjectTypes,
			&evidence.CompatibilityRequirements, &evidence.ExitConditions,
			&maintenance, &security, &popularity, &links, &evidence.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan Radar candidate evidence: %w", err)
		}
		if err := decodeObject(maintenance, &evidence.MaintenanceSignals); err != nil {
			return nil, err
		}
		if err := decodeObject(security, &evidence.SecuritySignals); err != nil {
			return nil, err
		}
		if err := decodeObject(popularity, &evidence.PopularitySignals); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(links, &evidence.Links); err != nil {
			return nil, fmt.Errorf("decode Radar evidence links: %w", err)
		}
		result = append(result, evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Radar candidate evidence: %w", err)
	}
	return result, nil
}

func processEvidence(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	evidence radar.CandidateEvidence,
	assessment radar.Assessment,
	now time.Time,
) (bool, error) {
	var candidateID string
	var inserted bool
	if err := transaction.QueryRow(ctx, `
		insert into app.package_candidates (
			user_id, ecosystem, package_name, repository_url, discovered_at,
			discovery_source, current_status, incumbent_package, review_at
		) values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (user_id, ecosystem, package_name) do update set
			repository_url = excluded.repository_url,
			discovery_source = excluded.discovery_source,
			incumbent_package = excluded.incumbent_package,
			updated_at = $5
		returning id::text, (xmax = 0)`,
		userID, evidence.Ecosystem, evidence.PackageName, evidence.RepositoryURL,
		now, evidence.DiscoverySource, assessment.SuggestedState, evidence.IncumbentPackage,
		now.Add(30*24*time.Hour),
	).Scan(&candidateID, &inserted); err != nil {
		return false, fmt.Errorf("upsert Radar candidate: %w", err)
	}
	maintenance := mergeSignals(evidence.MaintenanceSignals, map[string]any{
		"releaseCadenceDays": evidence.ReleaseCadenceDays,
		"issueResponseDays":  evidence.IssueResponseDays,
	})
	security := mergeSignals(evidence.SecuritySignals, map[string]any{
		"advisoryCount":         evidence.SecurityAdvisoryCount,
		"criticalAdvisoryCount": evidence.CriticalAdvisoryCount,
		"securityResponseDays":  evidence.SecurityResponseDays,
		"scorecardScore":        evidence.ScorecardScore,
	})
	provenance := map[string]any{
		"signedReleases": evidence.SignedReleases,
		"verified":       evidence.ProvenanceVerified,
	}
	maintenanceJSON, _ := json.Marshal(maintenance)
	securityJSON, _ := json.Marshal(security)
	popularityJSON, _ := json.Marshal(evidence.PopularitySignals)
	provenanceJSON, _ := json.Marshal(provenance)
	var metricID string
	if err := transaction.QueryRow(ctx, `
		insert into app.package_metrics (
			candidate_id, evidence_id, observed_at, release_version, license,
			contributor_count, runtime_compatibility, types_supported,
			bundle_size_bytes, maintenance_metrics, security_metrics,
			popularity_metrics, provenance_metrics
		) values ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9,
			$10::jsonb, $11::jsonb, $12::jsonb, $13::jsonb)
		on conflict (candidate_id, evidence_id) do update
		set observed_at = excluded.observed_at
		returning id::text`,
		candidateID, evidence.ID, evidence.ObservedAt, evidence.StableRelease, evidence.License,
		evidence.ContributorCount, evidence.RuntimeCompatibility, evidence.TypesSupported,
		evidence.BundleSizeBytes, maintenanceJSON, securityJSON, popularityJSON, provenanceJSON,
	).Scan(&metricID); err != nil {
		return false, fmt.Errorf("insert Radar metric snapshot: %w", err)
	}
	dimensions, err := json.Marshal(assessment.Dimensions)
	if err != nil {
		return false, fmt.Errorf("encode Radar comparison: %w", err)
	}
	links, err := json.Marshal(evidence.Links)
	if err != nil {
		return false, fmt.Errorf("encode Radar evidence links: %w", err)
	}
	var comparisonID string
	if err := transaction.QueryRow(ctx, `
		insert into app.radar_comparisons (
			candidate_id, metric_id, suggested_state, confidence, misleading,
			dimensions, evidence_links, assessed_at
		) values ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb, $7::jsonb, $8)
		on conflict (metric_id) do update
		set assessed_at = excluded.assessed_at
		returning id::text`,
		candidateID, metricID, assessment.SuggestedState, assessment.Confidence,
		assessment.Misleading, dimensions, links, now,
	).Scan(&comparisonID); err != nil {
		return false, fmt.Errorf("insert Radar comparison: %w", err)
	}
	var decisionCount int
	if err := transaction.QueryRow(ctx, `
		select count(*) from app.radar_decisions where candidate_id = $1::uuid`, candidateID).Scan(&decisionCount); err != nil {
		return false, fmt.Errorf("count Radar decisions: %w", err)
	}
	if decisionCount == 0 {
		decisionEvidence, marshalErr := json.Marshal(map[string]any{
			"comparisonId":  comparisonID,
			"evidenceLinks": evidence.Links,
			"reasons":       assessment.Reasons,
		})
		if marshalErr != nil {
			return false, fmt.Errorf("encode initial Radar decision evidence: %w", marshalErr)
		}
		if _, err := transaction.Exec(ctx, `
			insert into app.radar_decisions (
				candidate_id, owner_user_id, state, decision_source, rationale, evidence,
				decided_at, review_at, applicable_project_types,
				compatibility_requirements, exit_conditions
			) values ($1::uuid, $2::uuid, $3, 'system', $4, $5::jsonb, $6, $7, $8, $9, $10)`,
			candidateID, userID, assessment.SuggestedState, joinReasons(assessment.Reasons),
			decisionEvidence, now, now.Add(30*24*time.Hour), evidence.ProjectTypes,
			evidence.CompatibilityRequirements, evidence.ExitConditions,
		); err != nil {
			return false, fmt.Errorf("insert initial Radar assessment: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			update app.package_candidates
			set current_status = $2, review_at = $3, updated_at = $4
			where id = $1::uuid`, candidateID, assessment.SuggestedState,
			now.Add(30*24*time.Hour), now); err != nil {
			return false, fmt.Errorf("apply initial Radar assessment: %w", err)
		}
	}
	if _, err := transaction.Exec(ctx, `
		update app.package_candidate_evidence
		set processed_at = $2
		where id = $1::uuid`, evidence.ID, now); err != nil {
		return false, fmt.Errorf("mark Radar evidence processed: %w", err)
	}
	return inserted, nil
}

func mergeSignals(original map[string]any, additions map[string]any) map[string]any {
	merged := make(map[string]any, len(original)+len(additions))
	for key, value := range original {
		merged[key] = value
	}
	for key, value := range additions {
		merged[key] = value
	}
	return merged
}
