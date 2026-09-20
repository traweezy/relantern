-- +goose NO TRANSACTION
-- +goose Up
-- A validated advisory correction finds every admission from older raw
-- revisions of the same source entry. Keep that lookup bounded as history grows.
set lock_timeout = '5s';
drop index concurrently if exists app.idx_critical_alerts_raw_document_advisory;
create index concurrently idx_critical_alerts_raw_document_advisory
    on app.critical_alerts (raw_document_id, advisory_id);
reset lock_timeout;
