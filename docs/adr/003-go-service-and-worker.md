# ADR-003: Go service and worker

Status: Accepted
Date: 2026-08-29

## Context

Fetching, parsing, scheduling, ranking, and delivery are concurrent,
network-heavy workloads with strict resource and cancellation requirements.

## Decision

Use one Go 1.27 module for the private API, worker, migrations, and local fake
providers. Pass clocks, stores, transports, and provider clients explicitly.

## Consequences

The domain and scheduling core remain small and testable. A separate Python or
TypeScript AI worker is not introduced merely to gain another SDK.
