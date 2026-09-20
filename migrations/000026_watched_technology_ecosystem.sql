-- +goose Up
-- Existing watches have no proven registry identity. Keep them untyped so
-- urgent advisory matching cannot infer an ecosystem from a display label.
set local lock_timeout = '5s';

alter table app.watched_technologies
    add column ecosystem text;

alter table app.watched_technologies
    add constraint watched_technologies_ecosystem_check
    check (ecosystem in (
        'actions', 'composer', 'erlang', 'go', 'maven', 'npm', 'nuget',
        'other', 'pip', 'pub', 'rubygems', 'rust', 'swift'
    )) not valid;

alter table app.watched_technologies
    validate constraint watched_technologies_ecosystem_check;

-- Urgent delivery has its own owner policy. Digest schedules may be paused or
-- configured for different channels, so never derive critical delivery from them.
alter table app.owner_settings
    add column critical_alert_channels text[] not null default array['dashboard']::text[];

alter table app.owner_settings
    add constraint owner_settings_critical_alert_channels_check
    check (
        cardinality(critical_alert_channels) between 1 and 3
        and critical_alert_channels <@ array['dashboard', 'discord', 'email']::text[]
        and critical_alert_channels @> array['dashboard']::text[]
        and array_position(critical_alert_channels, null::text) is null
        and cardinality(array_positions(critical_alert_channels, 'dashboard')) <= 1
        and cardinality(array_positions(critical_alert_channels, 'discord')) <= 1
        and cardinality(array_positions(critical_alert_channels, 'email')) <= 1
    ) not valid;

alter table app.owner_settings
    validate constraint owner_settings_critical_alert_channels_check;
