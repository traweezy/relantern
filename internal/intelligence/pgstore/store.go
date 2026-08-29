package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/intelligence"
)

const storySummarySelect = `
	select
		cluster.id::text,
		brief.headline,
		brief.summary,
		brief.why_it_matters,
		brief.recommended_action,
		brief.confidence,
		cluster.first_seen_at,
		cluster.last_changed_at,
		case when item.status = 'updated' then 'updated' else 'new' end,
		case
			when exists (
				select 1 from app.claims claim
				join app.cluster_members member on member.item_id = claim.item_id
				where member.cluster_id = cluster.id and claim.claim_type = 'security'
			) then 'security'
			when exists (
				select 1 from app.claims claim
				join app.cluster_members member on member.item_id = claim.item_id
				where member.cluster_id = cluster.id and claim.claim_type = 'breaking_change'
			) then 'breaking-change'
			when exists (
				select 1 from app.claims claim
				join app.cluster_members member on member.item_id = claim.item_id
				where member.cluster_id = cluster.id and claim.claim_type = 'deprecation'
			) then 'deprecation'
			when item.event_type = 'release' then 'release'
			else 'general'
		end,
		coalesce((
			select source.source_tier
			from app.item_sources source
			where source.item_id = item.id
			order by source.sort_order, source.source_tier
			limit 1
		), 'T3'),
		(
			select count(*) from (
				select source_url from app.research_sources source
				where source.ai_run_id = brief.ai_run_id
				union
				select canonical_url from app.item_sources source
				where source.item_id = item.id
			) story_sources
		)::integer,
		greatest(1, ceil((length(brief.summary) + length(brief.why_it_matters)
			+ length(brief.recommended_action))::numeric / 1000))::integer,
		brief.uncertainties
	from app.research_briefs brief
	join app.ai_runs run on run.id = brief.ai_run_id
	join app.story_clusters cluster on cluster.id = brief.cluster_id
	join app.items item on item.id = cluster.primary_item_id
	where run.state in ('completed', 'needs_review')`

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("intelligence PostgreSQL store requires a database pool")
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Today(ctx context.Context, generatedAt time.Time) (intelligence.TodaySnapshot, error) {
	stories, err := store.listStories(ctx, generatedAt.Add(-24*time.Hour), 24)
	if err != nil {
		return intelligence.TodaySnapshot{}, err
	}
	snapshot := intelligence.TodaySnapshot{
		CoverageEndAt: generatedAt.UTC(), CoverageStartAt: generatedAt.Add(-24 * time.Hour).UTC(),
		DeliveryState: "unavailable", GeneratedAt: generatedAt.UTC(), Stories: stories,
	}
	var nextRunAt pgtype.Timestamptz
	var activeSources int
	var enabledSources int
	err = store.pool.QueryRow(ctx, `
		select
			case coalesce((
				select occurrence.state
				from app.schedule_occurrences occurrence
				join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
				where schedule.schedule_type = 'daily_digest'
				order by occurrence.scheduled_for desc
				limit 1
			), 'unavailable')
				when 'delivered' then 'delivered'
				when 'unavailable' then 'unavailable'
				else 'pending'
			end,
			(
				select min(next_due_at)
				from app.schedule_definitions
				where schedule_type = 'daily_digest' and enabled and paused_at is null
			),
			(select count(*) from app.sources where enabled and validation_state = 'active')::integer,
			(select count(*) from app.sources where enabled)::integer,
			coalesce((
				select to_char(sum(estimated_cost_usd), 'FM999999990.00000000')
				from app.ai_runs
				where started_at >= date_trunc('month', $1::timestamptz)
			), '0.00000000'),
			(select count(*) from app.ai_runs where state = 'needs_review')::integer`, generatedAt).Scan(
		&snapshot.DeliveryState,
		&nextRunAt,
		&activeSources,
		&enabledSources,
		&snapshot.Stats.EstimatedCostUSD,
		&snapshot.Stats.ReviewRequired,
	)
	if err != nil {
		return intelligence.TodaySnapshot{}, fmt.Errorf("select Today operational summary: %w", err)
	}
	if nextRunAt.Valid {
		nextRunUTC := nextRunAt.Time.UTC()
		snapshot.NextRunAt = &nextRunUTC
	}
	if enabledSources > 0 {
		snapshot.Stats.SourceCoverage = activeSources * 100 / enabledSources
	}
	for _, story := range stories {
		if story.Signal == "security" {
			snapshot.Stats.CriticalAlerts++
		}
		if story.Signal == "release" {
			snapshot.Stats.Releases++
		}
	}
	return snapshot, nil
}

