-- +goose Up
create table app.items (
    id uuid primary key default uuidv7(),
    current_revision_id uuid not null,
    canonical_url text not null,
    title text not null,
    normalized_title text not null,
    normalized_author text not null,
    package_name text not null default '',
    version text not null default '',
    slug text not null,
    lifecycle_state text not null,
    event_type text not null default 'unknown',
    published_at timestamptz,
    first_seen_at timestamptz not null,
    status text not null,
    simhash bytea not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint items_current_revision_id_fkey
        foreign key (current_revision_id) references app.content_revisions (id) on delete restrict,
    constraint items_current_revision_id_key unique (current_revision_id),
    constraint items_canonical_url_check check (length(canonical_url) between 1 and 4096),
    constraint items_title_check check (length(title) between 1 and 1000),
    constraint items_slug_key unique (slug),
    constraint items_slug_check check (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    constraint items_lifecycle_state_check check (
        lifecycle_state in (
            'discovered', 'fetched', 'normalized', 'duplicate', 'clustered',
            'awaiting_ai', 'extracting', 'needs_review', 'ready', 'published',
            'suppressed', 'failed_retryable', 'failed_terminal'
        )
    ),
    constraint items_status_check check (status in ('active', 'updated', 'suppressed')),
    constraint items_simhash_check check (octet_length(simhash) = 8)
);

create index idx_items_first_seen_at on app.items (first_seen_at desc, id desc);
create index idx_items_normalized_title on app.items (normalized_title);
create index idx_items_package_version_partial
    on app.items (package_name, version, first_seen_at desc)
    where package_name <> '' and version <> '';

create table app.story_clusters (
    id uuid primary key default uuidv7(),
    primary_item_id uuid not null,
    cluster_key text not null,
    title text not null,
    first_seen_at timestamptz not null,
    last_changed_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint story_clusters_primary_item_id_fkey
        foreign key (primary_item_id) references app.items (id) on delete restrict,
    constraint story_clusters_cluster_key_key unique (cluster_key),
    constraint story_clusters_cluster_key_check check (length(cluster_key) between 1 and 255),
    constraint story_clusters_title_check check (length(title) between 1 and 1000),
    constraint story_clusters_last_changed_at_check check (last_changed_at >= first_seen_at)
);

create index idx_story_clusters_last_changed_at
    on app.story_clusters (last_changed_at desc, id desc);

create table app.item_sources (
    revision_id uuid primary key,
    item_id uuid not null,
    canonical_url text not null,
    source_role text not null,
    source_tier text not null,
    sort_order integer not null,
    created_at timestamptz not null default now(),
    constraint item_sources_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint item_sources_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint item_sources_canonical_url_check check (length(canonical_url) between 1 and 4096),
    constraint item_sources_source_role_check
        check (source_role in ('primary', 'supporting', 'duplicate')),
    constraint item_sources_source_tier_check check (source_tier in ('T0', 'T1', 'T2', 'T3')),
    constraint item_sources_sort_order_check check (sort_order >= 0),
    constraint item_sources_item_sort_order_key unique (item_id, sort_order)
);

create index idx_item_sources_item_id on app.item_sources (item_id, sort_order);

create table app.cluster_members (
    cluster_id uuid not null,
    item_id uuid not null,
    similarity numeric(6, 5) not null,
    method text not null,
    created_at timestamptz not null default now(),
    constraint cluster_members_pkey primary key (cluster_id, item_id),
    constraint cluster_members_cluster_id_fkey
        foreign key (cluster_id) references app.story_clusters (id) on delete restrict,
    constraint cluster_members_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint cluster_members_item_id_key unique (item_id),
    constraint cluster_members_similarity_check check (similarity between 0 and 1),
    constraint cluster_members_method_check check (length(method) between 1 and 100)
);

create table app.dedupe_decisions (
    revision_id uuid primary key,
    item_id uuid not null,
    cluster_id uuid not null,
    candidate_item_id uuid,
    outcome text not null,
    method text not null,
    similarity numeric(6, 5) not null,
    simhash_distance integer,
    details jsonb not null,
    evaluated_at timestamptz not null,
    created_at timestamptz not null default now(),
    constraint dedupe_decisions_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint dedupe_decisions_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint dedupe_decisions_cluster_id_fkey
        foreign key (cluster_id) references app.story_clusters (id) on delete restrict,
    constraint dedupe_decisions_candidate_item_id_fkey
        foreign key (candidate_item_id) references app.items (id) on delete restrict,
    constraint dedupe_decisions_outcome_check
        check (outcome in ('created', 'duplicate', 'revision', 'clustered')),
    constraint dedupe_decisions_method_check
        check (method in ('new', 'revision', 'canonical_url', 'raw_sha256', 'normalized_sha256', 'metadata', 'simhash', 'package_version')),
    constraint dedupe_decisions_similarity_check check (similarity between 0 and 1),
    constraint dedupe_decisions_simhash_distance_check
        check (simhash_distance is null or simhash_distance between 0 and 64),
    constraint dedupe_decisions_details_check check (jsonb_typeof(details) = 'object')
);

create index idx_dedupe_decisions_item_id on app.dedupe_decisions (item_id, evaluated_at desc);
create index idx_dedupe_decisions_cluster_id on app.dedupe_decisions (cluster_id, evaluated_at desc);

-- +goose Down
drop table if exists app.dedupe_decisions;
drop table if exists app.cluster_members;
drop table if exists app.item_sources;
drop table if exists app.story_clusters;
drop table if exists app.items;
