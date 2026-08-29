# ADR-006: Next.js BFF authentication boundary

Status: Accepted
Date: 2026-08-29

## Context

The browser must never receive private-service credentials or directly reach
the Go API over Railway private networking.

## Decision

Use Next.js as the public BFF. Better Auth verifies GitHub OAuth and the owner
allowlist; server-side data boundaries recheck sessions. The BFF authenticates
to the private Go API with a rotated service credential.

## Consequences

Only `web` is publicly routed. Proxy or middleware redirects improve UX but are
never the sole authorization control.