func (store *Store) Live(ctx context.Context, generatedAt time.Time) (intelligence.LiveSnapshot, error) {
	stories, err := store.listStories(ctx, generatedAt.Add(-7*24*time.Hour), 50)
	if err != nil {
		return intelligence.LiveSnapshot{}, err
	}
	events := make([]intelligence.LiveEvent, 0, len(stories))
	for _, story := range stories {
		eventType := "story-created"
		if story.Status == "updated" {
			eventType = "story-updated"
		}
		events = append(events, intelligence.LiveEvent{
			ID:         story.ID + ":" + story.LastChangedAt.UTC().Format(time.RFC3339Nano),
			ObservedAt: story.LastChangedAt.UTC(), Story: story, Type: eventType,
		})
	}
	return intelligence.LiveSnapshot{Events: events, GeneratedAt: generatedAt.UTC()}, nil
}

func (store *Store) Story(ctx context.Context, storyID string) (intelligence.StoryDetail, error) {
	if strings.TrimSpace(storyID) == "" {
		return intelligence.StoryDetail{}, intelligence.ErrStoryNotFound
	}
	row := store.pool.QueryRow(ctx, storySummarySelect+`
		and cluster.id = $1::uuid
		order by brief.created_at desc
		limit 1`, storyID)
	story, uncertainties, err := scanStory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return intelligence.StoryDetail{}, intelligence.ErrStoryNotFound
	}
	if err != nil {
		return intelligence.StoryDetail{}, fmt.Errorf("select intelligence story: %w", err)
	}
	detail := intelligence.StoryDetail{StorySummary: story, Uncertainties: uncertainties}
	detail.Sources, err = store.storySources(ctx, storyID)
	if err != nil {
		return intelligence.StoryDetail{}, err
	}
	detail.Assertions, err = store.storyAssertions(ctx, storyID)
	if err != nil {
		return intelligence.StoryDetail{}, err
	}
	detail.Related, err = store.relatedStories(ctx, storyID, 3)
	if err != nil {
		return intelligence.StoryDetail{}, err
	}
	return detail, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanStory(row rowScanner) (intelligence.StorySummary, []string, error) {
	story := intelligence.StorySummary{}
	var uncertaintiesJSON []byte
	err := row.Scan(
		&story.ID, &story.Headline, &story.Summary, &story.WhyItMatters,
		&story.RecommendedAction, &story.Confidence, &story.FirstSeenAt,
		&story.LastChangedAt, &story.Status, &story.Signal, &story.SourceTier,
		&story.SourceCount, &story.ReadTimeMinutes, &uncertaintiesJSON,
	)
	if err != nil {
		return intelligence.StorySummary{}, nil, err
	}
	story.FirstSeenAt = story.FirstSeenAt.UTC()
	story.LastChangedAt = story.LastChangedAt.UTC()
	uncertainties := make([]string, 0)
	if len(uncertaintiesJSON) > 0 {
		if err := json.Unmarshal(uncertaintiesJSON, &uncertainties); err != nil {
			return intelligence.StorySummary{}, nil, fmt.Errorf("decode story uncertainties: %w", err)
		}
	}
	return story, uncertainties, nil
}

func (store *Store) listStories(
	ctx context.Context,
	changedAfter time.Time,
	limit int,
) ([]intelligence.StorySummary, error) {
	rows, err := store.pool.Query(ctx, storySummarySelect+`
		and cluster.last_changed_at >= $1
		and not exists (
			select 1 from app.research_briefs newer
			where newer.cluster_id = brief.cluster_id
				and (newer.created_at, newer.id) > (brief.created_at, brief.id)
		)
		order by cluster.last_changed_at desc, cluster.id desc
		limit $2`, changedAfter.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list intelligence stories: %w", err)
	}
	defer rows.Close()
	stories := make([]intelligence.StorySummary, 0)
	for rows.Next() {
		story, _, scanError := scanStory(rows)
		if scanError != nil {
			return nil, fmt.Errorf("scan intelligence story: %w", scanError)
		}
		stories = append(stories, story)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intelligence stories: %w", err)
	}
	return stories, nil
}

