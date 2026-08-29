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

## Consequences

Unsupported claims cannot publish. Changed source material creates a new
revision and never silently rewrites the evidence used by an existing brief.
