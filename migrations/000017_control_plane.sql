-- +goose Up
create table app.interest_profiles (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    version bigint not null default 1,
    name text not null,
    is_active boolean not null default true,
    profile_summary text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint interest_profiles_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint interest_profiles_version_check check (version > 0),
    constraint interest_profiles_name_check check (length(name) between 1 and 120),
    constraint interest_profiles_summary_check check (length(profile_summary) <= 2000),
    constraint interest_profiles_user_name_key unique (user_id, name)
);

create unique index interest_profiles_active_user_key
    on app.interest_profiles (user_id)
    where is_active;

create table app.interest_topics (
    profile_id uuid not null,
    topic_id text not null,
    priority smallint not null,
    weight numeric(5, 4) not null,
    keywords text[] not null default array[]::text[],
    exclusions text[] not null default array[]::text[],
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint interest_topics_pkey primary key (profile_id, topic_id),
    constraint interest_topics_profile_id_fkey
        foreign key (profile_id) references app.interest_profiles (id) on delete cascade,
    constraint interest_topics_topic_id_check
        check (topic_id ~ '^[a-z0-9]+(?:[-_][a-z0-9]+)*$' and length(topic_id) <= 100),
    constraint interest_topics_priority_check check (priority between 1 and 5),
    constraint interest_topics_weight_check check (weight between 0 and 1),
    constraint interest_topics_keywords_check check (cardinality(keywords) <= 50),
    constraint interest_topics_exclusions_check check (cardinality(exclusions) <= 50)
);

create index idx_interest_topics_profile_priority
    on app.interest_topics (profile_id, priority, topic_id);

create table app.user_source_preferences (
    user_id uuid not null,
    source_id text not null,
    muted boolean not null default false,
    exclude_from_digest boolean not null default false,
    relevance_adjustment numeric(5, 4) not null default 0,
    version bigint not null default 1,
    updated_at timestamptz not null default now(),
    constraint user_source_preferences_pkey primary key (user_id, source_id),
    constraint user_source_preferences_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint user_source_preferences_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint user_source_preferences_relevance_check
        check (relevance_adjustment between -1 and 1),
    constraint user_source_preferences_version_check check (version > 0)
);

create index idx_user_source_preferences_user_updated_at
    on app.user_source_preferences (user_id, updated_at desc, source_id);

create table app.owner_settings (
    user_id uuid primary key,
    quiet_hours_start time not null default '22:00',
    quiet_hours_end time not null default '07:00',
    critical_alerts_bypass boolean not null default true,
    monthly_soft_budget_usd numeric(14, 8) not null default 25,
    monthly_hard_budget_usd numeric(14, 8) not null default 50,
    raw_retention_days integer not null default 90,
    audit_retention_days integer not null default 365,
    version bigint not null default 1,
    updated_at timestamptz not null default now(),
    constraint owner_settings_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint owner_settings_quiet_hours_check
        check (quiet_hours_start <> quiet_hours_end),
    constraint owner_settings_budget_check
        check (
            monthly_soft_budget_usd > 0
            and monthly_hard_budget_usd >= monthly_soft_budget_usd
        ),
    constraint owner_settings_raw_retention_check
        check (raw_retention_days between 7 and 3650),
    constraint owner_settings_audit_retention_check
        check (audit_retention_days between 30 and 3650),
    constraint owner_settings_version_check check (version > 0)
);

insert into app.owner_settings (user_id)
select id from app.users
on conflict (user_id) do nothing;

insert into app.interest_profiles (user_id, name, profile_summary)
select id, 'Owner intelligence', 'Private software-engineering intelligence profile.'
from app.users
on conflict (user_id, name) do nothing;

insert into app.interest_topics (profile_id, topic_id, priority, weight)
select profile.id, 'software-engineering', 1, 1
from app.interest_profiles profile
where profile.is_active
on conflict (profile_id, topic_id) do nothing;

alter table app.watched_technologies
    add column version_constraint text not null default '',
    add column status text not null default 'active',
    add column source text not null default 'owner',
    add column last_verified_at timestamptz;

