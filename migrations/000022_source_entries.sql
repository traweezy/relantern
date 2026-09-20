-- +goose NO TRANSACTION
-- +goose Up
-- Indexes below build concurrently. Brief schema cutovers enforce a 5s lock
-- timeout inside their own statements; a busy deployment may retry safely.

-- +goose StatementBegin
do $$
begin
perform set_config('lock_timeout', '5s', true);
create table if not exists app.source_entries (
    id uuid primary key default uuidv7(),
    source_id text not null,
    external_id text not null,
    first_seen_at timestamptz not null,
    last_seen_at timestamptz not null,
    constraint source_entries_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint source_entries_source_external_id_key unique (source_id, external_id),
    constraint source_entries_external_id_check check (length(external_id) between 1 and 2048),
    constraint source_entries_seen_at_check check (last_seen_at >= first_seen_at)
);
end $$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
begin
perform set_config('lock_timeout', '5s', true);
alter table app.raw_documents
    add column if not exists parent_raw_document_id uuid,
    add column if not exists source_entry_id uuid,
    add column if not exists source_registry_id text,
    add column if not exists source_connector text,
    add column if not exists source_content_type text,
    add column if not exists ingestion_error_code text,
    add column if not exists ingestion_failed_at timestamptz;
end $$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
begin
    perform set_config('lock_timeout', '5s', true);
    if not exists (select 1 from pg_constraint where conname = 'raw_documents_parent_raw_document_id_fkey') then
        alter table app.raw_documents add constraint raw_documents_parent_raw_document_id_fkey
            foreign key (parent_raw_document_id) references app.raw_documents (id) on delete cascade not valid;
    end if;
    if not exists (select 1 from pg_constraint where conname = 'raw_documents_source_entry_id_fkey') then
        alter table app.raw_documents add constraint raw_documents_source_entry_id_fkey
            foreign key (source_entry_id) references app.source_entries (id) on delete cascade not valid;
    end if;
    if not exists (select 1 from pg_constraint where conname = 'raw_documents_entry_shape_check') then
        alter table app.raw_documents add constraint raw_documents_entry_shape_check check (
            (parent_raw_document_id is null and source_entry_id is null)
            or (parent_raw_document_id is not null and source_entry_id is not null)
        ) not valid;
    end if;
    if not exists (select 1 from pg_constraint where conname = 'raw_documents_parent_distinct_check') then
        alter table app.raw_documents add constraint raw_documents_parent_distinct_check check (
            parent_raw_document_id is null or parent_raw_document_id <> id
        ) not valid;
    end if;
    if not exists (select 1 from pg_constraint where conname = 'raw_documents_ingestion_failure_check') then
        alter table app.raw_documents add constraint raw_documents_ingestion_failure_check check (
            (ingestion_error_code is null and ingestion_failed_at is null)
            or (ingestion_error_code is not null and ingestion_failed_at is not null)
        ) not valid;
    end if;
end $$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
begin
    perform set_config('lock_timeout', '5s', true);
    alter table app.raw_documents validate constraint raw_documents_parent_raw_document_id_fkey;
    alter table app.raw_documents validate constraint raw_documents_source_entry_id_fkey;
    alter table app.raw_documents validate constraint raw_documents_entry_shape_check;
    alter table app.raw_documents validate constraint raw_documents_parent_distinct_check;
    alter table app.raw_documents validate constraint raw_documents_ingestion_failure_check;
end $$;
-- +goose StatementEnd

create index concurrently if not exists idx_source_fetches_object_key_partial
    on app.source_fetches (object_key, completed_at desc, id desc)
    where object_key is not null;

-- +goose StatementBegin
do $$
begin
    if not exists (
        select 1 from pg_index index_state
        join pg_class index_name on index_name.oid = index_state.indexrelid
        where index_name.relname = 'idx_source_fetches_object_key_partial'
            and index_state.indisvalid and index_state.indisready
    ) then
        raise exception 'source fetch object-key index is invalid; drop it concurrently and retry migration';
    end if;
end $$;
-- +goose StatementEnd

-- Preserve parse provenance beyond operational fetch-attempt retention. A raw
-- key may appear in later fetch attempts from another endpoint, so only the
-- one stored attempt matching the immutable first-observation timestamps,
-- source, URL, and digest may identify its creator. Ambiguous or missing
-- matches stay null for operator review instead of receiving false provenance.
-- Each 500-row batch commits independently, and an unresolved row cannot
-- prevent later identifiable rows from being backfilled.
-- +goose StatementBegin
do $$
declare
    changed integer;
    last_id uuid := '00000000-0000-0000-0000-000000000000';
    total_changed bigint := 0;
