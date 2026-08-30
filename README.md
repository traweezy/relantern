# Relantern

Relantern is a private, single-owner developer-intelligence platform. It
continuously gathers evidence from an explicit source registry and produces a
concise, personalized morning brief. Deterministic code owns coverage,
freshness, scheduling, deduplication, delivery, and cost limits; bounded AI
stages extract and synthesize claims only after evidence has been stored.

The implementation authority is
[the build specification](docs/PERSONAL_DEVELOPER_INTELLIGENCE_PLATFORM_BUILD_SPEC.md).

## Current phase

The reproducible foundation, database/API skeleton, parallel CI graph, reviewed
source registry, bounded fetch/storage and parser/revision boundaries, and the
PR 6 River queue plus durable local-time scheduler are implemented. PR 7 adds
evaluated URL/hash/metadata/SimHash deduplication, typed item provenance, and
deterministic story clusters. PR 8 adds immutable model-versioned embeddings,
evaluated semantic clustering, PostgreSQL full-text/trigram/vector retrieval,
reciprocal-rank fusion, and a typed re-embedding maintenance job. Live source
ingestion and hosted provider access remain intentionally disabled pending the
separately reviewed rollout phases. PR 9 adds bounded fast extraction through
the official OpenAI Responses SDK, strict versioned structured outputs,
deterministic evidence-span validation, immutable claim provenance, fixed-point
cost accounting with a hard stop, and a zero-network promotion eval harness.
PR 10 adds evidence-only research synthesis, bounded domain-filtered web search,
durable background Responses, verified public webhooks, private transactional
enqueue, and missed-webhook reconciliation. Hosted provider access remains
disabled by default. PR 11 adds Better Auth, numeric GitHub owner enforcement,
database-backed revocable sessions, an S256 PKCE local OAuth fixture, exact
public-route policy, and the first protected application shell. PR 12 adds the
private Today and Live read models, an evidence/provenance Story reader, a
responsive owner workspace, and an anonymous fixture-only demonstration with
local resettable triage. Strict nonce CSP keeps every document request-rendered;
the demo remains independent of auth, the database, APIs, SSE, and providers.
PR 13 adds independent Inbox/Later/Archive state, Starred and Snoozed views,
optimistic command triage, atomic bulk actions and Later ordering, server-backed
ten-second Undo, owner tags, revision-bound notes and verified highlights, and
state-aware Today filtering. PR 14 adds authenticated hybrid Search with durable
exact-filter shortcuts, source-verified Releases and Coming Soon, bounded URL
capture through the normal ingestion pipeline, approval-gated OPML import, and
OPML, JSON, CSV, and selected-story Markdown exports. The default local profile
remains disconnected from live providers and delivery APIs.

## Safe local workflow

Prerequisites are Docker Engine/Desktop with Compose Watch, GNU Make, and Git.
Host Go, Node, and pnpm are optional for editor tooling.

```sh
make doctor
make bootstrap
make dev
```

The default stack binds only to loopback and uses fake source, OpenAI, and
delivery providers. It cannot contact live delivery endpoints.

Useful targets:

```sh
make ps
make logs
make test
make auth-smoke
make demo-audit
make prepush
make prodlike-smoke
make stop
```

`make workflow-lint` validates all GitHub workflow and local composite-action
files with the pinned actionlint release. CI warms the pnpm and Go caches in
setup gates, then runs the independent frontend and Go checks in parallel. See
[ADR-011](docs/adr/011-parallel-github-actions.md) for the graph and plan-aware
security gates.

`make sources-verify` performs strict, zero-network validation of 91 reviewed
source endpoints, the deterministic fixture catalog, and every connector parser
scenario. The forward-migration release command mirrors built-ins into
PostgreSQL as paused entries; no poll is scheduled while the registry network
fuse is off. See
[ADR-012](docs/adr/012-reviewed-source-registry.md).

`make test-integration` validates additive migrations, checkpoint persistence,
normalized revision chains, the safe worker path, and staged content-addressed
object operations against the loopback-only PostgreSQL and MinIO services. It
also races two schedule reconcilers, verifies transactional River enqueue,
audited retry, idempotent fake delivery, and concurrent dedupe processors. No
publisher endpoint is contacted. `make test-dedupe` runs the zero-network
labeled exact, SimHash, and embedding precision fixtures. `make test-search`
validates the deterministic fake embedding client and hybrid retrieval quality
fixture. `make test-extraction` validates strict response schemas, grounding,
critical-security recall, retry behavior, and prompt-injection resistance
without network access or API spend. `make test-research` validates the
research schema, web-search/tool boundary, returned-source provenance,
background retries, durable webhook deduplication, and database hard stops. See
[ADR-013](docs/adr/013-bounded-fetch-and-raw-storage.md) and
[ADR-014](docs/adr/014-parser-normalization-and-revisions.md), and
[ADR-015](docs/adr/015-river-schedule-execution.md), and
[ADR-016](docs/adr/016-deterministic-dedupe-and-clusters.md), and
[ADR-009](docs/adr/009-postgresql-hybrid-search.md), and
[ADR-007](docs/adr/007-bounded-openai-pipeline.md), and
[ADR-017](docs/adr/017-bounded-research-background-webhooks.md).

Discarded jobs are private operational data. Inspect them with
`docker compose run --rm worker dead-letters`; use the audited retry command
only after following [the queue recovery runbook](docs/runbooks/queue-recovery.md).
Review budget blocks, provider retries, or `needs_review` extraction runs with
[the AI extraction runbook](docs/runbooks/ai-extraction.md).
For background delivery, use
[the webhook reconciliation runbook](docs/runbooks/webhook-reconciliation.md);
for either cost boundary, use
[the OpenAI budget runbook](docs/runbooks/openai-budget-exhausted.md).
For owner lockout, credential rotation, or session revocation, use
[the owner authentication runbook](docs/runbooks/owner-authentication.md).
The private intelligence and anonymous fixture dependency boundaries are
recorded in
[ADR-020](docs/adr/020-private-intelligence-and-demo-boundaries.md). Reading
semantics and recovery are recorded in
[ADR-018](docs/adr/018-independent-reading-state.md),
[ADR-019](docs/adr/019-command-driven-triage-and-undo.md), and the
[bulk mutation](docs/runbooks/bulk-state-mutation-recovery.md) and
[snooze recovery](docs/runbooks/snoozed-item-not-returned.md) runbooks. Manual
capture and source-list recovery are covered by the
[capture and OPML runbook](docs/runbooks/manual-capture-and-opml.md); migration
ordering and rollback constraints are recorded in the
[discovery portability migration notes](docs/migrations/000016-discovery-portability.md).

`make reset` is destructive and requires confirmation. It targets only the
validated Relantern Compose project and its volumes.

## Service boundaries

- `web`: the only public application service; Next.js UI and BFF boundary.
- `api`: private Huma/chi API.
- `worker`: private River execution, scheduling, intelligence, and delivery worker.
- `migrate`: one-shot forward migration service.
- `postgres`: PostgreSQL 18.6 with pgvector 0.8.6.
- `minio`: local-only S3-compatible development storage.
- fake providers: development/test-only source, OpenAI, and delivery surfaces.

No secret belongs in Git, a `NEXT_PUBLIC_*` variable, an APK, or a browser
bundle. See `.env.local.example` and `docs/runbooks/rotate-secrets.md` as those
artifacts are introduced.
