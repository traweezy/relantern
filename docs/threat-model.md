# Relantern Version 1 threat model

Status: reviewed for PR 18
Review date: 2026-08-30
Scope: web, private API, worker, PostgreSQL, object storage, provider adapters,
delivery, CI, public demo, backup, restore, and operator workflows.

## Security objectives

- Only the configured numeric GitHub owner may access private intelligence or
  mutate configuration.
- Only `web` accepts public application traffic; the browser never receives an
  internal service credential or provider secret.
- Every material claim retains primary evidence and model/run provenance.
- Untrusted network content cannot select tools, destinations, credentials, or
  executable behavior.
- Delivery and durable jobs are idempotent; recovery does not duplicate owner
  notifications or lose evidence.
- `/demo` remains useful with every private service unavailable and cannot
  cross into an authenticated or live-data boundary.
- Backups are confidential, integrity checked, and restorable within the
  initial 24-hour RPO and four-hour RTO.

## Assets and trust boundaries

High-value assets are the owner session, GitHub identity allowlist, internal
web-to-API token, OAuth and provider credentials, private notes/highlights,
raw and normalized evidence, claims, digests, audit records, database backups,
and build/release identities.

Trust boundaries are:

1. Anonymous browser to public `web` routes.
2. Authenticated browser to server components and same-origin BFF routes.
3. `web` to the private Go API using owner identity plus a service credential.
4. Worker to PostgreSQL, object storage, source hosts, OpenAI, Discord, and
   Resend through independently gated adapters.
5. GitHub-hosted CI to immutable source, registries, and artifact storage.
6. Operator workstation to GitHub and Railway control planes.
7. Backup destination to an isolated temporary restore database.

## Threat analysis and controls

| Threat | Boundary | Required control | Verification |
|---|---|---|---|
| Owner impersonation or account enumeration | browser/auth | numeric allowlist, neutral errors, secure cookie, session rotation, CSRF/origin checks | auth unit and smoke tests |
| Service-token theft or confused deputy | web/API | private network, constant-time credential check, owner header required, no browser serialization | API authorization tests and secret scan |
| Demo-to-private route confusion | anonymous web | exact normalized route grammar, data-boundary auth, static fixture adapter, forbidden imports and request graph | `make demo-audit` |
| SSRF, rebinding, redirect escape, decompression bomb | worker/source | HTTPS and port policy, resolved-address validation, redirect revalidation, proxy denial, body and ratio limits | fetcher unit/fuzz tests |
| Prompt/tool injection | evidence/AI | treat content as data, fixed schema, no tools in fast lane, domain allowlist and bounded web search in research lane, evidence spans before publication | extraction/research eval suites |
| Unsupported or malicious claim publication | AI/product | deterministic validation, primary-source requirement, review state, immutable claim provenance | claim/evidence tests and review runbook |
| Queue replay or duplicate delivery | worker/provider | River uniqueness, database occurrence keys, immutable payload hash, provider idempotency key, explicit delivery fuse | scheduler and digest integration tests |
| Cross-environment delivery | worker/provider | distinct credentials and recipients, hosted capture rejection, exact provider hosts, staging-only recipient review | config check and staging exercise |
| SQL injection or tenant crossover | API/database | parameterized SQL, owner ID on private reads/mutations, constraints, single-owner authorization boundary | repository tests and CodeQL |
| Evidence deletion through retention | worker/storage | published/claimed evidence exclusion, bounded candidates, delete-then-conditional-mark idempotency, immutable retention ledger | retention tests and runbook |
| Backup disclosure or unusable restore | database/backup | mode 0600, checksum, encrypted off-platform copy in hosted environments, isolated monthly restore, RPO/RTO ledger | `make restore-drill` |
| Secret disclosure in source/logs/artifacts | CI/operator | ignored secret files, redacted structured logs, gitleaks history scan, no secret output in runbooks | security workflow |
| Compromised dependency, action, or base image | supply chain | exact versions, lockfiles, seven-day floor, full action SHAs, image digests, OSV/Trivy/CodeQL/dependency review, SPDX SBOM | CI and nightly workflows |
| Workflow token escalation or expression injection | CI | read-only default permissions, job-scoped exceptions, no persisted checkout credentials, zizmor | workflow security scan |
| Resource exhaustion or provider-cost abuse | all | strict sizes, timeouts, bounded concurrency, queue backpressure, AI hard cap, rate limits, retry ceilings | config and integration tests |
| Destructive migration or unsafe rollback | database/release | forward-only hosted migrations, expand/backfill/contract, application-first rollback, restore only for proven corruption | migration docs and release runbook |

## Abuse cases reviewed

- Encoded slash, backslash, dot-segment, mixed-case, duplicate-slash, and
  unknown fixture paths do not become public demo routes.
- A source document asking the model to reveal secrets, invoke tools, ignore
  policy, or fetch an arbitrary URL is inert input and cannot change tool
  authority.
- A provider timeout cannot cause digest regeneration; retries use the exact
  stored bytes, hash, and idempotency key.
- A stale or replayed owner mutation cannot overwrite newer state because
  version and idempotency checks are transactional.
- A retention replay cannot delete outside validated content-addressed object
  namespaces and cannot prune evidence backing a published item or claim.
- A restore drill cannot target `relantern`; its generated database name must
  match `relantern_restore_[0-9A-Za-z_]+` and is dropped on exit.

## Residual risks and acceptance

- Railway and external providers remain trusted control planes. Independent
  credentials, least privilege, audit evidence, and kill switches reduce but
  do not eliminate provider compromise risk.
- Incoming Discord webhooks do not provide a universal provider-side
  idempotency guarantee. The database ledger and immutable retry procedure are
  authoritative; ambiguous acceptance requires manual verification.
- The initial RPO is 24 hours until production operation is trusted and PITR or
  hourly off-platform backups are verified. This is an explicit launch gate,
  not a claim of current hosted readiness.
- Railway baseline metrics are sufficient for staging; an external OTLP
  backend remains deferred by owner decision. Private worker metrics and alert
  evaluation are available for scraping and checks.
- No autonomous code execution, repository writes, package installation, or
  production modification is in Version 1. Adding any requires a new threat
  model and promotion phase.

## Review conclusion

The modeled Version 1 boundaries have a concrete preventive, detective, and
recovery control. Remaining hosted assumptions must be proven during the
14-day Railway staging soak. A demo isolation failure, lost evidence,
unaccepted high/critical vulnerability, cross-environment delivery, or failed
restore objective blocks production promotion.
