-- +goose Up
-- Keep the view shape stable for its API, digest, and Live consumers. A brief
-- belongs to the current story only while its primary item and revision remain
-- eligible; earlier evidence remains stored for provenance and audit.
set local lock_timeout = '5s';

create or replace view app.v_story_summaries as
select
    cluster.id as story_id,
    cluster.primary_item_id as item_id,
    brief.headline,
    brief.summary,
    brief.why_it_matters,
    brief.recommended_action,
    brief.confidence,
    cluster.first_seen_at,
    cluster.last_changed_at,
    case when item.status = 'updated' then 'updated' else 'new' end as status,
    case
        when exists (
            select 1 from app.claims claim
            join app.cluster_members member on member.item_id = claim.item_id
            join app.items evidence_item on evidence_item.id = member.item_id
            where member.cluster_id = cluster.id
                and claim.revision_id = evidence_item.current_revision_id
                and claim.verification_state = 'verified_span'
                and claim.claim_type = 'security'
        ) then 'security'
        when exists (
            select 1 from app.claims claim
            join app.cluster_members member on member.item_id = claim.item_id
            join app.items evidence_item on evidence_item.id = member.item_id
            where member.cluster_id = cluster.id
                and claim.revision_id = evidence_item.current_revision_id
                and claim.verification_state = 'verified_span'
                and claim.claim_type = 'breaking_change'
        ) then 'breaking-change'
        when exists (
            select 1 from app.claims claim
            join app.cluster_members member on member.item_id = claim.item_id
            join app.items evidence_item on evidence_item.id = member.item_id
            where member.cluster_id = cluster.id
                and claim.revision_id = evidence_item.current_revision_id
                and claim.verification_state = 'verified_span'
                and claim.claim_type = 'deprecation'
        ) then 'deprecation'
        when item.event_type = 'migration' then 'migration'
        when item.event_type = 'release' then 'release'
        else 'general'
    end as signal,
    coalesce((
        select source.source_tier
        from app.item_sources source
        where source.item_id = item.id
            and source.revision_id = item.current_revision_id
            and source.source_role = 'primary'
        limit 1
    ), 'T3') as source_tier,
    (
        select count(*) from (
            select source_url from app.research_sources source
            where source.ai_run_id = brief.ai_run_id
            union
            select canonical_url from app.item_sources source
            where source.item_id = item.id
        ) story_sources
    )::integer as source_count,
    greatest(1, ceil((length(brief.summary) + length(brief.why_it_matters)
        + length(brief.recommended_action))::numeric / 1000))::integer as read_time_minutes,
    brief.uncertainties,
    brief.created_at as brief_created_at,
    coalesce((
        select source.canonical_url
        from app.item_sources source
        where source.item_id = item.id
            and source.revision_id = item.current_revision_id
            and source.source_role = 'primary'
        limit 1
    ), item.canonical_url) as primary_source_url
from app.research_briefs brief
join app.ai_runs run on run.id = brief.ai_run_id
join app.story_clusters cluster on cluster.id = brief.cluster_id
join app.items item on item.id = cluster.primary_item_id
where run.state in ('completed', 'needs_review')
    and run.item_id = item.id
    and run.revision_id = item.current_revision_id
    and item.lifecycle_state in ('ready', 'published')
    and exists (
        select 1 from app.item_sources source
        where source.item_id = item.id
            and source.revision_id = item.current_revision_id
            and source.source_role = 'primary'
            and source.source_tier in ('T0', 'T1')
    )
    and not exists (
        select 1 from app.research_briefs newer
        join app.ai_runs newer_run on newer_run.id = newer.ai_run_id
        where newer.cluster_id = brief.cluster_id
            and newer_run.state in ('completed', 'needs_review')
            and newer_run.item_id = item.id
            and newer_run.revision_id = item.current_revision_id
            and (newer.created_at, newer.id) > (brief.created_at, brief.id)
    );
