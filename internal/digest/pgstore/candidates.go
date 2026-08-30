package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
)

func (store *Store) captureCandidates(
	ctx context.Context,
	transaction pgx.Tx,
	settings occurrenceSettings,
	windowStart time.Time,
	windowEnd time.Time,
) ([]digest.DigestCandidate, error) {
	stories, err := storyCandidates(ctx, transaction, settings, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	if !settings.IncludeRadarCandidates {
		return stories, nil
	}
	radarCandidates, err := radarCandidates(ctx, transaction, settings.UserID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	return append(stories, radarCandidates...), nil
}

func storyCandidates(
	ctx context.Context,
	transaction pgx.Tx,
	settings occurrenceSettings,
	windowStart time.Time,
	windowEnd time.Time,
) ([]digest.DigestCandidate, error) {
	rows, err := transaction.Query(ctx, `
		select
			story.story_id::text, story.item_id::text, story.headline, story.summary,
			story.recommended_action, story.confidence, story.last_changed_at,
			story.status, story.signal, story.source_tier, item.canonical_url,
			coalesce(extraction.lifecycle_state, 'unknown'), document.source_id,
			coalesce(reading.location, 'inbox')
		from app.v_story_summaries story
		join app.items item on item.id = story.item_id
		join app.content_revisions revision on revision.id = item.current_revision_id
		join app.raw_documents document on document.id = revision.raw_document_id
		left join app.user_item_states reading
			on reading.user_id = $1::uuid and reading.item_id = item.id
		left join app.user_source_preferences preference
			on preference.user_id = $1::uuid and preference.source_id = document.source_id
		left join lateral (
			select run.validated_output->>'lifecycle_state' as lifecycle_state
			from app.ai_runs run
			where run.revision_id = item.current_revision_id
				and run.purpose = 'structured_extraction'
				and run.state in ('completed', 'needs_review')
			order by run.completed_at desc nulls last, run.id desc
			limit 1
		) extraction on true
		where story.last_changed_at >= $2 and story.last_changed_at < $3
			and item.status <> 'suppressed'
			and coalesce(reading.location, 'inbox') <> 'archive'
			and (reading.snoozed_until is null or reading.snoozed_until <= $3)
			and ($4 or coalesce(reading.location, 'inbox') <> 'later')
			and not coalesce(preference.muted, false)
			and not coalesce(preference.exclude_from_digest, false)
			and ($5 or coalesce(extraction.lifecycle_state, 'unknown') not in ('preview', 'release_candidate'))
		order by story.last_changed_at desc, story.story_id`,
		settings.UserID, windowStart.UTC(), windowEnd.UTC(),
		settings.IncludeLaterReminders, settings.IncludeComingSoon)
	if err != nil {
		return nil, fmt.Errorf("select story digest candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]digest.DigestCandidate, 0)
	for rows.Next() {
		var candidate digest.DigestCandidate
		var confidence, status, sourceTier, lifecycle, sourceID, location string
		if err := rows.Scan(
			&candidate.ID, &candidate.ItemID, &candidate.Headline, &candidate.Summary,
			&candidate.Action, &confidence, &candidate.Observed, &status, &candidate.Signal,
			&sourceTier, &candidate.SourceURL, &lifecycle, &sourceID, &location,
		); err != nil {
			return nil, fmt.Errorf("scan story digest candidate: %w", err)
		}
		candidate.Type = "story"
		candidate.StoryID = candidate.ID
		candidate.AppPath = "/story/" + candidate.ID
		candidate.Score, candidate.Category, candidate.Reason = digest.ScoreStory(digest.StorySignals{
			Confidence: confidence, LifecycleState: lifecycle, Signal: candidate.Signal,
			SourceTier: sourceTier, Status: status, ObservedAt: candidate.Observed,
			WindowStart: windowStart, WindowEnd: windowEnd,
		})
		if location == "later" {
			candidate.Category = "later"
			candidate.Reason = "An owner-saved Later item is due for a bounded reminder."
		}
		candidate.Observed = candidate.Observed.UTC()
		candidate.Evidence = map[string]any{
			"confidence": confidence, "lifecycleState": lifecycle, "sourceId": sourceID,
			"sourceTier": sourceTier, "status": status,
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate story digest candidates: %w", err)
	}
	return candidates, nil
}

func radarCandidates(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
	windowStart time.Time,
	windowEnd time.Time,
) ([]digest.DigestCandidate, error) {
	rows, err := transaction.Query(ctx, `
		select candidate.id::text, candidate.package_name, candidate.incumbent_package,
			candidate.repository_url, candidate.current_status, candidate.ecosystem,
			comparison.misleading, comparison.assessed_at, comparison.confidence::double precision
		from app.package_candidates candidate
		join lateral (
			select current_comparison.*
			from app.radar_comparisons current_comparison
			where current_comparison.candidate_id = candidate.id
			order by current_comparison.assessed_at desc, current_comparison.id desc
			limit 1
		) comparison on true
		where candidate.user_id = $1::uuid and candidate.current_status <> 'reject'
			and comparison.assessed_at >= $2 and comparison.assessed_at < $3
		order by comparison.assessed_at desc, candidate.id`, userID, windowStart.UTC(), windowEnd.UTC())
	if err != nil {
		return nil, fmt.Errorf("select Radar digest candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]digest.DigestCandidate, 0)
	for rows.Next() {
		var candidate digest.DigestCandidate
		var packageName, incumbent, state, ecosystem string
		var misleading bool
		var confidence float64
		if err := rows.Scan(
			&candidate.ID, &packageName, &incumbent, &candidate.SourceURL, &state,
			&ecosystem, &misleading, &candidate.Observed, &confidence,
		); err != nil {
			return nil, fmt.Errorf("scan Radar digest candidate: %w", err)
		}
		candidate.Type = "radar"
		candidate.RadarID = candidate.ID
		candidate.Headline = packageName + " compared with " + incumbent
		candidate.Summary = fmt.Sprintf("%s is currently in %s with reviewed evidence for %s projects.", packageName, state, ecosystem)
		candidate.Action = "Review the comparison and record an explicit owner decision."
		candidate.Signal = "radar"
		candidate.Category = "radar"
		candidate.AppPath = "/radar?candidate=" + candidate.ID
		candidate.Score, candidate.Reason = digest.ScoreRadar(digest.RadarSignals{
			CurrentState: state, Misleading: misleading, ObservedAt: candidate.Observed,
		})
		candidate.Observed = candidate.Observed.UTC()
		candidate.Evidence = map[string]any{
			"confidence": confidence, "currentState": state, "ecosystem": ecosystem,
			"incumbent": incumbent, "misleading": misleading,
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Radar digest candidates: %w", err)
	}
	return candidates, nil
}

func loadEligibleCandidates(
	ctx context.Context,
	transaction pgx.Tx,
	occurrenceID string,
	settings occurrenceSettings,
	now time.Time,
) ([]digest.DigestCandidate, error) {
	rows, err := transaction.Query(ctx, `
		select captured.snapshot
		from app.digest_candidates captured
		where captured.occurrence_id = $1::uuid and (
			(captured.candidate_type = 'story' and exists (
				select 1
				from app.items item
				join app.content_revisions revision on revision.id = item.current_revision_id
				join app.raw_documents document on document.id = revision.raw_document_id
				left join app.user_item_states reading
					on reading.user_id = $2::uuid and reading.item_id = item.id
				left join app.user_source_preferences preference
					on preference.user_id = $2::uuid and preference.source_id = document.source_id
				where item.id = captured.item_id and item.status <> 'suppressed'
					and item.canonical_url ~ '^https?://'
					and coalesce(reading.location, 'inbox') <> 'archive'
					and (reading.snoozed_until is null or reading.snoozed_until <= $3)
					and ($4 or coalesce(reading.location, 'inbox') <> 'later')
					and not coalesce(preference.muted, false)
					and not coalesce(preference.exclude_from_digest, false)
			))
			or (captured.candidate_type = 'radar' and exists (
				select 1 from app.package_candidates candidate
				where candidate.id = captured.radar_candidate_id
					and candidate.user_id = $2::uuid and candidate.current_status <> 'reject'
			))
		)
		order by captured.candidate_type, captured.candidate_id`,
		occurrenceID, settings.UserID, now.UTC(), settings.IncludeLaterReminders)
	if err != nil {
		return nil, fmt.Errorf("recheck digest candidate eligibility: %w", err)
	}
	defer rows.Close()
	candidates := make([]digest.DigestCandidate, 0)
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return nil, fmt.Errorf("scan eligible digest candidate: %w", err)
		}
		var candidate digest.DigestCandidate
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			return nil, fmt.Errorf("decode eligible digest candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate eligible digest candidates: %w", err)
	}
	return candidates, nil
}
