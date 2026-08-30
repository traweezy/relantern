-- +goose Up
create table app.digest_candidates (
    occurrence_id uuid not null,
    candidate_type text not null,
    candidate_id uuid not null,
    story_id uuid,
    item_id uuid,
    radar_candidate_id uuid,
    score numeric(5, 4) not null,
    category text not null,
    reason text not null,
    snapshot jsonb not null,
    captured_at timestamptz not null,
    constraint digest_candidates_pkey
        primary key (occurrence_id, candidate_type, candidate_id),
    constraint digest_candidates_occurrence_id_fkey
        foreign key (occurrence_id) references app.schedule_occurrences (id) on delete cascade,
    constraint digest_candidates_story_id_fkey
        foreign key (story_id) references app.story_clusters (id) on delete restrict,
    constraint digest_candidates_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint digest_candidates_radar_candidate_id_fkey
        foreign key (radar_candidate_id) references app.package_candidates (id) on delete restrict,
    constraint digest_candidates_type_check check (candidate_type in ('story', 'radar')),
    constraint digest_candidates_shape_check check (
        (candidate_type = 'story' and story_id = candidate_id and item_id is not null and radar_candidate_id is null)
        or
        (candidate_type = 'radar' and radar_candidate_id = candidate_id and story_id is null and item_id is null)
    ),
    constraint digest_candidates_score_check check (score between 0 and 1),
    constraint digest_candidates_category_check
        check (category in ('security', 'release', 'coming_soon', 'radar', 'later', 'general')),
    constraint digest_candidates_reason_check check (length(reason) between 3 and 1000),
    constraint digest_candidates_snapshot_check check (
        jsonb_typeof(snapshot) = 'object' and octet_length(snapshot::text) <= 32768
    )
);

create index idx_digest_candidates_occurrence_score
    on app.digest_candidates (occurrence_id, score desc, candidate_type, candidate_id);

create table app.digests (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    schedule_occurrence_id uuid not null,
    local_digest_date date not null,
    window_start timestamptz not null,
    window_end timestamptz not null,
    channel text not null,
    state text not null,
    item_limit integer not null,
    minimum_score numeric(5, 4) not null,
    empty_behavior text not null,
    executive_summary text not null,
    rendered_payload jsonb not null,
    payload_sha256 bytea not null,
    provider_idempotency_key text not null,
    generated_at timestamptz not null,
    completed_at timestamptz,
    error_code text,
    constraint digests_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint digests_schedule_occurrence_id_fkey
        foreign key (schedule_occurrence_id) references app.schedule_occurrences (id) on delete restrict,
    constraint digests_channel_check check (channel in ('dashboard', 'discord', 'email')),
    constraint digests_state_check
        check (state in ('ready', 'delivering', 'delivered', 'skipped', 'failed')),
    constraint digests_item_limit_check check (item_limit in (5, 10, 15, 20)),
    constraint digests_minimum_score_check check (minimum_score between 0 and 1),
    constraint digests_empty_behavior_check
        check (empty_behavior in ('send_nothing', 'all_clear', 'dashboard_only')),
    constraint digests_summary_check check (length(executive_summary) between 1 and 2000),
    constraint digests_payload_check check (
        jsonb_typeof(rendered_payload) = 'object' and octet_length(rendered_payload::text) <= 65536
    ),
    constraint digests_payload_sha256_check check (octet_length(payload_sha256) = 32),
    constraint digests_provider_idempotency_key_check
        check (length(provider_idempotency_key) between 16 and 255),
    constraint digests_window_check check (window_end > window_start),
    constraint digests_completed_at_check check (
        (state in ('delivered', 'skipped') and completed_at is not null)
        or (state in ('ready', 'delivering', 'failed') and completed_at is null)
    ),
    constraint digests_error_code_check
        check (error_code is null or length(error_code) between 1 and 120),
    constraint digests_user_date_channel_key unique (user_id, local_digest_date, channel),
    constraint digests_occurrence_channel_key unique (schedule_occurrence_id, channel),
    constraint digests_provider_idempotency_key_key unique (provider_idempotency_key)
);

create index idx_digests_user_generated_at
    on app.digests (user_id, generated_at desc, id desc);
create index idx_digests_failed_delivery
    on app.digests (generated_at, id)
    where state = 'failed' and channel in ('discord', 'email');

create table app.digest_items (
    digest_id uuid not null,
    candidate_type text not null,
    candidate_id uuid not null,
    sort_order integer not null,
    score numeric(5, 4) not null,
    category text not null,
    reason text not null,
    snapshot jsonb not null,
    constraint digest_items_pkey primary key (digest_id, sort_order),
    constraint digest_items_digest_id_fkey
        foreign key (digest_id) references app.digests (id) on delete cascade,
    constraint digest_items_candidate_type_check check (candidate_type in ('story', 'radar')),
    constraint digest_items_sort_order_check check (sort_order between 0 and 19),
    constraint digest_items_score_check check (score between 0 and 1),
    constraint digest_items_category_check
        check (category in ('security', 'release', 'coming_soon', 'radar', 'later', 'general')),
    constraint digest_items_reason_check check (length(reason) between 3 and 1000),
    constraint digest_items_snapshot_check check (
        jsonb_typeof(snapshot) = 'object' and octet_length(snapshot::text) <= 32768
    ),
    constraint digest_items_candidate_key unique (digest_id, candidate_type, candidate_id)
);

create table app.delivery_attempts (
    id bigint generated always as identity primary key,
    digest_id uuid not null,
    channel text not null,
    provider_id text,
    idempotency_key text not null,
    state text not null,
    attempt_count integer not null default 0,
    payload_sha256 bytea not null,
    attempted_at timestamptz,
    completed_at timestamptz,
    error_code text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint delivery_attempts_digest_id_fkey
        foreign key (digest_id) references app.digests (id) on delete cascade,
    constraint delivery_attempts_channel_check check (channel in ('discord', 'email')),
    constraint delivery_attempts_state_check
        check (state in ('pending', 'delivering', 'delivered', 'failed')),
    constraint delivery_attempts_attempt_count_check check (attempt_count >= 0),
    constraint delivery_attempts_payload_sha256_check check (octet_length(payload_sha256) = 32),
    constraint delivery_attempts_provider_id_check
        check (provider_id is null or length(provider_id) between 1 and 255),
    constraint delivery_attempts_error_code_check
        check (error_code is null or length(error_code) between 1 and 120),
    constraint delivery_attempts_completed_at_check check (
        (state = 'delivered' and completed_at is not null and provider_id is not null)
        or (state <> 'delivered' and completed_at is null)
    ),
    constraint delivery_attempts_digest_channel_key unique (digest_id, channel),
    constraint delivery_attempts_idempotency_key_key unique (idempotency_key)
);

create index idx_delivery_attempts_state_updated_at
    on app.delivery_attempts (state, updated_at, id);

-- +goose Down
drop table if exists app.delivery_attempts;
drop table if exists app.digest_items;
drop table if exists app.digests;
drop table if exists app.digest_candidates;
