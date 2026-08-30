# ADR-015: River queue and durable schedule execution

Status: Accepted
Date: 2026-08-29

## Context

The worker needs PostgreSQL-native jobs without making River's memory-backed
periodic schedule the source of truth for owner-local digest time. A worker can
restart at the exact due minute, two replicas can reconcile together, and a
provider can fail after work has started. Every case must preserve one durable
occurrence identity and make failed work inspectable without enabling a live
provider.

## Decision

- Pin River OSS 0.44.1, the newest stable release that passed the repository's
  seven-day age gate. Apply its bundled migrations through the one-shot migrate
  command in the dedicated `river` schema; application code never migrates at
  worker startup.
- Configure bounded `critical`, `fetch`, `parse`, `ai_fast`, `ai_research`,
  `delivery`, and `maintenance` queues. Only implemented job kinds are
  registered; declaring a queue does not authorize source polling, OpenAI, or
  external delivery.
- Use a one-minute River periodic job with `RunOnStart`. Its minute-level unique
  job only wakes `ReconcileSchedules`; application schedule definitions and the
  occurrence ledger remain the timing authority.
- Propagate the reconciliation run ID into typed occurrence jobs for traceable
  one-shot completion. Occurrence uniqueness remains keyed only by occurrence
  ID so the correlation metadata cannot weaken duplicate suppression.
- Reconcile in a serializable transaction guarded by a dedicated PostgreSQL
  advisory lock. Lock due rows, create or recover each occurrence, insert the
  typed occurrence job with `InsertTx`, link its typed River job ID, and advance
  `next_due_at` before commit.
- Advance at most 100 overdue occurrences for one definition in a tick. Persist
  skipped and missed occurrences, enqueue catch-up work only inside its grace
  period, and clear `skip_next_at` only when its exact occurrence is consumed.
- Preserve UTC `scheduled_for`, owner `local_date`, and local UTC offset.
  Ambiguous fall-back time resolves to the first instant; a spring-forward gap
  resolves to its first valid minute. API and worker binaries embed `time/tzdata`.
- Bound reconciliation at three attempts and occurrence work at five attempts.
  Use injected-clock exponential delays from five seconds through five minutes.
  Invalid IDs and non-retryable provider 4xx responses are permanent;
  exhausted jobs remain discarded indefinitely for inspection.
- Keep manual retry private. The worker CLI accepts only cancelled or discarded
  jobs and commits the retry plus `job.manual_retry` audit event atomically.
  No unauthenticated HTTP retry endpoint is introduced before the auth and
  operations phases.

## Reliability and observability

The target is one successful scheduler tick per minute. Readiness fails after
three missed intervals and reports both the last successful tick and oldest
overdue occurrence. Structured logs record job ID, kind, queue, attempt, lock
outcome, occurrences created, and occurrence jobs enqueued without logging job
arguments or provider payloads.

Database tests race two reconcilers, repeat reconciliation after a simulated
restart, force enqueue rollback, inspect dead letters, retry with an audit, and
process the same occurrence twice. Ten-year DST fixtures cover both New York
transitions. River's unique job constraint complements, but does not replace,
the occurrence unique key and downstream delivery idempotency key.

## Consequences

Business state and jobs become visible atomically, crashed River jobs are
rescued by PostgreSQL-backed maintenance, and a worker restart cannot create a
second schedule occurrence. The queue dependency and its MPL-2.0 license remain
exactly pinned, scanned, and recorded in the supply-chain manifest.

The safe local profile still targets only the in-process fake delivery service.
Source polling, OpenAI, Discord, Resend, and Railway delivery remain disabled
until their separate rollout gates pass.

## Owner control-plane amendment

PR15 adds optimistic, audited schedule edits plus Preview, Skip next, timed
pause, resume, and Run now controls. A timed pause is durable and the reconciler
clears it under the existing advisory lock after expiry. Run now creates an
idempotent preview-only occurrence and cannot reach an external delivery
provider before the PR17 delivery gate. Schedule state, audit evidence, and
outbox publication commit together.

## Primary references

- [River getting started and migrations](https://riverqueue.com/docs)
- [River periodic jobs](https://riverqueue.com/docs/periodic-jobs)
- [River unique jobs](https://riverqueue.com/docs/unique-jobs)
- [River transactional enqueueing](https://riverqueue.com/docs/transactional-enqueueing)
