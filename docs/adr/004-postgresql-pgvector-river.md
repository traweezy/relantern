# ADR-004: PostgreSQL, pgvector, and River

Status: Accepted
Date: 2026-08-29

## Context

Version 1 needs durable relational state, full-text and semantic retrieval,
transactional jobs, and replay without unnecessary infrastructure.

## Decision

Use PostgreSQL 18.6, pgvector 0.8.6, pgx/v5, sqlc, and River OSS. Application
tables own schedule definitions and occurrences; River wakes reconciliation and
executes typed jobs. The one-shot migration service applies River's bundled,
versioned migrations in a dedicated `river` schema after application Goose
migrations establish that schema.

## Consequences

Business state, outbox events, and jobs can commit atomically. Redis, Kafka,
Temporal, Elasticsearch, and a separate vector database require measured
triggers before adoption. The concrete queue, retry, and schedule execution
contract is recorded in ADR-015.
