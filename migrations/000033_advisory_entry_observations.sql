-- +goose Up
set local lock_timeout = '5s';

-- A collection can contain the same provider entry more than once. Preserve
-- each ordered encounter, including byte-identical repeats, so a later
-- critical -> corrected -> critical sequence is never hidden by raw dedupe.
create table app.advisory_entry_observations (
    id bigint generated always as identity primary key,
    collection_observation_id bigint not null,
    entry_ordinal integer not null,
    source_entry_id uuid not null,
    child_raw_document_id uuid not null,
    state text not null default 'pending',
    processed_at timestamptz,
    created_at timestamptz not null default now(),
    constraint advisory_entry_observations_collection_observation_id_fkey
        foreign key (collection_observation_id)
        references app.advisory_collection_observations (id) on delete cascade,
    constraint advisory_entry_observations_source_entry_id_fkey
        foreign key (source_entry_id) references app.source_entries (id) on delete cascade,
    constraint advisory_entry_observations_child_raw_document_id_fkey
        foreign key (child_raw_document_id) references app.raw_documents (id) on delete cascade,
    constraint advisory_entry_observations_collection_ordinal_key
        unique (collection_observation_id, entry_ordinal),
    constraint advisory_entry_observations_entry_ordinal_check
        check (entry_ordinal between 0 and 499),
    constraint advisory_entry_observations_state_check
        check (state in ('pending', 'processed')),
    constraint advisory_entry_observations_completion_check check (
        (state = 'pending' and processed_at is null)
        or (state = 'processed' and processed_at is not null)
    )
);

create index idx_advisory_entry_observations_source_entry_order
    on app.advisory_entry_observations
    (source_entry_id, collection_observation_id desc, entry_ordinal desc);

-- Existing alerts are episode one. PostgreSQL stores this constant default
-- in metadata, avoiding a rewrite of the existing alert table. New evidence
-- links are nullable until each alert path is converted to the event ledger.
alter table app.critical_alerts
    add column episode_number integer not null default 1,
    add column opening_observation_id bigint,
    add column correction_observation_id bigint;

alter table app.critical_alerts
    add constraint critical_alerts_episode_number_check
        check (episode_number > 0) not valid,
    add constraint critical_alerts_opening_observation_id_fkey
        foreign key (opening_observation_id)
        references app.advisory_entry_observations (id) on delete restrict not valid,
    add constraint critical_alerts_correction_observation_id_fkey
        foreign key (correction_observation_id)
        references app.advisory_entry_observations (id) on delete restrict not valid;
