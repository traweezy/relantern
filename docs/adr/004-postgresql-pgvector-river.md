# ADR-004: PostgreSQL, pgvector, and River

Status: Accepted
Date: 2026-08-29

## Context

Version 1 needs durable relational state, full-text and semantic retrieval,
transactional jobs, and replay without unnecessary infrastructure.

## Decision

Use PostgreSQL 18.6, pgvector 0.8.6, pgx/v5, sqlc, and River OSS. Application
tables own schedule definitions and occurrences; River wakes reconciliation and
executes typed jobs.

## Consequences

Business state, outbox events, and jobs can commit atomically. Redis, Kafka,
Temporal, Elasticsearch, and a separate vector database require measured
triggers before adoption.
