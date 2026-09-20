package pgstore

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/research"
)

const (
	// A recent, signal-prioritized slice bounds each schedule's planning query.
	// A busy day with more than 500 high-value stories may defer older stories.
	researchCandidateScanLimit = 500
	researchCompletionGrace    = 10 * time.Minute
)

// QueuedResearch identifies a job authorized before the digest cutoff. Only
// that exact River job may start during the ten-minute completion window.
type QueuedResearch struct {
	ClusterID   string
	RevisionID  string
	InputSHA256 string
}

type researchSchedule struct {
	userID            string
	cutoff            time.Time
	windowStart       time.Time
	minimumScore      float64
	maximumItems      int
	includeComingSoon bool
	includeLater      bool
}

// ResearchAdmissions uses the same deterministic digest scorer and bounded
// category allocation as delivery. It is shared by both enqueue paths and the
// final provider-work check, so neither replay nor a delayed job can spend on
// an ineligible story.
func ResearchAdmissions(
	ctx context.Context, tx pgx.Tx, now time.Time, queued *QueuedResearch,
) (map[string]struct{}, error) {
	schedules, err := activeResearchSchedules(ctx, tx, now.UTC(), queued)
	if err != nil {
		return nil, err
	}
	admitted := make(map[string]struct{})
	for _, schedule := range schedules {
		candidates, err := researchCandidates(ctx, tx, schedule)
		if err != nil {
			return nil, err
		}
		selected, err := selectIncompleteResearch(ctx, tx, candidates, schedule)
		if err != nil {
			return nil, err
		}
		for _, candidate := range selected {
			admitted[candidate.ID] = struct{}{}
		}
	}
	return admitted, nil
}

func selectIncompleteResearch(
	ctx context.Context, tx pgx.Tx, candidates []digest.DigestCandidate, schedule researchSchedule,
) ([]digest.DigestCandidate, error) {
	return rankIncompleteResearch(candidates, schedule.maximumItems, schedule.minimumScore,
		func(clusterID string) (bool, error) {
			currentRevisionID, inputSHA256, err := currentInputSHA256(ctx, tx, clusterID, false)
			if err != nil {
				return false, fmt.Errorf("inspect ranked research facts: %w", err)
			}
			var hasCurrentBrief bool
			if err := tx.QueryRow(ctx, `
				select exists (
					select 1 from app.ai_runs run
					join app.research_briefs brief on brief.ai_run_id = run.id
					where run.cluster_id = $1::uuid and run.revision_id = $2::uuid
						and run.purpose = $3 and run.input_sha256 = decode($4, 'hex')
						and run.state in ('completed', 'needs_review')
				)`, clusterID, currentRevisionID, research.Purpose,
				inputSHA256).Scan(&hasCurrentBrief); err != nil {
				return false, fmt.Errorf("inspect current research brief: %w", err)
			}
			return hasCurrentBrief, nil
		})
}

func rankIncompleteResearch(
	candidates []digest.DigestCandidate, maximumItems int, minimumScore float64,
	isComplete func(string) (bool, error),
) ([]digest.DigestCandidate, error) {
	// Re-rank only after a selected candidate proves complete. This normally
	// hashes at most maximum_items stories, but can examine the bounded 500-story
	// slice when most higher-ranked stories already have current briefs.
	completed := make(map[string]bool)
	incomplete := make(map[string]bool)
	for len(candidates) > 0 {
		selected := digest.Select(candidates, maximumItems, minimumScore)
		if len(selected) == 0 {
			return nil, nil
		}
		removed := false
		for _, candidate := range selected {
			if incomplete[candidate.ID] {
				continue
			}
			hasCurrentBrief, err := isComplete(candidate.ID)
			if err != nil {
				return nil, err
			}
			if hasCurrentBrief {
				completed[candidate.ID] = true
				removed = true
			} else {
				incomplete[candidate.ID] = true
			}
		}
		if !removed {
			return selected, nil
		}
		candidates = slices.DeleteFunc(candidates, func(candidate digest.DigestCandidate) bool {
			return completed[candidate.ID]
		})
	}
	return nil, nil
}

