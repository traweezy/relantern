# ADR-023: Operational hardening and evidence-aware retention

Status: Accepted
Date: 2026-08-30

## Context

Operational records grow continuously, while evidence, claims, digests, and
audit identities must remain inspectable. Backups that have never been
restored are not reliable evidence. Security scanners and generated SBOMs also
need exact, reproducible tool inputs rather than floating installation steps.

## Decision

- A daily typed River job runs one bounded, date-idempotent retention cycle.
- Raw bodies become eligible after 180 days and normalized bodies after 365
  days. Objects supporting a published item or claim are retained. Metadata,
  hashes, citations, claims, digests, and compact audit identities remain.
- Fetch and parse attempts retain 90 days, outbox replay retains seven days,
  processed provider webhook detail retains 30 days, and item-mutation
  before/after snapshots compact after 30 days.
- Every retention run stores its exact policy, state, bounded counts, and error
  code. Object keys must pass the content-addressed namespace validator before
  deletion.
- `make backup` emits a private custom-format logical dump and SHA-256 file.
  `make restore-drill` restores only into a generated disposable database,
  verifies migration and data counts, records RPO/RTO, and drops that database.
- Worker `/metrics` and `/alerts` are private low-cardinality operational
  surfaces. Railway logs/metrics remain the initial hosted telemetry backend.
- OSV-Scanner, Trivy, zizmor, Syft, gitleaks, and govulncheck are exact-version
  inputs. Repository and image SBOMs use SPDX JSON.

## Consequences

Operational storage is bounded without silently removing published evidence.
Deletion and database marking cannot be one distributed transaction, so
object removal is idempotent and a failed mark is safely replayed. Production
backup encryption and destination configuration remain environment-specific
PR19 gates; local dumps are mode 0600 and ignored.

Hosted rollback remains application-first and forward-only. Migration 20 is
retained. Its local down path refuses to run after any object or mutation
snapshot has been pruned because reversal could not recreate deleted content.
