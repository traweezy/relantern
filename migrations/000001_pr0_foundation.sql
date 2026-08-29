-- +goose Up
create schema if not exists app;

create extension if not exists pg_trgm with schema public;
create extension if not exists vector with schema public;

create table app.users (
    id uuid primary key default uuidv7(),
    github_user_id bigint not null,
    login text not null,
    display_name text,
    timezone text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint users_github_user_id_key unique (github_user_id),
    constraint users_login_check check (length(login) between 1 and 255),
    constraint users_timezone_check check (length(timezone) between 1 and 255)
);

create table app.schedule_definitions (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    schedule_type text not null,
    timezone text not null,
    local_time time not null,
    days_of_week smallint[] not null,
    enabled boolean not null default true,
    catchup_policy text not null,
    catchup_grace interval not null,
    next_due_at timestamptz not null,
    skip_next_at timestamptz,
    paused_at timestamptz,
    config jsonb not null default '{}'::jsonb,
    version bigint not null default 1,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint schedule_definitions_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint schedule_definitions_user_schedule_type_key unique (user_id, schedule_type),
    constraint schedule_definitions_schedule_type_check
        check (schedule_type in ('daily_digest', 'weekly_radar', 'maintenance')),
    constraint schedule_definitions_days_of_week_check
        check (cardinality(days_of_week) between 1 and 7 and days_of_week <@ array[1,2,3,4,5,6,7]::smallint[]),
    constraint schedule_definitions_catchup_policy_check
        check (catchup_policy in ('catch_up', 'skip')),
    constraint schedule_definitions_catchup_grace_check
        check (catchup_grace >= interval '0 seconds'),
    constraint schedule_definitions_version_check check (version > 0)
);

create index idx_schedule_definitions_next_due_at_partial
    on app.schedule_definitions (next_due_at)
    where enabled and paused_at is null;

create table app.schedule_occurrences (
    id uuid primary key default uuidv7(),
    schedule_id uuid not null,
    scheduled_for timestamptz not null,
    local_date date not null,
    local_offset_seconds integer not null,
    state text not null,
    trigger_type text not null,
    idempotency_key text not null,
    started_at timestamptz,
    completed_at timestamptz,
    error_code text,
    metadata jsonb not null default '{}'::jsonb,
    constraint schedule_occurrences_schedule_id_fkey
        foreign key (schedule_id) references app.schedule_definitions (id) on delete cascade,
    constraint schedule_occurrences_schedule_scheduled_key unique (schedule_id, scheduled_for),
    constraint schedule_occurrences_idempotency_key_key unique (idempotency_key),
    constraint schedule_occurrences_state_check
        check (state in ('due', 'enqueued', 'preparing', 'ready', 'delivering', 'delivered', 'skipped', 'missed', 'failed')),
    constraint schedule_occurrences_trigger_type_check
        check (trigger_type in ('scheduled', 'catch_up', 'run_now', 'retry'))
);

create index idx_schedule_occurrences_state_scheduled_for
    on app.schedule_occurrences (state, scheduled_for);

create table app.outbox_events (
    id bigint generated always as identity primary key,
    event_type text not null,
    aggregate_type text not null,
    aggregate_id uuid not null,
    payload jsonb not null,
    created_at timestamptz not null default now(),
    constraint outbox_events_event_type_check check (length(event_type) between 1 and 120),
    constraint outbox_events_aggregate_type_check check (length(aggregate_type) between 1 and 120)
);

create index idx_outbox_events_created_at on app.outbox_events (created_at, id);

-- +goose Down
drop table if exists app.outbox_events;
drop table if exists app.schedule_occurrences;
drop table if exists app.schedule_definitions;
drop table if exists app.users;
drop schema if exists app;
