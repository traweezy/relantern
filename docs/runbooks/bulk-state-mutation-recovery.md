# Bulk state mutation recovery runbook

Use this runbook when a confirmed bulk reading command fails, conflicts, or has
an uncertain client outcome. Never edit owner state or mutation rows directly.

## Trigger

- A confirmed bulk command returns an error, conflict, or uncertain client
  outcome.
- Bulk Undo is partial, rejected unexpectedly, or returns an incorrect count.

## Impact

The affected owner collection may appear stale or uncertain. A transaction
must remain all-or-nothing; unrelated reading state is unaffected.

## Immediate containment

- Stop replaying the command and preserve its request ID, bulk ID, expected
  count, owner ID, and idempotency key.
- Do not edit mutation rows, owner state, versions, or Undo timestamps.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m api postgres
make test-integration
```

- Record the request ID, owner ID, bulk ID, command, expected count, and client
  timestamp. Do not record note or highlight bodies.
- Query mutation metadata by owner and `bulk_id`; verify the row count equals
  the confirmed count and whether every row has the same Undo deadline/status.
- Inspect API logs for a bounded conflict, expiry, or transaction error and
  PostgreSQL metrics for serialization failures or lock waits.
- Refresh the affected collection. Idempotent retries with the original key
  must return the original result; a different command needs a new key.

## Recovery

- If no mutation rows exist, the transaction did not commit. Refresh and issue
  a new owner-authorized command from current versions.
- If all rows exist and the ten-second window remains open, use the normal bulk
  Undo endpoint with the recorded bulk ID.
- If any member has a newer version, do not force Undo. Refresh, present the
  current states, and obtain a new explicit owner decision.
- If the window expired, create a new inverse command from current versions;
  do not alter `undone_at` or restore stored JSON manually.

## Data-integrity checks

- Confirm exact affected count and collection membership.
- Confirm all members advanced monotonically to the same semantic outcome.
- Confirm linked feedback was removed only for a successful Undo.
- Confirm no partial mutation or partial Undo exists.

## Communication

Record the affected count, visible owner impact, containment decision, recovery
action, and whether Undo remained available. Never include annotation bodies.

## Post-incident evidence

Retain request and bulk IDs, sanitized API/database logs, mutation counts,
version checks, recovery timestamps, and the regression test or change.