begin
    loop
        perform set_config('lock_timeout', '5s', true);
        with batch as (
            select raw.id, raw.source_id, raw.canonical_url, raw.raw_sha256,
                raw.object_key, raw.first_seen_at, raw.first_fetched_at
            from app.raw_documents raw
            where raw.id > last_id and raw.source_registry_id is null
                and raw.object_key is not null
            order by raw.id
            limit 500
            for update of raw
        ), provenance as (
            select batch.id, original.registry_id, original.connector,
                original.content_type
            from batch
            join lateral (
                select min(candidate.registry_id) as registry_id,
                    min(candidate.connector) as connector,
                    min(candidate.content_type) as content_type
                from (
                    select endpoint.registry_id, endpoint.connector,
                        coalesce(source_fetch.content_type, '') as content_type
                    from app.source_fetches source_fetch
                    join app.source_endpoints endpoint
                        on endpoint.id = source_fetch.endpoint_id
                    where source_fetch.object_key = batch.object_key
                        and source_fetch.outcome = 'stored'
                        and source_fetch.final_url = batch.canonical_url
                        and source_fetch.raw_sha256 = batch.raw_sha256
                        and source_fetch.attempted_at = batch.first_seen_at
                        and source_fetch.completed_at = batch.first_fetched_at
                        and endpoint.source_id = batch.source_id
                    limit 2
                ) candidate
                having count(*) = 1
            ) original on true
        ), updated as (
            update app.raw_documents raw
            set source_registry_id = provenance.registry_id,
                source_connector = provenance.connector,
                source_content_type = provenance.content_type
            from provenance
            where raw.id = provenance.id
            returning raw.id
        )
        select (select id from batch order by id desc limit 1),
            (select count(*) from updated)
        into last_id, changed;
        total_changed := total_changed + changed;
        exit when last_id is null;
        commit;
    end loop;
    raise notice 'source-entry provenance backfilled % raw documents', total_changed;
end $$;
-- +goose StatementEnd

-- Existing parents retain their URL/digest identity. Child revisions use the
-- provider's stable external ID so two entries may share a link or body.
create unique index concurrently if not exists idx_raw_documents_parent_source_url_sha256
    on app.raw_documents (source_id, canonical_url, raw_sha256)
    where source_entry_id is null;

-- +goose StatementBegin
do $$
begin
    perform set_config('lock_timeout', '5s', true);
    if not exists (
        select 1 from pg_index index_state
        join pg_class index_name on index_name.oid = index_state.indexrelid
        where index_name.relname = 'idx_raw_documents_parent_source_url_sha256'
            and index_state.indisvalid and index_state.indisready
    ) then
        raise exception 'parent source URL index is invalid; drop it concurrently and retry migration';
    end if;
    alter table app.raw_documents drop constraint if exists raw_documents_source_url_sha256_key;
end $$;
-- +goose StatementEnd

create unique index concurrently if not exists idx_raw_documents_entry_sha256
    on app.raw_documents (source_entry_id, raw_sha256)
    where source_entry_id is not null;

create index concurrently if not exists idx_raw_documents_source_entry_first_seen_at
    on app.raw_documents (source_entry_id, first_seen_at desc, id desc)
    where source_entry_id is not null;

create index concurrently if not exists idx_raw_documents_parent_raw_document_id_partial
    on app.raw_documents (parent_raw_document_id)
    where parent_raw_document_id is not null;

create index concurrently if not exists idx_raw_documents_ingestion_failed_partial
    on app.raw_documents (ingestion_failed_at, id)
    where ingestion_error_code is not null;

-- +goose StatementBegin
do $$
begin
    if exists (
        select 1 from pg_index index_state
        join pg_class index_name on index_name.oid = index_state.indexrelid
        where index_name.relname in (
            'idx_raw_documents_entry_sha256',
            'idx_raw_documents_source_entry_first_seen_at',
            'idx_raw_documents_parent_raw_document_id_partial',
            'idx_raw_documents_ingestion_failed_partial'
        ) and (not index_state.indisvalid or not index_state.indisready)
    ) then
        raise exception 'a source-entry index is invalid; drop it concurrently and retry migration';
    end if;
end $$;
-- +goose StatementEnd
