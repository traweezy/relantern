# Bulk state mutation recovery runbook

Use this runbook when a confirmed bulk reading command fails, conflicts, or has
an uncertain client outcome. Never edit owner state or mutation rows directly.

## Inspect

- Record the request ID, owner ID, bulk ID, command, expected count, and client
  timestamp. Do not record note or highlight bodies.
- Query mutation metadata by owner and `bulk_id`; verify the row count equals
  the confirmed count and whether every row has the same Undo deadline/status.
- Inspect API logs for a bounded conflict, expiry, or transaction error and
  PostgreSQL metrics for serialization failures or lock waits.
- Refresh the affected collection. Idempotent retries with the original key
  must return the original result; a different command needs a new key.

## Recover

- If no mutation rows exist, the transaction did not commit. Refresh and issue
  a new owner-authorized command from current versions.
- If all rows exist and the ten-second window remains open, use the normal bulk
  Undo endpoint with the recorded bulk ID.
- If any member has a newer version, do not force Undo. Refresh, present the
  current states, and obtain a new explicit owner decision.
- If the window expired, create a new inverse command from current versions;
  do not alter `undone_at` or restore stored JSON manually.

## Verify

- Confirm exact affected count and collection membership.
- Confirm all members advanced monotonically to the same semantic outcome.
- Confirm linked feedback was removed only for a successful Undo.
- Confirm no partial mutation or partial Undo exists.
