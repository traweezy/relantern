# ADR-024: Railway parity and evidence-gated staging soak

Status: Accepted
Date: 2026-08-30

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
- Keep database, API, worker, migration, and object storage private. A public
  domain is a separately reviewed platform control and may exist only for
  `web`; public TCP proxies are forbidden for every service.
- Generate and seal internal database, auth, webhook-signing, and service-token
  secrets per environment. Preserve external credentials for attended,
  out-of-band entry; never print variables from an audit command.
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

This change has no database migration. Before the first infrastructure apply,
rollback is a normal code revert. After a hosted database, bucket, or volume
contains evidence, rollback is application-first to a prior compatible SHA;
those durable resources are never deleted as a code rollback. Database schema
changes remain forward-only, and restore is reserved for proven corruption.

The Railway project is not modified by this decision. Deployment, provider
credential entry, public-domain creation, and the soak clock require a later
attended operation after all local gates pass.
