# ADR-002: Explicit source registry

Status: Accepted
Date: 2026-08-29

## Context

Claiming whole-web monitoring is unverifiable, costly, and unsafe. Coverage
must be measurable and auditable.

## Decision

Use a code-reviewed built-in YAML registry mirrored into PostgreSQL. Owner-added
sources remain pending until connector, SSRF, policy, parse, fixture, and owner
approval checks pass.

## Consequences

The product reports source health and coverage gaps honestly. New parser code
requires fixtures and normal review rather than runtime configuration.
