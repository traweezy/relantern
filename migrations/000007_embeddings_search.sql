-- +goose Up
create table app.embedding_models (
    model_id text primary key,
    provider text not null,
    dimensions integer not null,
    lifecycle_state text not null,
    evaluated_at timestamptz,
    activated_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint embedding_models_model_id_check check (length(model_id) between 1 and 255),
    constraint embedding_models_provider_check check (provider in ('openai')),
    constraint embedding_models_dimensions_check check (dimensions between 1 and 4096),
    constraint embedding_models_lifecycle_state_check
        check (lifecycle_state in ('building', 'active', 'inactive', 'retired')),
    constraint embedding_models_activation_check check (
        (lifecycle_state = 'active' and evaluated_at is not null and activated_at is not null)
        or lifecycle_state <> 'active'
    )
);

create unique index embedding_models_single_active_key
    on app.embedding_models ((true))
    where lifecycle_state = 'active';

insert into app.embedding_models (
    model_id,
    provider,
    dimensions,
    lifecycle_state,
    evaluated_at,
    activated_at
) values (
    'text-embedding-3-small',
    'openai',
    1536,
    'active',
    '2026-08-29T00:00:00Z',
    '2026-08-29T00:00:00Z'
);

create table app.embeddings (
    id uuid primary key default uuidv7(),
    entity_type text not null,
    entity_id uuid not null,
    revision_id uuid,
    model_id text not null,
    dimensions integer not null,
    embedding vector not null,
    content_sha256 bytea not null,
    created_at timestamptz not null default now(),
    constraint embeddings_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint embeddings_model_id_fkey
        foreign key (model_id) references app.embedding_models (model_id) on delete restrict,
    constraint embeddings_entity_type_check check (entity_type in ('item', 'story_cluster')),
    constraint embeddings_dimensions_check check (dimensions between 1 and 4096),
    constraint embeddings_vector_dimensions_check check (vector_dims(embedding) = dimensions),
    constraint embeddings_vector_norm_check check (vector_norm(embedding) > 0),
    constraint embeddings_content_sha256_check check (octet_length(content_sha256) = 32),
    constraint embeddings_entity_model_content_key
        unique (entity_type, entity_id, model_id, content_sha256)
);

create index idx_embeddings_entity_model_created_at
    on app.embeddings (entity_type, entity_id, model_id, created_at desc, id desc);
create index idx_embeddings_text_embedding_3_small_cosine_hnsw
    on app.embeddings
    using hnsw ((embedding::vector(1536)) vector_cosine_ops)
    where model_id = 'text-embedding-3-small' and dimensions = 1536;

create table app.search_documents (
    item_id uuid primary key,
    revision_id uuid not null,
    title text not null,
    summary text not null default '',
    entity_text text not null default '',
    package_name text not null default '',
    normalized_content text not null,
    source_tier text not null,
    lifecycle_state text not null,
    published_at timestamptz,
    first_seen_at timestamptz not null,
    search_vector tsvector generated always as (
        setweight(to_tsvector('english', title), 'A')
        || setweight(to_tsvector('english', package_name), 'A')
        || setweight(to_tsvector('english', entity_text), 'B')
        || setweight(to_tsvector('english', summary), 'B')
        || setweight(to_tsvector('english', normalized_content), 'C')
    ) stored,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint search_documents_item_id_fkey
        foreign key (item_id) references app.items (id) on delete cascade,
    constraint search_documents_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint search_documents_title_check check (length(title) between 1 and 1000),
    constraint search_documents_summary_check check (length(summary) <= 10000),
    constraint search_documents_entity_text_check check (length(entity_text) <= 10000),
    constraint search_documents_package_name_check check (length(package_name) <= 255),
    constraint search_documents_normalized_content_check
        check (length(normalized_content) between 1 and 1000000),
    constraint search_documents_source_tier_check check (source_tier in ('T0', 'T1', 'T2', 'T3')),
    constraint search_documents_lifecycle_state_check check (
        lifecycle_state in (
            'discovered', 'fetched', 'normalized', 'duplicate', 'clustered',
            'awaiting_ai', 'extracting', 'needs_review', 'ready', 'published',
            'suppressed', 'failed_retryable', 'failed_terminal'
        )
    )
);

create index idx_search_documents_search_vector
    on app.search_documents using gin (search_vector);
create index idx_search_documents_title_trgm
    on app.search_documents using gin (lower(title) gin_trgm_ops);
create index idx_search_documents_package_name_trgm
    on app.search_documents using gin (lower(package_name) gin_trgm_ops);
create index idx_search_documents_filters
    on app.search_documents (lifecycle_state, source_tier, first_seen_at desc, item_id);

alter table app.dedupe_decisions
    add constraint dedupe_decisions_method_v2_check
    check (method in (
        'new', 'revision', 'canonical_url', 'raw_sha256',
        'normalized_sha256', 'metadata', 'simhash', 'embedding',
        'package_version'
    )) not valid;
alter table app.dedupe_decisions
    validate constraint dedupe_decisions_method_v2_check;
alter table app.dedupe_decisions
    drop constraint dedupe_decisions_method_check;
alter table app.dedupe_decisions
    rename constraint dedupe_decisions_method_v2_check
    to dedupe_decisions_method_check;

-- +goose Down
alter table app.dedupe_decisions
    add constraint dedupe_decisions_method_v1_check
    check (method in (
        'new', 'revision', 'canonical_url', 'raw_sha256',
        'normalized_sha256', 'metadata', 'simhash', 'package_version'
    )) not valid;
alter table app.dedupe_decisions
    validate constraint dedupe_decisions_method_v1_check;
alter table app.dedupe_decisions
    drop constraint dedupe_decisions_method_check;
alter table app.dedupe_decisions
    rename constraint dedupe_decisions_method_v1_check
    to dedupe_decisions_method_check;
drop table if exists app.search_documents;
drop table if exists app.embeddings;
drop table if exists app.embedding_models;
