# ADR-001: Single-owner private product

Status: Accepted
Date: 2026-08-29

## Context

Relantern handles private reading state, interests, notes, delivery addresses,
costs, and operational evidence. Version 1 has exactly one owner.

## Decision

Authorize only GitHub numeric user ID `5276132`. Keep the implementation
repository private. Public presentation is restricted to the independently
audited static `/demo` fixture boundary.

## Consequences

The data model retains a user boundary for correctness and future migrations,
but Version 1 avoids public signup, billing, teams, and generalized tenancy.
