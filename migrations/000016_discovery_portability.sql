-- +goose Up
create table app.manual_captures (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    idempotency_key text not null,
    requested_url text not null,
    canonical_url text not null,
    state text not null default 'queued',
    source_id text,
    endpoint_id uuid,
    item_id uuid,
    cluster_id uuid,
    error_code text,
    created_at timestamptz not null default now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz not null default now(),
    constraint manual_captures_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint manual_captures_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete restrict,
    constraint manual_captures_endpoint_id_fkey
        foreign key (endpoint_id) references app.source_endpoints (id) on delete restrict,
    constraint manual_captures_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint manual_captures_cluster_id_fkey
        foreign key (cluster_id) references app.story_clusters (id) on delete restrict,
    constraint manual_captures_idempotency_key
        unique (user_id, idempotency_key),
    constraint manual_captures_idempotency_check
        check (length(idempotency_key) between 16 and 200),
    constraint manual_captures_requested_url_check
        check (length(requested_url) between 9 and 4096),
    constraint manual_captures_canonical_url_check
        check (length(canonical_url) between 9 and 4096),
    constraint manual_captures_state_check
        check (state in ('queued', 'fetching', 'parsing', 'deduplicating', 'completed', 'failed')),
    constraint manual_captures_error_code_check
        check (error_code is null or length(error_code) between 1 and 100),
    constraint manual_captures_started_at_check
        check (started_at is null or started_at >= created_at),
    constraint manual_captures_completed_at_check
        check (completed_at is null or (started_at is not null and completed_at >= started_at)),
    constraint manual_captures_state_shape_check check (
        (state = 'queued' and started_at is null and completed_at is null and error_code is null)
        or (state in ('fetching', 'parsing', 'deduplicating') and started_at is not null and completed_at is null and error_code is null)
        or (state = 'completed' and completed_at is not null and item_id is not null and cluster_id is not null and error_code is null)
        or (state = 'failed' and completed_at is not null and error_code is not null)
    )
);

create index idx_manual_captures_user_created_at
    on app.manual_captures (user_id, created_at desc, id desc);
create index idx_manual_captures_queued
    on app.manual_captures (created_at, id)
    where state = 'queued';

create table app.source_import_previews (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    document_sha256 bytea not null,
    candidates jsonb not null,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    committed_at timestamptz,
    constraint source_import_previews_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint source_import_previews_document_sha256_check
        check (octet_length(document_sha256) = 32),
    constraint source_import_previews_candidates_check
        check (jsonb_typeof(candidates) = 'array'),
    constraint source_import_previews_expires_at_check
        check (expires_at > created_at),
    constraint source_import_previews_committed_at_check
        check (committed_at is null or committed_at between created_at and expires_at)
);

create index idx_source_import_previews_user_expires_at
    on app.source_import_previews (user_id, expires_at desc, id desc);

create table app.saved_searches (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    name text not null,
    query text not null,
    filters jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint saved_searches_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint saved_searches_name_check check (length(name) between 1 and 100),
    constraint saved_searches_query_check check (length(query) between 2 and 500),
    constraint saved_searches_filters_check check (jsonb_typeof(filters) = 'object'),
    constraint saved_searches_user_name_key unique (user_id, name)
);

create index idx_saved_searches_user_updated_at
    on app.saved_searches (user_id, updated_at desc, id desc);

create table app.watched_technologies (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    technology text not null,
    package_name text not null,
    current_version text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint watched_technologies_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint watched_technologies_technology_check
        check (length(technology) between 1 and 120),
    constraint watched_technologies_package_name_check
        check (length(package_name) between 1 and 255),
    constraint watched_technologies_current_version_check
        check (length(current_version) <= 100),
    constraint watched_technologies_user_package_key unique (user_id, package_name)
);

create index idx_watched_technologies_user_technology
    on app.watched_technologies (user_id, technology, id);

-- +goose Down
drop table if exists app.watched_technologies;
drop table if exists app.saved_searches;
drop table if exists app.source_import_previews;
drop table if exists app.manual_captures;
