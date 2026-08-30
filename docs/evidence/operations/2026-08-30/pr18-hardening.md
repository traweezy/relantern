# PR 18 security and operational hardening evidence

Date: 2026-08-30

Scope: the local `staging` PR 18 change set. No Railway deployment, live
delivery, live OpenAI request, or GitHub write was performed.

## Acceptance results

| Requirement | Result | Evidence |
|---|---|---|
| threat-model review complete | PASS | `docs/threat-model.md` and ADR-023 define assets, trust boundaries, threats, controls, residual risks, retention, and rollback |
| no unaccepted high/critical vulnerability | PASS | OSV, Go 1.27-built govulncheck, Trivy, pnpm audit, gitleaks, and zizmor completed with no unaccepted high/critical result |
| restore drill meets RPO/RTO | PASS | migration 20 restored into an isolated database; RPO 0s/86400s and RTO 3s/14400s |
| secret rotation drill succeeds | PASS | the API accepts the prior credential before rotation, rejects it after rotation, and accepts the rotated credential |
| incident exercises completed | PASS | restore, compromised-dependency triage, credential rotation, alert threshold, and demo isolation exercises completed locally |

## Security and supply-chain results

- `make security-scan`: PASS.
  - OSV-Scanner 2.5.1 found no unaccepted issues.
  - govulncheck 1.7.0, compiled by the pinned Go 1.27 toolchain, found zero
    reachable vulnerabilities.
  - Trivy 0.74.0 found zero high/critical dependency vulnerabilities and zero
    Dockerfile misconfigurations.
  - zizmor 1.29.0 found no workflow issues; one documented `GITHUB_PATH`
    write in the local setup action is narrowly ignored.
- `GO-2026-5932` is a time-bounded scanner exception through 2026-11-30.
  The advisory affects only the unmaintained `golang.org/x/crypto/openpgp`
  packages. `go list -deps ./...`, `go mod why`, and govulncheck prove those
  packages and symbols are not used. `osv-scanner.toml` records the reason and
  expiry; the exception does not suppress any other `x/crypto` advisory.
- `gitleaks git --redact --no-banner`: PASS across 37 commits. Eight exact
  fingerprints in `.gitleaksignore` are synthetic test idempotency keys or
  disconnected provider credentials. No file- or rule-wide exclusion exists.
- `pnpm audit --audit-level high`: PASS with no known vulnerabilities.
- `pnpm audit signatures`: PASS for 249 packages.
- `make sbom`: PASS. Syft 1.51.0 produced an SPDX 2.3 repository SBOM with
  1,187 packages at `dist/sbom/repository.spdx.json`; the ignored local
  artifact SHA-256 was
  `a90cbe842990ac03ec025187c59d41684b70776a79ff29d6efb24d6fa251e7d8`.

## Restore and retention evidence

- `make restore-drill`: PASS at `2026-08-30T04:27:42Z`.
- Backup SHA-256:
  `47922676e5a4b1ec1835238719ce8938738a149994a50ad88c166642aebdc6eb`.
- Restored migration: 20.
- Verified row counts: 61 sources, 8 users, 3 schedules, 4 audit events,
  and 1 digest.
- The drill report is an ignored local artifact at
  `dist/restore-drills/20260830T042738Z.json`; the durable database ledger is
  the hosted evidence authority.
- The first same-day retention attempt failed closed on a schema-name defect
  before deleting data. The corrected retry reused the date idempotency key,
  completed successfully, and retained accurate partial-progress accounting.
- Published stories and claim-bearing revisions are excluded from object
  pruning. Object deletion is content-addressed, rejects staging keys, and is
  idempotent. Operational pruning and mutation-state compaction are bounded.

## Incident exercise evidence

| Exercise | Result | Observed containment or recovery |
|---|---|---|
| database restore | PASS | checksum-verified dump restored only into a generated temporary database, verified, recorded, and dropped |
| credential rotation | PASS | prior service token became unauthorized after rotation while the new token remained valid |
| compromised dependency | PASS | a newly published module-level OpenPGP advisory was detected, traced to unused packages, independently call-analyzed, and given an expiring narrow exception |
| alert threshold | PASS | source freshness, queue age, digest delay, retention failure, and restore RPO/RTO/staleness fixtures produced the documented alerts |
| public demo isolation | PASS | 65 web tests and the production build verified normalization and forbidden imports without emitted private demo HTML |

## Quality and runtime gates

- `make prepush`: PASS after correcting a stale Operations restore-status test.
  The uninterrupted rerun passed Biome, gofmt, vet, typecheck, unit and
  database/object integration tests, generation drift, config and Railway
  parity, source fixtures, repository policy, Go race tests, and the Next.js
  production build.
- The first two all-at-once `make prodlike-smoke` attempts exhausted this
  workstation's memory while BuildKit compiled Go services concurrently.
  No product assertion failed. The exact service stages were then built one at
  a time with `docker compose build <service>`, started with `up --no-build`,
  and passed the normal stack wait, web/API/fake-delivery health probes,
  disconnected owner OAuth/session/sign-out smoke, private worker `/metrics`,
  and private worker `/alerts` validation.
- The local production-like build used Go 1.27.0, Node 26.8.1, pnpm 11.24.0,
  and the immutable base-image digests in `docs/version-manifest.md`.

## Residual release gate

PR 18 is complete locally. Production remains blocked until PR 19 records at
least 14 consecutive days of Railway staging soak evidence and PR 20 records
the final release decision and immutable artifact evidence.
