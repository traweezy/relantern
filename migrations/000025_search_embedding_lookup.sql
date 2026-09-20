-- +goose NO TRANSACTION
-- +goose Up
-- Allow the semantic HNSW scan to find its pinned search document by vector ID.
-- A stopped concurrent build can leave an invalid index, so retry by dropping it.
set lock_timeout = '5s';
drop index concurrently if exists app.idx_search_documents_embedding_id;
create index concurrently idx_search_documents_embedding_id
    on app.search_documents (embedding_id)
    where embedding_id is not null;
reset lock_timeout;