func activeResearchSchedules(ctx context.Context, tx pgx.Tx, now time.Time, queued *QueuedResearch) ([]researchSchedule, error) {
	rows, err := tx.Query(ctx, `
		select schedule.user_id::text, schedule.next_due_at,
			schedule.minimum_score::double precision, schedule.maximum_items,
			schedule.include_coming_soon, schedule.include_later_reminders,
			coalesce((
				select max(digest.window_end) from app.digests digest
				where digest.user_id = schedule.user_id
					and digest.channel = 'dashboard' and digest.state = 'delivered'
					and digest.window_end < schedule.next_due_at - interval '15 minutes'
			), schedule.next_due_at - interval '1 day 15 minutes')
		from app.schedule_definitions schedule
		where schedule.schedule_type = 'daily_digest' and schedule.enabled
			and schedule.paused_at is null
			and (schedule.skip_next_at is null or schedule.skip_next_at <> schedule.next_due_at)
		order by schedule.user_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("select research admission schedules: %w", err)
	}
	defer rows.Close()
	schedules := make([]researchSchedule, 0)
	for rows.Next() {
		var schedule researchSchedule
		var dueAt time.Time
		if err := rows.Scan(&schedule.userID, &dueAt, &schedule.minimumScore,
			&schedule.maximumItems, &schedule.includeComingSoon,
			&schedule.includeLater, &schedule.windowStart); err != nil {
			return nil, fmt.Errorf("scan research admission schedule: %w", err)
		}
		schedule.cutoff = dueAt.UTC().Add(-15 * time.Minute)
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate research admission schedules: %w", err)
	}
	rows.Close()
	active := make([]researchSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		if !now.Before(schedule.cutoff) {
			if queued == nil || now.After(schedule.cutoff.Add(researchCompletionGrace)) {
				continue
			}
			var authorized bool
			if err := tx.QueryRow(ctx, `
				select exists (
					select 1 from river.river_job job
					where job.kind = $1
						and job.args @> jsonb_build_object(
							'clusterId', $2::text, 'revisionId', $3::text,
							'inputSha256', $4::text)
						and job.created_at < $5
				)`, jobqueue.ResearchStoryKind, queued.ClusterID,
				queued.RevisionID, queued.InputSHA256, schedule.cutoff).Scan(&authorized); err != nil {
				return nil, fmt.Errorf("check pre-cutoff research authorization: %w", err)
			}
			if !authorized {
				continue
			}
		}
		active = append(active, schedule)
	}
	return active, nil
}

func researchCandidates(ctx context.Context, tx pgx.Tx, schedule researchSchedule) ([]digest.DigestCandidate, error) {
	rows, err := tx.Query(ctx, `
		select cluster.id::text, cluster.last_changed_at, item.status,
			source.source_tier, item.event_type,
			coalesce(extraction.lifecycle_state, 'unknown'),
			case when facts.high_confidence then 'high'
				when facts.medium_confidence then 'medium'
				when facts.low_confidence then 'low' else 'unknown' end,
			facts.security, facts.breaking_change, facts.deprecation
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		join app.item_sources source on source.item_id = item.id
			and source.revision_id = item.current_revision_id
			and source.source_role = 'primary' and source.source_tier in ('T0', 'T1')
		join app.content_revisions revision on revision.id = item.current_revision_id
		join app.raw_documents document on document.id = revision.raw_document_id
		left join app.user_source_preferences preference
			on preference.user_id = $1::uuid and preference.source_id = document.source_id
		left join app.user_item_states reading
			on reading.user_id = $1::uuid and reading.item_id = item.id
		left join lateral (
			select run.validated_output->>'lifecycle_state' as lifecycle_state
			from app.ai_runs run
			where run.revision_id = item.current_revision_id
				and run.purpose = 'structured_extraction'
				and run.state in ('completed', 'needs_review')
			order by run.completed_at desc nulls last, run.id desc
			limit 1
		) extraction on true
		join lateral (
			select count(*) > 0 as verified,
				coalesce(bool_or(claim.claim_type = 'security'), false) as security,
				coalesce(bool_or(claim.claim_type = 'breaking_change'), false) as breaking_change,
				coalesce(bool_or(claim.claim_type = 'deprecation'), false) as deprecation,
				coalesce(bool_or(claim.confidence = 'high'), false) as high_confidence,
				coalesce(bool_or(claim.confidence = 'medium'), false) as medium_confidence,
				coalesce(bool_or(claim.confidence = 'low'), false) as low_confidence
			from app.cluster_members member
			join app.items evidence_item on evidence_item.id = member.item_id
			join app.item_sources evidence_source
				on evidence_source.item_id = evidence_item.id
				and evidence_source.revision_id = evidence_item.current_revision_id
				and evidence_source.source_tier in ('T0', 'T1')
			join app.claims claim on claim.item_id = evidence_item.id
				and claim.revision_id = evidence_item.current_revision_id
				and claim.verification_state = 'verified_span'
			where member.cluster_id = cluster.id
		) facts on facts.verified
		where item.lifecycle_state in ('ready', 'published') and item.status <> 'suppressed'
			and cluster.last_changed_at >= $2 and cluster.last_changed_at < $3
			and coalesce(reading.location, 'inbox') <> 'archive'
			and ($5 or coalesce(reading.location, 'inbox') <> 'later')
			and (reading.snoozed_until is null or reading.snoozed_until <= $3)
			and not coalesce(preference.muted, false)
			and not coalesce(preference.exclude_from_digest, false)
			and (facts.security or facts.breaking_change or facts.deprecation
				or item.event_type in ('security', 'breaking_change', 'deprecation',
					'release', 'migration', 'preview', 'proposal'))
		order by case
			when facts.security or item.event_type = 'security' then 0
			when facts.breaking_change or item.event_type = 'breaking_change' then 1
			when facts.deprecation or item.event_type = 'deprecation' then 2
			when item.event_type in ('release', 'migration') then 3
			else 4 end,
			cluster.last_changed_at desc, cluster.id
		limit $4`, schedule.userID, schedule.windowStart, schedule.cutoff,
		researchCandidateScanLimit, schedule.includeLater)
	if err != nil {
		return nil, fmt.Errorf("select high-value research candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]digest.DigestCandidate, 0)
	for rows.Next() {
		var candidate digest.DigestCandidate
		var status, sourceTier, eventType, lifecycle, confidence string
		var security, breakingChange, deprecation bool
		if err := rows.Scan(&candidate.ID, &candidate.Observed, &status, &sourceTier,
			&eventType, &lifecycle, &confidence, &security, &breakingChange,
			&deprecation); err != nil {
			return nil, fmt.Errorf("scan high-value research candidate: %w", err)
		}
		signal := researchSignal(eventType, security, breakingChange, deprecation)
		if !schedule.includeComingSoon && (signal == "proposal" || lifecycle == "preview" || lifecycle == "release_candidate") {
			continue
		}
		candidate.Type = "story"
		candidate.Score, candidate.Category, candidate.Reason = digest.ScoreStory(digest.StorySignals{
			Confidence: confidence, LifecycleState: lifecycle, Signal: signal,
			SourceTier: sourceTier, Status: status, ObservedAt: candidate.Observed,
			WindowStart: schedule.windowStart, WindowEnd: schedule.cutoff,
		})
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate high-value research candidates: %w", err)
	}
	return candidates, nil
}

func researchSignal(eventType string, security, breakingChange, deprecation bool) string {
	switch {
	case security || eventType == "security":
		return "security"
	case breakingChange || eventType == "breaking_change":
		return "breaking-change"
	case deprecation || eventType == "deprecation":
		return "deprecation"
	case eventType == "release":
		return "release"
	case eventType == "migration":
		return "migration"
	case eventType == "proposal" || eventType == "preview":
		return "proposal"
	default:
		return "general"
	}
}
