# ADR-009: PostgreSQL hybrid search

Status: Accepted
Date: 2026-08-29

## Context

Personal-scale retrieval needs exact package/version lookup, typo tolerance,
filters, and semantic concept search.

## Decision

Combine PostgreSQL full-text search, pg_trgm, pgvector cosine retrieval, and
reciprocal-rank fusion. Version embedding indexes and migrate them in parallel.

## Consequences

No separate search service is required initially. A replacement requires a
measured relevance, latency, corpus, or database-contention trigger.
