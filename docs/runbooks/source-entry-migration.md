# Source entry migration rollout

Migration `000022_source_entries.sql` adds individual feed/API entry identities and
raw-document provenance. It is forward-only and uses Goose `NO TRANSACTION` so
its large-table indexes can build concurrently. Brief column/constraint changes
and the final uniqueness cutover use a 5-second lock timeout. A busy database
may reject one step; rerun the migration after the blocking transaction ends.

The migration first adds nullable fields and `NOT VALID` constraints, then
validates them without blocking ordinary writes. It backfills retained raw
documents from fetch attempts in 500-row transactions. A creator is accepted
only when exactly one stored attempt matches the raw document's source, URL,
digest, first-seen time, and first-fetched time. Rows with missing or ambiguous
matches remain unset for explicit reconciliation. A new fetch fills missing
provenance only when the original stored attempt can still be identified
uniquely. Otherwise the fetched bytes are retained, the endpoint is stopped,
and the raw document is marked `provenance_unresolved`. During rollout,
`LoadRawDocument` applies the same unique-match rule for a legacy row without a
snapshot. Keep operational fetch-attempt retention paused until the backfill
completes and the provenance check below is reviewed.

```sql
select count(*) as missing_provenance
from app.raw_documents raw
where raw.object_key is not null and raw.source_registry_id is null;
```

Review each remaining raw document against its original fetch evidence before
setting provenance. A later fetch that reused the same object key is not proof
of origin. If the original attempt cannot be identified uniquely, leave the
snapshot unset and keep its fetch evidence available for investigation.

The migration verifies each concurrent index before dropping the old broad
URL/digest constraint and before completing. An interrupted concurrent build
can leave an invalid index; `IF NOT EXISTS` does not repair it. If the migration
reports an invalid index, inspect `pg_index.indisvalid` and `indisready`, then
drop only that invalid index with `DROP INDEX CONCURRENTLY` and rerun Goose.
The valid parent partial unique index must stay in place after the old
constraint is dropped. Never remove it during recovery.

```sql
select index_name.relname, index_state.indisvalid, index_state.indisready
from pg_index index_state
join pg_class index_name on index_name.oid = index_state.indexrelid
where index_name.relname in (
  'idx_source_fetches_object_key_partial',
  'idx_raw_documents_parent_source_url_sha256',
  'idx_raw_documents_entry_sha256',
  'idx_raw_documents_source_entry_first_seen_at',
  'idx_raw_documents_parent_raw_document_id_partial',
  'idx_raw_documents_ingestion_failed_partial'
);
```

After rollout, verify a stored feed creates one parent raw document, one
`source_entries` row per external ID, and one child raw document per changed
entry payload. Repeating the same fetch must reuse each child object key and
dedupe decision. Keep the live external API fuse off during migration checks.
