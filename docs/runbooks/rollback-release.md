# Roll back a release runbook

## Trigger

- A new release breaks security, data integrity, availability, or a critical
  owner journey and cannot be corrected safely in place.

## Impact

Application images return to a prior compatible SHA. Forward migrations and
new durable evidence remain; rollback is not a database restore.

## Immediate containment

1. Disable worker intake and external delivery for queue, payload, or data-risk
   failures. Preserve the failing deployment and database state.
2. Record current/prior SHAs, migration version, affected services, trigger,
   and first/last known good evidence.
3. Do not run Goose down in staging/production.

## Exact verification commands

```sh
git status -sb
git log --oneline --decorate -10
make config-check
make prepush
make prodlike-smoke
```

## Recovery

1. Confirm the prior images accept the current forward-compatible schema.
2. Roll back API and worker before web when private contracts changed; retain
   migration and audit rows.
3. Run health/auth/demo smoke checks, then reconcile durable queues.
4. Re-enable intake in stages and delivery last. Forward-fix schema defects
   with a new additive migration.

## Data-integrity checks

- Current migration remains applied and no down migration ran.
- Outbox, occurrence, digest, attempt, retention, and audit identities remain.
- Replayed jobs produce no duplicate visible effects.

## Communication

Record reason, SHAs, services, containment, compatibility decision, owner
impact, and recovery time. State explicitly whether data was restored (normally
no).

## Post-incident evidence

Retain deployment timelines, smoke results, queue reconciliation, metrics,
screenshots with private content redacted, and the forward fix.
