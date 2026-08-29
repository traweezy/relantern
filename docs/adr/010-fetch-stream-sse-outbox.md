# ADR-010: Fetch-stream SSE and durable outbox

Status: Accepted
Date: 2026-08-29

## Context

Live updates are server-to-client, must expose HTTP failure status, support
abort and retry control, and replay after disconnection.

## Decision

Use fetch, `ReadableStream`, `AbortController`, and `eventsource-parser` behind
a `LiveTransport` interface. Persist monotonic events in PostgreSQL; use
LISTEN/NOTIFY only as a wake-up signal and retain replay rows for seven days.

## Consequences

Reconnects resume from durable cursors. Socket.IO and WebSockets are deferred
until sustained bidirectional requirements or measured SSE limits appear.
