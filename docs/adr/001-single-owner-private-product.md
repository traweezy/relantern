# ADR-001: Single-owner private product

Status: Accepted for application access; source visibility superseded by ADR-026
Date: 2026-08-29

## Context

Relantern handles private reading state, interests, notes, delivery addresses,
costs, and operational evidence. Version 1 has exactly one owner.

## Decision

Authorize only GitHub numeric user ID `5276132` for the authenticated application.
The original private-source decision was superseded on 2026-09-06 by
[ADR-026](026-public-source-private-runtime.md). Source is public for inspection;
anonymous application access remains restricted to the independently audited
static `/demo` fixture boundary.

## Consequences

The data model retains a user boundary for correctness and future migrations,
but Version 1 avoids public signup, billing, teams, and generalized tenancy.