func (store *Store) storySources(ctx context.Context, storyID string) ([]intelligence.Source, error) {
	rows, err := store.pool.Query(ctx, `
		with latest_brief as (
			select brief.ai_run_id, cluster.primary_item_id
			from app.research_briefs brief
			join app.story_clusters cluster on cluster.id = brief.cluster_id
			where brief.cluster_id = $1::uuid
			order by brief.created_at desc
			limit 1
		), combined_sources as (
			select source.source_url, source.source_domain, 'T1'::text as source_tier, 1 as sort_order
			from app.research_sources source
			join latest_brief brief on brief.ai_run_id = source.ai_run_id
			union
			select source.canonical_url, '', source.source_tier, 0
			from app.item_sources source
			join latest_brief brief on brief.primary_item_id = source.item_id
		)
		select source_url, source_domain, source_tier
		from combined_sources
		order by sort_order, source_tier, source_url`, storyID)
	if err != nil {
		return nil, fmt.Errorf("select intelligence story sources: %w", err)
	}
	defer rows.Close()
	sources := make([]intelligence.Source, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		source := intelligence.Source{}
		if err := rows.Scan(&source.URL, &source.Domain, &source.Tier); err != nil {
			return nil, fmt.Errorf("scan intelligence source: %w", err)
		}
		if _, exists := seen[source.URL]; exists {
			continue
		}
		seen[source.URL] = struct{}{}
		if source.Domain == "" {
			source.Domain = sourceDomain(source.URL)
		}
		source.Label = source.Domain
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intelligence sources: %w", err)
	}
	return sources, nil
}

func (store *Store) storyAssertions(ctx context.Context, storyID string) ([]intelligence.ClaimEvidence, error) {
	rows, err := store.pool.Query(ctx, `
		with latest_brief as (
			select id from app.research_briefs
			where cluster_id = $1::uuid
			order by created_at desc
			limit 1
		)
		select assertion.assertion_text, assertion.material,
			coalesce(jsonb_agg(jsonb_build_object(
				'domain', source.source_domain,
				'label', source.source_domain,
				'tier', 'T1',
				'url', source.source_url
			) order by source.source_domain, source.source_url)
			filter (where source.id is not null), '[]'::jsonb)
		from app.research_assertions assertion
		join latest_brief brief on brief.id = assertion.research_brief_id
		left join app.research_assertion_sources link on link.research_assertion_id = assertion.id
		left join app.research_sources source on source.id = link.research_source_id
		group by assertion.id, assertion.assertion_text, assertion.material, assertion.assertion_index
		order by assertion.assertion_index`, storyID)
	if err != nil {
		return nil, fmt.Errorf("select intelligence assertions: %w", err)
	}
	defer rows.Close()
	assertions := make([]intelligence.ClaimEvidence, 0)
	for rows.Next() {
		assertion := intelligence.ClaimEvidence{}
		var sourcesJSON []byte
		if err := rows.Scan(&assertion.Claim, &assertion.Material, &sourcesJSON); err != nil {
			return nil, fmt.Errorf("scan intelligence assertion: %w", err)
		}
		assertion.Sources = make([]intelligence.Source, 0)
		if err := json.Unmarshal(sourcesJSON, &assertion.Sources); err != nil {
			return nil, fmt.Errorf("decode assertion sources: %w", err)
		}
		assertions = append(assertions, assertion)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intelligence assertions: %w", err)
	}
	return assertions, nil
}

func (store *Store) relatedStories(
	ctx context.Context,
	storyID string,
	limit int,
) ([]intelligence.StorySummary, error) {
	rows, err := store.pool.Query(ctx, storySummarySelect+`
		and cluster.id <> $1::uuid
		and not exists (
			select 1 from app.research_briefs newer
			where newer.cluster_id = brief.cluster_id
				and (newer.created_at, newer.id) > (brief.created_at, brief.id)
		)
		order by cluster.last_changed_at desc, cluster.id desc
		limit $2`, storyID, limit)
	if err != nil {
		return nil, fmt.Errorf("select related intelligence stories: %w", err)
	}
	defer rows.Close()
	related := make([]intelligence.StorySummary, 0)
	for rows.Next() {
		story, _, scanError := scanStory(rows)
		if scanError != nil {
			return nil, fmt.Errorf("scan related intelligence story: %w", scanError)
		}
		related = append(related, story)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate related intelligence stories: %w", err)
	}
	return related, nil
}

func sourceDomain(sourceURL string) string {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "source"
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}