alter table app.watched_technologies
    add constraint watched_technologies_version_constraint_check
        check (length(version_constraint) <= 200),
    add constraint watched_technologies_status_check
        check (status in ('active', 'evaluating', 'legacy', 'planned')),
    add constraint watched_technologies_source_check
        check (length(source) between 1 and 120);

create table app.source_validation_runs (
    id uuid primary key default uuidv7(),
    source_id text not null,
    requested_by uuid not null,
    state text not null,
    check_count integer not null,
    failed_check_count integer not null,
    explanation text not null,
    started_at timestamptz not null,
    completed_at timestamptz not null,
    constraint source_validation_runs_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint source_validation_runs_requested_by_fkey
        foreign key (requested_by) references app.users (id) on delete cascade,
    constraint source_validation_runs_state_check check (state in ('passed', 'failed')),
    constraint source_validation_runs_count_check check (
        check_count > 0
        and failed_check_count between 0 and check_count
    ),
    constraint source_validation_runs_explanation_check
        check (length(explanation) between 1 and 1000),
    constraint source_validation_runs_completed_at_check check (completed_at >= started_at)
);

create index idx_source_validation_runs_source_completed_at
    on app.source_validation_runs (source_id, completed_at desc, id desc);

alter table app.schedule_definitions
    add column weekend_mode text not null default 'normal',
    add column maximum_items integer not null default 10,
    add column minimum_score numeric(5, 4) not null default 0.5,
    add column include_coming_soon boolean not null default true,
    add column include_radar_candidates boolean not null default true,
    add column include_later_reminders boolean not null default false,
    add column empty_behavior text not null default 'dashboard_only',
    add column channels text[] not null default array['dashboard']::text[],
    add column paused_until timestamptz;

alter table app.schedule_definitions
    add constraint schedule_definitions_weekend_mode_check
        check (weekend_mode in ('normal', 'weekly_only', 'off')),
    add constraint schedule_definitions_maximum_items_check
        check (maximum_items in (5, 10, 15, 20)),
    add constraint schedule_definitions_minimum_score_check
        check (minimum_score between 0 and 1),
    add constraint schedule_definitions_empty_behavior_check
        check (empty_behavior in ('send_nothing', 'all_clear', 'dashboard_only')),
    add constraint schedule_definitions_channels_check check (
        cardinality(channels) between 1 and 3
        and channels <@ array['dashboard', 'discord', 'email']::text[]
    ),
    add constraint schedule_definitions_paused_until_check check (
        (paused_at is null and paused_until is null)
        or (paused_at is not null and paused_until is null)
        or (paused_at is not null and paused_until > paused_at)
    );

create index idx_schedule_definitions_paused_until_partial
    on app.schedule_definitions (paused_until)
    where paused_at is not null and paused_until is not null;

-- +goose Down
drop index if exists app.idx_schedule_definitions_paused_until_partial;

alter table app.schedule_definitions
    drop constraint if exists schedule_definitions_paused_until_check,
    drop constraint if exists schedule_definitions_channels_check,
    drop constraint if exists schedule_definitions_empty_behavior_check,
    drop constraint if exists schedule_definitions_minimum_score_check,
    drop constraint if exists schedule_definitions_maximum_items_check,
    drop constraint if exists schedule_definitions_weekend_mode_check,
    drop column if exists paused_until,
    drop column if exists channels,
    drop column if exists empty_behavior,
    drop column if exists include_later_reminders,
    drop column if exists include_radar_candidates,
    drop column if exists include_coming_soon,
    drop column if exists minimum_score,
    drop column if exists maximum_items,
    drop column if exists weekend_mode;

drop table if exists app.source_validation_runs;

alter table app.watched_technologies
    drop constraint if exists watched_technologies_source_check,
    drop constraint if exists watched_technologies_status_check,
    drop constraint if exists watched_technologies_version_constraint_check,
    drop column if exists last_verified_at,
    drop column if exists source,
    drop column if exists status,
    drop column if exists version_constraint;

drop table if exists app.owner_settings;
drop table if exists app.user_source_preferences;
drop table if exists app.interest_topics;
drop table if exists app.interest_profiles;
