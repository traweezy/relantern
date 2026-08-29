# ADR-008: Evidence and claim provenance

Status: Accepted
Date: 2026-08-29

## Context

Release, security, migration, and deprecation claims must be inspectable and
must survive source revisions.

## Decision

Persist immutable raw hashes, normalized revisions, atomic claims, source-span
references, prompt/schema/model versions, response IDs, and verification state.
Material claims require T0/T1 evidence.

Each `ai_runs` row is immutable in identity: item, source revision, purpose,
model config, and prompt version. It records the normalized input digest,
validated structured output, provider response ID, aggregate token counts,
fixed-point cost, timestamps, retry count, and terminal error code. Each
provider call has its own `ai_run_attempts` row so retries and conservative
reservations cannot disappear into aggregates.

Claims are stored atomically and in provider order. `normalized_value` remains
JSON so later claim types can use versioned scalar shapes, but PR 9 permits only
a bounded string. Every claim belongs to the exact item, revision, and AI run.
Its verification state is:

- `verified_span` for span-valid claims from T0/T1 evidence;
- `review_required` for material claims from T2/T3 evidence.

An evidence row stores the source revision, deterministic span identifier,
section path, byte offsets, and SHA-256 of the quoted text. The quote itself is
reconstructed from the immutable normalized object using those offsets and
verified against the hash; it is not copied into mutable application state.
UTF-8 boundary checks happen before the model call and again through the stored
revision digest.

The active prompt row contains semantic version plus prompt and schema SHA-256.
Migration 8 seeds prompt/schema version 1.0.0. Any prompt or schema edit requires
a new versioned file and registry row. The worker refuses database/file digest
drift, avoiding provenance that only appears reproducible.

Completion and claim insertion share one serializable transaction. If the item
points to a different revision by completion time, the run becomes `obsolete`
and no claims are inserted. Replaying a terminal identity returns its existing
result without a new provider call.

## Consequences

Unsupported claims cannot publish. Changed source material creates a new
revision and never silently rewrites the evidence used by an existing brief.
Operators can audit every cost and evidence transition without retaining a raw
provider response indefinitely. Deleting revisions, claims, or AI provenance
requires a separately reviewed retention/privacy workflow because foreign keys
default to `restrict`.
