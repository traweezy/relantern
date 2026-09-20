# ADR-014: Bounded parsing, normalization, and revision provenance

Status: Accepted
Date: 2026-08-29

## Context

Immutable HTTP bodies are evidence, but their feed markup, page chrome,
character encodings, provider shapes, hidden controls, and embedded
instructions are not a safe or stable input to later deduplication and
intelligence stages. A publisher can also correct, expand, or revert a document
at the same URL. Those observations must remain distinguishable from retrying
the same raw input.

Live polling, generalized deduplication, story creation, and AI processing are
still outside this boundary. Parsing must therefore be deterministic,
zero-network, independently bounded, and durable without opening a new provider
or scheduling path.

## Decision

- Parse the reviewed Atom, RSS, and JSON Feed connectors with gofeed; parse page
  content through the pinned readeck readability adapter; and use explicit JSON
  adapters for GitHub releases, GitHub advisories, package registries, and the
  reviewed structured API shape.
- Reapply connector-specific decoded-body limits after fetch storage, require an
  absolute fragment-free HTTP(S) source URL, reject non-UTF-8 JSON, and decode
  declared legacy feed/page encodings to UTF-8.
- Remove scriptable, form, embedded, hidden, and event-handler content. Treat
  prompt-like instructions as untrusted evidence, remove them from normalized
  model input, and retain a durable warning while immutable raw evidence remains
  available.
- Normalize line endings and Unicode to NFC. Preserve headings, paragraphs,
  lists, table rows, code blocks, source-relative anchors, a normalized outline,
  and an offset map. Record publisher dates separately from observation time and
  use `und` when language cannot be determined safely.
- Hash normalized UTF-8 text with SHA-256 and stage it through the existing
  object-storage adapter before promoting it to
  `normalized/{source_id}/{sha256}.txt`. The adapter accepts only reviewed,
  content-addressed raw and normalized key shapes.
- Persist every parse attempt with a stable outcome, warning list, duration, and
  error code. Persist successful content revisions with parser version, outline,
  offset map, normalized object identity, publisher timestamps, and a link to
  the immediately previous observed revision for the same source URL.
- Treat only the same raw-document ID and normalized hash as an idempotent
  reprocessing result. A later raw document whose normalized content reverts to
  an older hash creates a new material revision and keeps the complete sequence.
- Serialize revision creation per source and canonical fetch URL with a
  transaction-scoped advisory lock. Database metadata may reference normalized
  content only after object promotion succeeds.

## Reliability and observability

Parser failures never become silent drops: they retain a raw-document identity,
parser name/version, stable error code, timestamps, and warnings. The parsing
package is fixture-, unit-, and fuzz-tested above the 80 percent core coverage
floor, and PostgreSQL integration covers initial, unchanged, changed, reverted,
and failed observations.

Object promotion can succeed before the serializable revision transaction
fails. The content-addressed key makes retry safe and allows a later maintenance
job to remove unreferenced objects after a retention window. The reverse state
is forbidden: the database never records an object that was not promoted.

## Consequences

Every normalized revision is reproducible from immutable evidence and remains
inspectable before any model sees it. Parser warnings can feed Source Health in
a later UI phase. gofeed, readeck readability, and the directly used Go text and
HTML modules remain exactly pinned, scanned, and recorded in the supply-chain
manifest.

This change does not enable the source-registry network fuse, schedule polling,
create items or story clusters, call OpenAI, deliver notifications, or deploy
Railway. Queue wiring is owned by ADR-015; the deterministic downstream
deduplication and clustering boundary is owned by ADR-016.

## Subsequent source-intelligence handoff

The later staging integration connects parsed leaf revisions to ADR-016's
deterministic item/cluster decision. In one database transaction, the handoff
checks raw-document, source, revision, dedupe, item, and cluster identities;
locks the raw document and item; and confirms the revision is the item's current
primary source before inserting downstream jobs. It acknowledges the raw parse
marker in that same transaction. On a non-cancelled handoff error, the worker
records `source_handoff_failed` for replay and returns both errors if that marker
write also fails.
Repeated handoff is idempotent; an older or nonprimary revision cannot queue
work for the current story.

The worker passes explicit capabilities to the handoff. It queues
`reembed_entity` only when the current search document lacks an embedding pin
for the configured model and an embedding worker exists. Reconciliation links
an existing immutable vector when its exact content digest matches, avoiding a
duplicate provider call for identical normalized revisions. It queues
`extract_item` only for a clustered current primary T0/T1 item when fast
extraction is enabled. A current primary revision from a T2/T3 source moves
the item to `needs_review` and cannot enqueue fast extraction. Research requires
verified T0/T1 claims and admission to the owner's bounded high-value digest
selection before its cutoff; already queued work may start during the
ten-minute completion grace. Embedding and extraction job identities include
the revision; the embedding projection version permits repairing a missing pin
after an older completed job. Research job identity also includes the validated
research input hash, so a changed verified claim from a supporting source can
trigger new synthesis even when the primary revision is unchanged. Existing
jobs remain idempotent for the same inputs.

The source reconciler reserves a bounded 100-job batch for eligible current
revisions, split across enabled embedding, extraction, and research stages. It
scores a bounded 500-candidate digest slice and compares exact research facts
for admitted stories. A rotating cursor prevents older unchanged stories from
hiding later updates; claim timestamps do not decide whether evidence changed.
Due source polling is scheduled before intelligence recovery, and each enabled
recovery stage is attempted independently. Reconciliation runs even when live
source polling is disabled; that fuse still controls new
endpoint schedules and network fetches. Disabled downstream capabilities leave
stored evidence for a later reconciliation. Cancelled or discarded River jobs
require the audited operator retry path in the queue recovery runbook.
