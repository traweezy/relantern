-- +goose NO TRANSACTION
-- +goose Up

-- Keep legacy NULL ecosystems distinct from typed registry identities. Add the
-- replacement invariant before releasing the older package-only constraint.
create unique index concurrently if not exists watched_technologies_user_ecosystem_package_key
    on app.watched_technologies (user_id, ecosystem, package_name) nulls not distinct;

create index concurrently if not exists idx_watched_technologies_active_ecosystem_package
    on app.watched_technologies (ecosystem, package_name)
    where ecosystem is not null and status = 'active';

set lock_timeout = '5s';

-- +goose StatementBegin
do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conrelid = 'app.watched_technologies'::regclass
            and conname = 'watched_technologies_user_ecosystem_package_key'
    ) then
        alter table app.watched_technologies
            add constraint watched_technologies_user_ecosystem_package_key
            unique using index watched_technologies_user_ecosystem_package_key;
    end if;
end $$;
-- +goose StatementEnd

alter table app.watched_technologies
    drop constraint if exists watched_technologies_user_package_key;

reset lock_timeout;
