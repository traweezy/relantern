# ADR-009: PostgreSQL hybrid search

Status: Accepted
Date: 2026-08-29

## Context

Personal-scale retrieval needs exact package and version lookup, typo tolerance,
filtered browsing, and semantic concept search. A separate search service would
add another privileged data copy and operational surface before the expected
corpus or measured latency requires it.

PR 8 also needs semantic candidate clustering without weakening the evaluated
98 percent dedupe-precision floor. Embeddings must remain reproducible, retain
their model lineage, and support a model migration without overwriting the only
known-good vectors.

## Decision

### Storage and model lifecycle

- `app.embedding_models` owns model ID, provider, dimensions, evaluation time,
  activation time, and the `building`, `active`, `inactive`, or `retired`
  lifecycle. A partial unique index permits exactly one active model.
- `app.embeddings` is append-only by entity, model, and content digest. It
  retains revision lineage and checks dimensions, non-zero vector norm, and
  SHA-256 length at the database boundary. Go rejects non-finite coordinates.
- The initial model is `text-embedding-3-small` at 1,536 dimensions. Its cosine
  HNSW index is a partial, dimension-cast index so other model versions can be
  built in parallel without being queried accidentally.
- A replacement model is registered as `building`, populated, evaluated, and
  indexed before a serializable activation transaction makes the old model
  `inactive` and changes the active row. Inactive models remain eligible for an
  evaluated rollback; only explicitly `retired` models are ineligible. The only
  active model is never overwritten.

### Indexing execution path

`ReembedEntity` is typed, unique maintenance work keyed by entity, revision,
and model. The worker reads only a validated content-addressed normalized
object, bounds the read to 1,000,000 bytes, verifies its stored SHA-256, and
uses at most 100,000 UTF-8-safe bytes as embedding input. It then idempotently
persists the immutable vector and updates the item search projection. A job for
a superseded revision exits successfully without changing the current index.

The current repository-safe profile accepts only the loopback or Compose
`fake-openai` HTTP origin. The fake provider produces deterministic fixture
vectors. Live OpenAI access, credentials, and network authority remain fused
off pending the separately reviewed intelligence rollout.

### Retrieval and clustering

`app.search_documents` projects the current item revision, primary source tier,
lifecycle, dates, summary, entities, package, and normalized content. A stored
weighted English `tsvector`, GIN full-text index, and lowercased pg_trgm indexes
support keyword and typo-tolerant retrieval.

Semantic retrieval selects only the latest vector for each item under the
active model. Keyword and cosine candidate lists are combined with reciprocal
rank fusion using constant `k = 60`, followed by stable rank, date, and UUID
tie-breaks. Lifecycle, source-tier, and date predicates apply inside both
candidate branches. The bounded candidate window is 50 to 500 rows and the
store result limit is 1 to 100 rows.

Embedding candidate clustering runs only within the 30-day story window and
rejects conflicting package or version metadata. The promoted cosine threshold
is `0.86`, selected by the reviewed labeled fixture at precision at least 0.98
and recall at least 0.90. Exact, revision, hash, metadata, and SimHash decisions
retain higher precedence.

## Reliability, performance, and observability

- Search has a p95 latency objective below 300 milliseconds at expected owner
  concurrency. PostgreSQL query timing and slow-query logging are the initial
  measurement points; exceeding the objective after plan and index tuning is a
  replacement trigger, not a reason to pre-emptively add a search service.
- Provider calls use an explicit five-second timeout. River applies bounded
  retries and retains discarded jobs for operator inspection and audited retry.
- Re-embedding is safe to replay: vector uniqueness and search upsert make
  retries idempotent, while revision checks prevent stale jobs from regressing
  the projection.
- HNSW construction for a replacement model requires a forward migration and a
  reviewed resource window. Activation follows successful retrieval and
  clustering evaluations.

## Evaluation evidence

- `evals/fixtures/search.json` verifies exact-package and semantic retrieval,
  requiring MRR at least 0.90 and recall at 5 equal to 1.0.
- `evals/fixtures/dedupe-embedding.json` contains reviewed positive and negative
  semantic pairs and deterministically selects the 0.86 clustering threshold.
- PostgreSQL integration tests cover cosine search, reciprocal-rank fusion,
  filters, immutable vectors, model activation, semantic clustering, current
  revision indexing, stale jobs, and content-integrity failure.

## Variables and permissions

New non-secret variables are `FAKE_OPENAI_URL`, `OPENAI_EMBEDDING_MODEL`,
`EMBEDDING_DIMENSIONS`, `SEARCH_HYBRID_ENABLED`, `SEARCH_RRF_K`, and
`DEDUPE_EMBEDDING_THRESHOLD`. The worker receives private-network access only
to PostgreSQL, MinIO, and `fake-openai`; no public route, external credential,
GitHub write permission, delivery authority, or live OpenAI permission is
added.

## Migration and rollback

Migration 7 is additive. It creates model, embedding, and search-projection
tables and indexes, then validates the expanded dedupe method constraint before
swapping constraint names. Application rollback keeps these forward-compatible
tables and stops enqueueing `ReembedEntity`; it does not delete vectors or
search data. A model-quality rollback atomically reactivates a retained
compatible model after evaluation. Production never runs the Goose down
migration.

## Consequences

No separate search service is required initially. PostgreSQL bears additional
vector-index memory and write cost, bounded by the personal corpus and explicit
candidate windows. A replacement requires a measured relevance, latency,
corpus, or database-contention trigger.
