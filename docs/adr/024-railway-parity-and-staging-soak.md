# ADR-024: Railway parity and evidence-gated staging soak

Status: Accepted
Date: 2026-08-30
Amended: 2026-09-20

## Context

The local production-like stack already uses the production Docker stages, but
hosted readiness requires an isolated Railway topology and operational evidence
from one frozen release. Railway's current configuration-as-code interface is a
typed SDK; legacy Railway configuration files cannot create the complete
project graph. Production must also remain impossible to activate accidentally
while staging is being prepared.

The existing Railway project contains distinct `staging` and `production`
environments with private networking enabled. At this decision date neither
environment contains a service, volume, or bucket, so no hosted soak has begun.

## Decision

- Pin `railway` 3.10.0 and define the complete graph in
  `.railway/railway.ts`: PostgreSQL with pgvector and a private volume, one
  private bucket, and `migrate`, `api`, `worker`, and `web` services.
- Source staging application services only from `staging` and production
  application services only from `master`. Railway must wait for GitHub check
  suites. Every Dockerfile and database image is exact and reviewable.
- Give `migrate`, `api`, `worker`, and `web` the same reviewed runtime-input
  watch patterns. A code or infrastructure change must queue all four at one
  SHA; documentation and evidence-only commits do not redeploy applications.
  Bound API, worker, and web readiness to 600 seconds to allow parallel builds,
  migration, and the eight-minute schema wait before declaring a rollout failed.
- Add forward migration 36 for `app.migration_completions`, keyed by a full
  release SHA and exact Goose version. The one-shot migrator records
  completion only after River and source-registry work succeed. A same-SHA
  rerun preserves its prior successful marker so existing replicas remain
  available. API and worker wait for this row before serving or starting jobs
  and recheck it for readiness. Local and test may use the literal `unknown`
  SHA; hosted environments require a full lowercase SHA. This closes the gap
  between Goose finishing and the complete migration job finishing, even when
  SQL versions do not change across releases.
- Hold a PostgreSQL advisory lock through each complete `migrate up` run, with
  bounded acquisition and session release on every exit. The migrator uses a
  direct PostgreSQL connection or session pooling. Transaction-pooled
  PgBouncer cannot support this lock. Normal staging releases wait for the
  prior migration to finish and the prior application to become ready before
  the next merge. Incident recovery may supersede an unhealthy release after
  its migration job is terminal and worker intake is disabled. Overlapping
  deployment generation fencing is deferred; the lock serializes jobs but
  cannot infer Git ancestry from opaque SHAs.
- Keep database, API, worker, migration, and object storage private. A public
  domain is a separately reviewed platform control and may exist only for
  `web`; public TCP proxies are forbidden for every service.
- Preserve all secret values for attended, out-of-band entry and seal every
  hosted copy. Railway sealed variables cannot be unsealed, retrieved, or used
  as cross-service references, so database URLs, the internal service token,
  and the OpenAI API key are entered identically at each consumer in one
  maintenance window. Never print variables from an audit command.
- Stage with distinct provider credentials, a test delivery destination, and a
  low OpenAI cap. Keep production delivery, extraction, and research disabled
  until the production launch procedure explicitly enables them.
- Do not expose an infrastructure apply or production deploy Make target.
  `make railway-plan` is read-only and rejects destructive drift.
  `make railway-readiness` additionally requires a zero-change plan, exact
  service graph, private networking, successful deployments at one full Git
  SHA, and the web-only public boundary.
- Record P9 in a strict versioned JSON ledger. Every daily observation and
  recovery exercise is bound to the frozen full SHA and at least one durable
  SHA-256 evidence reference. `soakctl` computes `pass`, `extend`, `fail`, or
  `in_progress`; release mode succeeds only for `pass`.

## Consequences

Infrastructure drift becomes reviewable and testable without modifying the
hosted project. A template, a short run, a changed SHA, missing daily evidence,
or an incomplete exercise cannot be mistaken for a successful soak. A security
boundary failure, lost evidence, unbounded provider cost, repeated critical
miss, or hard-cap overrun fails immediately.

Migration 36 is additive and has a five-second schema lock timeout. Rollback
deploys a prior compatible application SHA while retaining migration history,
the hosted database, bucket, volume, and evidence. Database schema changes
remain forward-only, and restore is reserved for proven corruption.

This amendment does not apply Railway configuration. Staging activation
requires a successful migration job and verified application readiness.
Public-domain creation and the soak clock remain attended operations after
the local gates.
