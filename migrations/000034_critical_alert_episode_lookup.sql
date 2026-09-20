-- +goose NO TRANSACTION
-- +goose Up
-- Prepare the episode key online while the legacy one-alert uniqueness still
-- prevents episode two. The legacy constraint is removed only after the
-- ordered assessment path and alert writer have passed their cutover tests.
set lock_timeout = '5s';
create unique index concurrently if not exists critical_alerts_user_advisory_package_episode_key
    on app.critical_alerts
    (user_id, advisory_id, ecosystem, package_name, episode_number);
reset lock_timeout;

-- A failed concurrent build may leave an invalid index with this name. Do
-- not silently mark the migration complete when IF NOT EXISTS skips it.
-- +goose StatementBegin
do $$
begin
    if not exists (
        select 1 from pg_index state
        join pg_class index_name on index_name.oid = state.indexrelid
        where index_name.relname = 'critical_alerts_user_advisory_package_episode_key'
            and state.indisvalid and state.indisready and state.indisunique
    ) then
        raise exception 'episode unique index is invalid; drop it concurrently and retry migration';
    end if;
end $$;
-- +goose StatementEnd
