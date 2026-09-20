-- +goose Up
-- Validation scans existing rows under SHARE UPDATE EXCLUSIVE. Keeping it in
-- a separate migration releases migration 29's ACCESS EXCLUSIVE DDL locks first.
set local lock_timeout = '5s';
set local statement_timeout = '5min';

alter table app.critical_alerts
    validate constraint critical_alerts_correction_revision_id_fkey;

alter table app.critical_alerts
    validate constraint critical_alerts_correction_check;

alter table app.critical_alert_deliveries
    validate constraint critical_alert_deliveries_state_check;
