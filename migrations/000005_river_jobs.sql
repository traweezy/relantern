-- +goose Up
create schema if not exists river;

alter table app.schedule_occurrences
    add column river_job_id bigint,
    add constraint schedule_occurrences_river_job_id_key unique (river_job_id),
    add constraint schedule_occurrences_river_job_id_check
        check (river_job_id is null or river_job_id > 0);

create table app.audit_events (
    id bigint generated always as identity primary key,
    actor_type text not null,
    actor_id text not null,
    action text not null,
    target_type text not null,
    target_id text not null,
    request_id text,
    metadata jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    constraint audit_events_actor_type_check check (length(actor_type) between 1 and 80),
    constraint audit_events_actor_id_check check (length(actor_id) between 1 and 255),
    constraint audit_events_action_check check (length(action) between 1 and 120),
    constraint audit_events_target_type_check check (length(target_type) between 1 and 80),
    constraint audit_events_target_id_check check (length(target_id) between 1 and 255),
    constraint audit_events_request_id_check check (request_id is null or length(request_id) between 1 and 255)
);

create index idx_audit_events_created_at on app.audit_events (created_at, id);
create index idx_audit_events_target on app.audit_events (target_type, target_id, created_at desc);

-- +goose Down
drop table if exists app.audit_events;
alter table app.schedule_occurrences drop column if exists river_job_id;
