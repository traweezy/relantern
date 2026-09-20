-- +goose Up
set local lock_timeout = '5s';

-- Every stored official advisory collection fetch is an observation, even
-- when its bytes match an earlier raw document. The fetch ledger is retained
-- for only 90 days, so source_fetch_id is an immutable idempotency key rather
-- than a foreign key to that operational table. IDs order observations only
-- after the collector serializes writes on the source row.
-- Processed means all entries in this collection have been recorded and their
-- advisory assessment effects completed. Delivery remains a separate queue.
-- Keep a later observation pending behind an earlier failed assessment.
create table app.advisory_collection_observations (
    id bigint generated always as identity primary key,
    source_id text not null,
    source_registry_id text not null,
    source_fetch_id bigint not null,
    parent_raw_document_id uuid not null,
    observed_at timestamptz not null,
    state text not null default 'pending',
    entry_count integer,
    split_completed_at timestamptz,
    processed_at timestamptz,
    created_at timestamptz not null default now(),
    constraint advisory_collection_observations_source_id_fkey
        foreign key (source_id) references app.sources (id) on delete cascade,
    constraint advisory_collection_observations_parent_raw_document_id_fkey
        foreign key (parent_raw_document_id) references app.raw_documents (id) on delete cascade,
    constraint advisory_collection_observations_source_fetch_id_key
        unique (source_fetch_id),
    constraint advisory_collection_observations_source_registry_id_check
        check (length(source_registry_id) between 1 and 255),
    constraint advisory_collection_observations_source_fetch_id_check
        check (source_fetch_id > 0),
    constraint advisory_collection_observations_state_check
        check (state in ('pending', 'processed')),
    constraint advisory_collection_observations_split_check check (
        (entry_count is null and split_completed_at is null)
        or (entry_count between 0 and 500 and split_completed_at is not null
            and split_completed_at >= observed_at)
    ),
    constraint advisory_collection_observations_completion_check check (
        (state = 'pending' and processed_at is null)
        or (state = 'processed' and entry_count is not null
            and split_completed_at is not null and processed_at is not null
            and processed_at >= split_completed_at)
    )
);

create index idx_advisory_collection_observations_source_id
    on app.advisory_collection_observations (source_id, id);

create index idx_advisory_collection_observations_pending
    on app.advisory_collection_observations (source_id, id)
    where state = 'pending';

create index idx_advisory_collection_observations_parent_pending
    on app.advisory_collection_observations (parent_raw_document_id)
    where state = 'pending';
