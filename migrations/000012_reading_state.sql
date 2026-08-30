-- +goose Up
create view app.v_story_summaries as
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
    end as signal,
    coalesce((
        select source.source_tier
        from app.item_sources source
        where source.item_id = item.id
        order by source.sort_order, source.source_tier
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
    brief.created_at as brief_created_at
from app.research_briefs brief
join app.ai_runs run on run.id = brief.ai_run_id
join app.story_clusters cluster on cluster.id = brief.cluster_id
join app.items item on item.id = cluster.primary_item_id
where run.state in ('completed', 'needs_review')
    and not exists (
        select 1 from app.research_briefs newer
        where newer.cluster_id = brief.cluster_id
            and (newer.created_at, newer.id) > (brief.created_at, brief.id)
    );

create table app.user_item_states (
    user_id uuid not null,
    item_id uuid not null,
    location text not null default 'inbox',
    is_read boolean not null default false,
    read_at timestamptz,
    starred_at timestamptz,
    snoozed_until timestamptz,
    snoozed_from_location text,
    reading_progress numeric(6, 5) not null default 0,
    last_paragraph_id text,
    later_position bigint,
    dismissed_reason text,
    version bigint not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint user_item_states_pkey primary key (user_id, item_id),
    constraint user_item_states_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint user_item_states_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint user_item_states_location_check
        check (location in ('inbox', 'later', 'archive')),
    constraint user_item_states_read_check
        check ((is_read and read_at is not null) or (not is_read and read_at is null)),
    constraint user_item_states_snooze_check check (
        (snoozed_until is null and snoozed_from_location is null)
        or (
            snoozed_until is not null
            and snoozed_from_location in ('inbox', 'later')
            and location = snoozed_from_location
        )
    ),
    constraint user_item_states_reading_progress_check
        check (reading_progress between 0 and 1),
    constraint user_item_states_last_paragraph_id_check
        check (last_paragraph_id is null or length(last_paragraph_id) between 1 and 255),
    constraint user_item_states_later_position_check
        check (later_position is null or later_position >= 0),
    constraint user_item_states_dismissed_reason_check check (
        dismissed_reason is null
        or dismissed_reason in (
            'irrelevant_topic', 'duplicate', 'too_promotional', 'low_quality', 'already_known'
        )
    ),
    constraint user_item_states_version_check check (version >= 0)
);

create index idx_user_item_states_inbox
    on app.user_item_states (user_id, updated_at desc, item_id)
    where location = 'inbox';
create index idx_user_item_states_later
    on app.user_item_states (user_id, later_position, item_id)
    where location = 'later';
create index idx_user_item_states_archive
    on app.user_item_states (user_id, updated_at desc, item_id)
    where location = 'archive';
create index idx_user_item_states_starred
    on app.user_item_states (user_id, starred_at desc, item_id)
    where starred_at is not null;
create index idx_user_item_states_snoozed
    on app.user_item_states (user_id, snoozed_until, item_id)
    where snoozed_until is not null;

create table app.tags (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    name text not null,
    normalized_name text not null,
    color_token text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint tags_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint tags_user_normalized_name_key unique (user_id, normalized_name),
    constraint tags_name_check check (length(name) between 1 and 80),
    constraint tags_normalized_name_check
        check (length(normalized_name) between 1 and 80 and normalized_name = lower(normalized_name)),
    constraint tags_color_token_check
        check (color_token in ('accent', 'blue', 'green', 'orange', 'purple', 'red', 'slate'))
);

create index idx_tags_user_name on app.tags (user_id, normalized_name, id);

create table app.item_tags (
    user_id uuid not null,
    item_id uuid not null,
    tag_id uuid not null,
    created_at timestamptz not null default now(),
    constraint item_tags_pkey primary key (user_id, item_id, tag_id),
    constraint item_tags_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint item_tags_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint item_tags_tag_id_fkey
        foreign key (tag_id) references app.tags (id) on delete cascade
);

create index idx_item_tags_tag_id on app.item_tags (user_id, tag_id, item_id);

create table app.annotations (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    item_id uuid not null,
    revision_id uuid not null,
    annotation_type text not null,
    start_offset integer,
    end_offset integer,
    quote_hash bytea,
    body text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint annotations_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint annotations_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint annotations_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint annotations_annotation_type_check
        check (annotation_type in ('document_note', 'highlight', 'highlight_note')),
    constraint annotations_span_check check (
        (
            annotation_type = 'document_note'
            and start_offset is null
            and end_offset is null
            and quote_hash is null
            and length(body) between 1 and 20000
        )
        or (
            annotation_type in ('highlight', 'highlight_note')
            and start_offset >= 0
            and end_offset > start_offset
            and octet_length(quote_hash) = 32
            and length(body) <= 20000
        )
    )
);

create index idx_annotations_user_item
    on app.annotations (user_id, item_id, created_at, id);

create table app.feedback (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    target_type text not null,
    target_id uuid not null,
    feedback_type text not null,
    note text,
    created_at timestamptz not null default now(),
    constraint feedback_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint feedback_target_type_check check (target_type in ('story', 'item')),
    constraint feedback_feedback_type_check check (
        feedback_type in (
            'useful', 'already_known', 'irrelevant', 'too_shallow',
            'too_verbose', 'incorrect', 'dismissed'
        )
    ),
    constraint feedback_note_check check (note is null or length(note) <= 2000)
);

create index idx_feedback_user_target
    on app.feedback (user_id, target_type, target_id, created_at desc);

create table app.item_state_mutations (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    item_id uuid not null,
    mutation_type text not null,
    before_state jsonb not null,
    after_state jsonb not null,
    idempotency_key text not null,
    bulk_id uuid,
    undo_deadline timestamptz not null,
    undone_at timestamptz,
    created_at timestamptz not null default now(),
    constraint item_state_mutations_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint item_state_mutations_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint item_state_mutations_user_idempotency_key_key
        unique (user_id, idempotency_key),
    constraint item_state_mutations_mutation_type_check
        check (length(mutation_type) between 1 and 80),
    constraint item_state_mutations_before_state_check
        check (jsonb_typeof(before_state) = 'object'),
    constraint item_state_mutations_after_state_check
        check (jsonb_typeof(after_state) = 'object'),
    constraint item_state_mutations_idempotency_key_check
        check (length(idempotency_key) between 16 and 255),
    constraint item_state_mutations_undo_deadline_check
        check (undo_deadline >= created_at),
    constraint item_state_mutations_undone_at_check
        check (undone_at is null or undone_at >= created_at)
);

create index idx_item_state_mutations_user_item
    on app.item_state_mutations (user_id, item_id, created_at desc, id desc);
create index idx_item_state_mutations_retention
    on app.item_state_mutations (created_at, id);
create index idx_item_state_mutations_bulk_id
    on app.item_state_mutations (user_id, bulk_id, id)
    where bulk_id is not null;

-- +goose Down
drop table if exists app.item_state_mutations;
drop table if exists app.feedback;
drop table if exists app.annotations;
drop table if exists app.item_tags;
drop table if exists app.tags;
drop table if exists app.user_item_states;
drop view if exists app.v_story_summaries;
