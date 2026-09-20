-- +goose Up
-- Old search rows remain keyword-readable. Reconciliation reindexes them with
-- a proven embedding ID before they participate in semantic search.
set local lock_timeout = '5s';

alter table app.search_documents
    add column embedding_id uuid;

alter table app.search_documents
    add constraint search_documents_embedding_id_fkey
    foreign key (embedding_id) references app.embeddings (id)
    on delete set null not valid;

alter table app.search_documents
    validate constraint search_documents_embedding_id_fkey;
