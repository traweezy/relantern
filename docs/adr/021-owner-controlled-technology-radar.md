# ADR-021: Owner-controlled evidence-first Technology Radar

Status: Accepted
Date: 2026-08-29

## Context

PR 16 adds recurring package discovery and assessment. Popularity can surface
useful candidates, but downloads and stars do not establish license fit,
security, maintenance quality, compatibility, or a reversible migration. A
recommendation engine must also remain unable to install, execute, or adopt
downloaded code.

The safe local profile cannot reach live GitHub write, OpenAI, delivery, or
other external providers. Discovery therefore needs a durable boundary between
collection adapters and assessment.

## Decision

- External and ingestion adapters may write attributable observations only to
  the owner-scoped package-evidence inbox after their own review and validation.
- `RunWeeklyRadarDiscovery` processes at most 200 pending observations per
  transaction from that inbox. It does not browse, install packages, execute
  candidate code, mutate dependency manifests, or call a model.
- Every observation produces an immutable package metric and an exactly
  seven-dimension comparison: Capability, Stability, Maintenance, Security,
  Performance, Migration, and Reversibility.
- Hard license or critical-security failures suggest Reject. Incomplete
  stability, bus-factor, response, compatibility, provenance, or attributable
  evidence suggests Hold. Sufficient evidence suggests Assess.
- Popularity affects the `misleading` warning only. It cannot improve the
  suggested state or override license, security, or stability findings.
- System processing cannot suggest Trial or Adopt. A database constraint also
  rejects any system-originated Adopt decision.
- The authenticated owner records the final state with a rationale, evidence,
  review date, applicable project types, compatibility requirements, and exit
  conditions. Candidate versions provide optimistic concurrency.
- Decision and metric history is append-only. The candidate row is only the
  current projection.

## Reliability and performance objectives

- Owner Radar reads target p95 below 300 milliseconds for 100 candidates and
  the latest 50 decisions per candidate.
- A 200-observation evidence pass completes within two minutes in the local
  production-like stack and is safe to retry by run and evidence identity.
- Owner run requests are idempotent and create exactly one River job.
- Structured worker logs report run ID and evidence, candidate, and misleading
  counts without repository payloads or owner profile data.

## Security and rollback

Radar endpoints require both an owner session at the Next.js boundary and the
existing private service credential at the Go API. Repository and evidence URLs
must use HTTPS. No secret or package archive is stored in the ledger.

Rollback removes the new UI and handlers first and leaves the additive schema
intact. Evidence and owner decisions remain available for forward recovery.

## Consequences

The first release does not claim whole-registry or live-provider coverage. An
empty evidence inbox produces a valid, visible empty state. New adapters can be
added later without giving the assessment worker network or execution
authority.
