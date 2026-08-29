-- +goose Up
create table app.source_checkpoints (
    endpoint_id uuid primary key,
    cursor text,
    etag text,
    last_modified text,
    provider_state jsonb not null default '{}'::jsonb,
    updated_at timestamptz not null default now(),
    constraint source_checkpoints_endpoint_id_fkey
        foreign key (endpoint_id) references app.source_endpoints (id) on delete cascade,
    constraint source_checkpoints_provider_state_check
        check (jsonb_typeof(provider_state) = 'object')
);

create table app.source_runtime_overrides (
    source_id text primary key,
    polling_enabled boolean not null,
    poll_interval interval,
    reason text not null,
    updated_by uuid,
    updated_at timestamptz not null default now(),
    constraint source_runtime_overrides_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint source_runtime_overrides_updated_by_fkey
        foreign key (updated_by) references app.users (id) on delete set null,
    constraint source_runtime_overrides_poll_interval_check
        check (poll_interval is null or poll_interval between interval '5 minutes' and interval '7 days'),
    constraint source_runtime_overrides_reason_check check (length(reason) between 1 and 1000)
);

create table app.source_fetches (
    id bigint generated always as identity primary key,
    endpoint_id uuid not null,
    attempted_at timestamptz not null,
    completed_at timestamptz not null,
    outcome text not null,
    status_code integer,
    final_url text,
    content_type text,
    compressed_bytes bigint not null default 0,
    bytes bigint not null default 0,
    duration_ms integer not null,
    error_code text,
    retry_after timestamptz,
    etag text,
    last_modified text,
    raw_sha256 bytea,
    object_key text,
    created_at timestamptz not null default now(),
    constraint source_fetches_endpoint_id_fkey
        foreign key (endpoint_id) references app.source_endpoints (id) on delete cascade,
    constraint source_fetches_outcome_check
        check (outcome in ('stored', 'metadata_only', 'not_modified', 'failed')),
    constraint source_fetches_status_code_check
        check (status_code is null or status_code between 100 and 599),
    constraint source_fetches_completed_at_check check (completed_at >= attempted_at),
    constraint source_fetches_compressed_bytes_check check (compressed_bytes >= 0),
    constraint source_fetches_bytes_check check (bytes >= 0),
    constraint source_fetches_duration_ms_check check (duration_ms >= 0),
    constraint source_fetches_raw_sha256_check
        check (raw_sha256 is null or octet_length(raw_sha256) = 32),
    constraint source_fetches_outcome_shape_check check (
        (outcome = 'stored' and status_code between 200 and 299 and raw_sha256 is not null and object_key is not null and error_code is null)
        or (outcome = 'metadata_only' and status_code between 200 and 299 and raw_sha256 is not null and object_key is null and error_code is null)
        or (outcome = 'not_modified' and status_code = 304 and object_key is null and error_code is null)
        or (outcome = 'failed' and error_code is not null and object_key is null)
    )
);

create index idx_source_fetches_endpoint_attempted_at
    on app.source_fetches (endpoint_id, attempted_at desc, id desc);

create index idx_source_fetches_error_code_partial
    on app.source_fetches (error_code, attempted_at desc)
    where error_code is not null;

create table app.raw_documents (
    id uuid primary key default uuidv7(),
    source_id text not null,
    canonical_url text not null,
    object_key text,
    raw_sha256 bytea not null,
    first_seen_at timestamptz not null,
    first_fetched_at timestamptz not null,
    source_published_at timestamptz,
    content_policy text not null,
    created_at timestamptz not null default now(),
    constraint raw_documents_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint raw_documents_source_url_sha256_key
        unique (source_id, canonical_url, raw_sha256),
    constraint raw_documents_object_key_key unique (object_key),
    constraint raw_documents_raw_sha256_check check (octet_length(raw_sha256) = 32),
    constraint raw_documents_first_fetched_at_check check (first_fetched_at >= first_seen_at),
    constraint raw_documents_content_policy_check
        check (content_policy in ('link-and-excerpt', 'metadata-only')),
    constraint raw_documents_object_policy_check
        check ((content_policy = 'link-and-excerpt' and object_key is not null) or (content_policy = 'metadata-only' and object_key is null))
);

create index idx_raw_documents_source_first_seen_at
    on app.raw_documents (source_id, first_seen_at desc, id desc);

-- +goose Down
drop table if exists app.raw_documents;
drop table if exists app.source_fetches;
drop table if exists app.source_runtime_overrides;
drop table if exists app.source_checkpoints;
