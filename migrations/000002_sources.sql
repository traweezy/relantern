-- +goose Up
create table app.sources (
    id text primary key,
    name text not null,
    trust_tier text not null,
    owner text not null,
    origin text not null,
    validation_state text not null,
    homepage_url text not null,
    content_policy text not null,
    enabled boolean not null,
    topics text[] not null,
    reviewed_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint sources_id_check check (id ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    constraint sources_name_check check (length(name) between 1 and 255),
    constraint sources_trust_tier_check check (trust_tier in ('T0', 'T1', 'T2', 'T3')),
    constraint sources_origin_check check (origin in ('system', 'owner')),
    constraint sources_validation_state_check
        check (validation_state in ('pending', 'active', 'degraded', 'paused', 'rejected')),
    constraint sources_content_policy_check
        check (content_policy in ('link-and-excerpt', 'metadata-only')),
    constraint sources_topics_check check (cardinality(topics) > 0)
);

create table app.source_endpoints (
    id uuid primary key default uuidv7(),
    registry_id text not null,
    source_id text not null,
    connector text not null,
    url text not null,
    poll_interval interval not null,
    priority text not null,
    robots_policy text not null,
    expected_content_types text[] not null,
    max_response_bytes bigint not null,
    fixture_suite text not null,
    config jsonb not null default '{}'::jsonb,
    next_poll_at timestamptz,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    health_state text not null default 'unverified',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint source_endpoints_registry_id_key unique (registry_id),
    constraint source_endpoints_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint source_endpoints_source_connector_url_key unique (source_id, connector, url),
    constraint source_endpoints_connector_check
        check (connector in ('atom', 'rss', 'json_feed', 'page', 'github_releases', 'github_advisories', 'registry', 'structured_api')),
    constraint source_endpoints_priority_check check (priority in ('critical', 'high', 'normal')),
    constraint source_endpoints_robots_policy_check check (robots_policy in ('feed', 'page', 'api')),
    constraint source_endpoints_poll_interval_check
        check (poll_interval between interval '5 minutes' and interval '7 days'),
    constraint source_endpoints_expected_content_types_check
        check (cardinality(expected_content_types) > 0),
    constraint source_endpoints_max_response_bytes_check check (max_response_bytes > 0),
    constraint source_endpoints_health_state_check
        check (health_state in ('unverified', 'healthy', 'degraded', 'paused', 'failed'))
);

create index idx_source_endpoints_next_poll_at_partial
    on app.source_endpoints (next_poll_at)
    where next_poll_at is not null and health_state <> 'paused';

create index idx_source_endpoints_source_id on app.source_endpoints (source_id);

create table app.github_repositories (
    source_id text primary key,
    node_id text not null,
    repository_owner text not null,
    repository_name text not null,
    enabled_events text[] not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint github_repositories_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint github_repositories_node_id_key unique (node_id),
    constraint github_repositories_owner_name_key unique (repository_owner, repository_name),
    constraint github_repositories_owner_check check (length(repository_owner) between 1 and 255),
    constraint github_repositories_name_check check (length(repository_name) between 1 and 255),
    constraint github_repositories_enabled_events_check
        check (
            cardinality(enabled_events) > 0
            and enabled_events <@ array['releases', 'security_advisories']::text[]
        )
);

-- +goose Down
drop table if exists app.github_repositories;
drop table if exists app.source_endpoints;
drop table if exists app.sources;
