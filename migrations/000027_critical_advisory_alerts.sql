-- +goose Up
set local lock_timeout = '5s';

-- Keep the matched evidence and installed version immutable. A later source
-- revision or owner watch edit may change the current decision, but cannot
-- silently rewrite what was delivered.
create table app.critical_alerts (
    id uuid primary key default uuidv7(),
    user_id uuid not null references app.users (id) on delete cascade,
    advisory_id text not null,
    ecosystem text not null,
    package_name text not null,
    current_version text not null,
    vulnerable_range text not null,
    patched_version text not null default '',
    raw_document_id uuid not null references app.raw_documents (id) on delete restrict,
    revision_id uuid not null references app.content_revisions (id) on delete restrict,
    item_id uuid not null references app.items (id) on delete restrict,
    source_id text not null references app.sources (id) on delete restrict,
    source_url text not null,
    title text not null,
    observed_at timestamptz not null,
    created_at timestamptz not null default now(),
    constraint critical_alerts_user_advisory_package_key
        unique (user_id, advisory_id, ecosystem, package_name),
    constraint critical_alerts_advisory_id_check
        check (advisory_id ~ '^GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$'),
    constraint critical_alerts_ecosystem_check
        check (ecosystem in ('go', 'npm', 'rust', 'pub')),
    constraint critical_alerts_package_name_check
        check (length(package_name) between 1 and 255),
    constraint critical_alerts_version_check
        check (length(current_version) between 1 and 100 and length(vulnerable_range) between 1 and 200
            and length(patched_version) <= 100),
    constraint critical_alerts_source_url_check
        check (length(source_url) between 1 and 4096),
    constraint critical_alerts_title_check
        check (length(title) between 1 and 500)
);

create index idx_critical_alerts_user_observed_at
    on app.critical_alerts (user_id, observed_at desc, id desc);

create index idx_critical_alerts_user_created_at
    on app.critical_alerts (user_id, created_at desc, id desc);

create table app.critical_alert_deliveries (
    id uuid primary key default uuidv7(),
    alert_id uuid not null references app.critical_alerts (id) on delete cascade,
    channel text not null,
    state text not null default 'pending',
    idempotency_key text not null,
    payload_sha256 bytea not null,
    next_attempt_at timestamptz not null,
    attempt_count integer not null default 0,
    last_attempt_at timestamptz,
    provider_id text,
    delivered_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint critical_alert_deliveries_alert_channel_key unique (alert_id, channel),
    constraint critical_alert_deliveries_idempotency_key unique (idempotency_key),
    constraint critical_alert_deliveries_channel_check check (channel in ('discord', 'email')),
    constraint critical_alert_deliveries_state_check
        check (state in ('pending', 'sending', 'sent', 'failed', 'permanent')),
    constraint critical_alert_deliveries_key_check check (length(idempotency_key) between 1 and 200),
    constraint critical_alert_deliveries_payload_sha256_check check (octet_length(payload_sha256) = 32),
    constraint critical_alert_deliveries_attempt_count_check check (attempt_count >= 0)
);

create index idx_critical_alert_deliveries_due
    on app.critical_alert_deliveries (next_attempt_at, id)
    where state in ('pending', 'failed');

create table app.critical_alert_attempts (
    id bigint generated always as identity primary key,
    delivery_id uuid not null references app.critical_alert_deliveries (id) on delete cascade,
    attempt_number integer not null,
    outcome text not null,
    error_code text,
    provider_id text,
    attempted_at timestamptz not null,
    completed_at timestamptz not null,
    constraint critical_alert_attempts_delivery_number_key unique (delivery_id, attempt_number),
    constraint critical_alert_attempts_attempt_number_check check (attempt_number > 0),
    constraint critical_alert_attempts_outcome_check check (outcome in ('sent', 'retryable', 'permanent')),
    constraint critical_alert_attempts_error_code_check
        check (error_code is null or length(error_code) between 1 and 100),
    constraint critical_alert_attempts_completed_at_check check (completed_at >= attempted_at)
);
