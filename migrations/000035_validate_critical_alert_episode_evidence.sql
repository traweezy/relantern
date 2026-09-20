-- +goose Up
-- Validate under SHARE UPDATE EXCLUSIVE after migration 33 has released its
-- brief ACCESS EXCLUSIVE schema locks. Validation does not rewrite alerts.
set local lock_timeout = '5s';
set local statement_timeout = '5min';

alter table app.critical_alerts
    validate constraint critical_alerts_episode_number_check;
alter table app.critical_alerts
    validate constraint critical_alerts_opening_observation_id_fkey;
alter table app.critical_alerts
    validate constraint critical_alerts_correction_observation_id_fkey;
