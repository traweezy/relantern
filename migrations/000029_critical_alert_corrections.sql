-- +goose Up
set local lock_timeout = '5s';

-- Preserve the original admission snapshot and point to the later validated
-- revision that made an unsent urgent delivery stale.
alter table app.critical_alerts
    add column correction_reason text,
    add column correction_revision_id uuid,
    add column corrected_at timestamptz;

alter table app.critical_alerts
    add constraint critical_alerts_correction_revision_id_fkey
        foreign key (correction_revision_id) references app.content_revisions (id)
        on delete restrict not valid;

alter table app.critical_alerts
    add constraint critical_alerts_correction_check check (
        (correction_reason is null and correction_revision_id is null and corrected_at is null)
        or (correction_reason in ('withdrawn', 'no_longer_published',
                'severity_downgraded', 'severity_unconfirmed')
            and correction_revision_id is not null and corrected_at is not null)
    ) not valid;

-- Suppression is terminal and distinct from a provider or payload failure.
alter table app.critical_alert_deliveries
    add constraint critical_alert_deliveries_state_expanded_check
        check (state in ('pending', 'sending', 'sent', 'failed', 'permanent', 'suppressed')) not valid;
alter table app.critical_alert_deliveries
    drop constraint critical_alert_deliveries_state_check;
alter table app.critical_alert_deliveries
    rename constraint critical_alert_deliveries_state_expanded_check
    to critical_alert_deliveries_state_check;
