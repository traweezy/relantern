# ADR-016: Deterministic deduplication and story clusters

Status: Accepted
Date: 2026-08-29

## Context

Normalized revisions are durable evidence, but feeds, release pages, repository
tags, and community sources often report the same change. Publishing every
observation would create repetitive inbox and digest entries. A model is not an
acceptable first-line identity authority: deduplication must be reproducible,
explainable, inexpensive, and safe to replay before embeddings or OpenAI are
enabled.

The same source URL can also publish a corrected revision. That is an update to
an existing item, not a second story. Distinct reports about the same package
release belong in one story cluster while retaining their own item and evidence
identity.

## Decision

- Canonicalize absolute HTTP(S) URLs by normalizing host IDNs and default ports,
  removing fragments and a reviewed tracking-parameter allowlist, sorting query
  values, and rejecting user information. A publisher `rel=canonical` is
  accepted only inside the same registrable domain or an explicit host-alias
  pair; an invalid or foreign canonical falls back to the fetched URL.
- Evaluate candidates in fixed order: same-source revision, canonical URL,
  exact raw SHA-256, exact normalized SHA-256, normalized metadata, and SimHash.
  Exact evidence always wins over probabilistic signals.
- Normalize title, author, package, and version metadata with Unicode NFKC and
  stable token boundaries. Metadata dedupe requires the same non-empty title
  plus either the same author or the same package and version. Conflicting
  explicit package or version metadata disables SimHash matching.
- Use a deterministic 64-bit SimHash over normalized word and adjacent-word
  features. The distance of 17 is selected from the committed labeled fixture,
  not intuition. The current 24-case set achieves 100 percent precision and
  91.7 percent recall; promotion requires at least 98 percent precision and 90
  percent recall. Any fixture or feature change must reselect and review the
  constant.
- Limit near-duplicate and cluster candidates to 30 days. Exact URL and digest
  candidates remain eligible outside the time window. Bound every candidate
  read to 2,000 rows for predictable personal-scale cost.
- Treat a same-source canonical observation as a revision. Attach exact,
  metadata, and SimHash matches to one item as duplicate evidence. Create a new
  item in an existing cluster only when non-empty package and version metadata
  match inside the age window; semantic clustering remains PR 8 scope.
- Pick primary evidence and the cluster primary deterministically by source tier
  (`T0` through `T3`), first-seen time, then UUID. A newly observed higher-tier
  duplicate can become the item's current primary revision without deleting the
  previous evidence. Items backed only by `T2` or `T3` evidence remain
  `needs_review`; they cannot enter later publish paths until a `T0` or `T1`
  primary arrives or an authenticated owner workflow explicitly decides them.
- Persist typed `items`, `item_sources`, `story_clusters`, `cluster_members`, and
  one `dedupe_decisions` row per revision. Record outcome, method, candidate,
  similarity, optional Hamming distance, evaluated configuration, and timestamp.
  Preserve the canonical identity on every item-source revision so promoting a
  higher-tier primary never breaks a later revision from another source.
- Serialize candidate reads and graph writes with one transaction-scoped
  advisory lock. Use read-committed transactions deliberately so a waiter sees
  the lock holder's committed decision; this makes concurrent reprocessing
  converge on one item and one cluster instead of retaining a pre-wait snapshot.

## Reliability, performance, and observability

The target is an idempotent decision for every normalized revision with no
silent drop. Database constraints enforce one item source and decision per
revision and one cluster per item. The global lock favors correctness at the
current personal corpus size; split locks or partitioned candidate indexes
require measured contention before adoption.

Unit tests cover canonical boundaries, matching precedence, metadata conflicts,
trust-tier tie-breaking, and the labeled precision fixture. PostgreSQL tests
cover create, exact duplicate, corrected revision, deterministic cluster,
unrelated story, replay, invalid boundaries, graph invariants, and two concurrent
processors.

## Migration and rollback

Migration `000006_dedupe_clusters.sql` is additive and forward-only in hosted
environments. Application rollback can stop enqueueing/processing new dedupe
work while retaining the graph and provenance tables. The Goose down section is
for disposable local/test databases only; production rollback never drops
evidence, items, decisions, or clusters.

## Consequences

Later ranking, search, AI, and delivery stages receive one explainable item or
story cluster instead of repeated source observations. Revision and duplicate
evidence remain queryable, and a future evaluator can reproduce every decision
without a network or model call.

This change does not create embeddings, hybrid search, source polling, OpenAI
requests, external delivery, public APIs, or Railway deployments. Those remain
behind their later specification phases and existing safety fuses.
