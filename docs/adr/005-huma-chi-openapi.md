# ADR-005: Huma/chi OpenAPI 3.1 contract

Status: Accepted
Date: 2026-08-29

## Context

The browser and later Android client require a stable typed HTTP contract and
consistent problem responses.

## Decision

Use chi for routing/middleware and Huma v2 for typed operations and canonical
OpenAPI 3.1 generation. Generate one platform-neutral TypeScript client in
`packages/api-client` and fail CI on drift.

## Consequences

Handlers, schemas, and clients remain reviewably aligned. Production API docs
are disabled even though the committed contract remains available to builds.
