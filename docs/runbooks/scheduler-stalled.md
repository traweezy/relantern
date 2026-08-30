# Scheduler stalled runbook

## Trigger

- Scheduler last success is older than three one-minute intervals.
- `/ops` reports an overdue occurrence or growing scheduled/delivery backlog.
- A due digest has no occurrence ledger row after its catch-up grace begins.

## Impact

Daily or weekly dashboard digests may be late. Durable schedule definitions,
occurrence uniqueness, and queued jobs remain in PostgreSQL, so a safe restart
can reconcile missed work without creating a second occurrence.

## Immediate containment

1. Keep external delivery disabled until occurrence and delivery idempotency is
   confirmed.
2. Record environment, release SHA, schedule ID, next due time, pause state,
   oldest overdue occurrence, queue depth, and scheduler last-success time.
3. Do not edit `app.schedule_occurrences` or River tables directly.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api
make test-scheduler
make test-integration
```

For a reproducible local DST or missed-window investigation, use an isolated
clock and remove it only with the separately confirmed cleanup target:

```sh
make time-travel at=2026-11-01T01:30:00-04:00
```

## Recovery

1. Restore PostgreSQL connectivity or correct the worker configuration.
2. Restart the worker through the normal service supervisor. Reconciliation
   runs on startup and uses a PostgreSQL advisory lock.
3. Confirm an elapsed timed pause clears automatically and that an active pause
   remains excluded.
4. Use Run now only as a preview until external delivery is separately enabled.
5. If a job is discarded, follow the queue recovery runbook and prove its
   downstream idempotency key before retrying.

## Data-integrity checks

- One schedule/local occurrence has at most one occurrence ledger row.
- One occurrence links to at most one River job and one delivery idempotency
  key.
- `next_due_at` advances inside the same transaction as occurrence enqueue.
- Skip-next clears only when its exact occurrence is consumed.
- Spring-forward and fall-back projections retain the expected local date and
  UTC offset.

## Communication

Record the missed window, owner impact, whether catch-up applied, delivery
state, and recovery time. Avoid posting schedule tokens, owner IDs, or provider
destinations.

## Post-incident evidence

Retain scheduler logs, occurrence IDs, queue transitions, idempotency evidence,
the fixed-clock reproduction, and the regression test or configuration change.
