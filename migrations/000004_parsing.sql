-- +goose Up
create table app.content_revisions (
    id uuid primary key default uuidv7(),
    raw_document_id uuid not null,
    previous_revision_id uuid,
    normalized_sha256 bytea not null,
    normalized_text_object_key text not null,
    parser_name text not null,
    parser_version text not null,
    title text not null default '',
    author text not null default '',
    language text not null,
    source_published_at timestamptz,
    source_updated_at timestamptz,
    normalized_bytes bigint not null,
    outline jsonb not null,
    offset_map jsonb not null,
    warnings jsonb not null,
    change_kind text not null,
    change_reason text not null,
    material_change boolean not null,
    observed_at timestamptz not null,
    created_at timestamptz not null default now(),
    constraint content_revisions_raw_document_id_fkey
        foreign key (raw_document_id) references app.raw_documents (id) on delete restrict,
    constraint content_revisions_previous_revision_id_fkey
        foreign key (previous_revision_id) references app.content_revisions (id) on delete restrict,
    constraint content_revisions_raw_document_sha256_key
        unique (raw_document_id, normalized_sha256),
    constraint content_revisions_normalized_sha256_check
        check (octet_length(normalized_sha256) = 32),
    constraint content_revisions_parser_name_check check (length(parser_name) between 1 and 100),
    constraint content_revisions_parser_version_check check (length(parser_version) between 1 and 50),
    constraint content_revisions_language_check check (language ~ '^[a-z]{2,3}$' or language = 'und'),
    constraint content_revisions_normalized_bytes_check check (normalized_bytes > 0),
    constraint content_revisions_outline_check check (jsonb_typeof(outline) = 'array'),
    constraint content_revisions_offset_map_check check (jsonb_typeof(offset_map) = 'array'),
    constraint content_revisions_warnings_check check (jsonb_typeof(warnings) = 'array'),
    constraint content_revisions_change_kind_check check (change_kind in ('initial', 'material')),
    constraint content_revisions_change_reason_check check (length(change_reason) between 1 and 1000),
    constraint content_revisions_change_shape_check check (
        (change_kind = 'initial' and previous_revision_id is null and not material_change)
        or (change_kind = 'material' and previous_revision_id is not null and material_change)
    )
);

create index idx_content_revisions_raw_document_observed_at
    on app.content_revisions (raw_document_id, observed_at desc, id desc);

create index idx_content_revisions_normalized_sha256
    on app.content_revisions (normalized_sha256);

create index idx_content_revisions_normalized_text_object_key
    on app.content_revisions (normalized_text_object_key);

create index idx_raw_documents_source_canonical_url
    on app.raw_documents (source_id, canonical_url, first_seen_at desc, id desc);

create table app.source_parse_attempts (
    id bigint generated always as identity primary key,
    raw_document_id uuid not null,
    revision_id uuid,
    outcome text not null,
    parser_name text not null,
    parser_version text not null,
    attempted_at timestamptz not null,
    completed_at timestamptz not null,
    duration_ms integer not null,
    error_code text,
    warnings jsonb not null,
    created_at timestamptz not null default now(),
    constraint source_parse_attempts_raw_document_id_fkey
        foreign key (raw_document_id) references app.raw_documents (id) on delete cascade,
    constraint source_parse_attempts_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete set null,
    constraint source_parse_attempts_outcome_check check (outcome in ('created', 'unchanged', 'failed')),
    constraint source_parse_attempts_completed_at_check check (completed_at >= attempted_at),
    constraint source_parse_attempts_duration_ms_check check (duration_ms >= 0),
    constraint source_parse_attempts_parser_name_check check (length(parser_name) between 1 and 100),
    constraint source_parse_attempts_parser_version_check check (length(parser_version) between 1 and 50),
    constraint source_parse_attempts_error_code_check check (error_code is null or length(error_code) between 1 and 100),
    constraint source_parse_attempts_warnings_check check (jsonb_typeof(warnings) = 'array'),
    constraint source_parse_attempts_outcome_shape_check check (
        (outcome in ('created', 'unchanged') and revision_id is not null and error_code is null)
        or (outcome = 'failed' and revision_id is null and error_code is not null)
    )
);

create index idx_source_parse_attempts_raw_document_attempted_at
    on app.source_parse_attempts (raw_document_id, attempted_at desc, id desc);

create index idx_source_parse_attempts_error_code_partial
    on app.source_parse_attempts (error_code, attempted_at desc)
    where error_code is not null;

-- +goose Down
drop table if exists app.source_parse_attempts;
drop table if exists app.content_revisions;
drop index if exists app.idx_raw_documents_source_canonical_url;
