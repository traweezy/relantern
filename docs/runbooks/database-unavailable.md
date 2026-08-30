# Database unavailable runbook

## Trigger

- API or worker readiness reports PostgreSQL unavailable.
- Connection errors persist across two health intervals.
- Railway reports a database incident, storage exhaustion, or failed restart.

## Impact

Private reads, mutations, schedules, ingestion, delivery, audit, and durable job
progress stop. The anonymous fixture demo should remain available.

## Immediate containment

1. Disable worker intake and external delivery; do not run `make reset`, a down
   migration, manual table repair, or automatic restore.
2. Record environment, release SHA, database service state, first failure,
   latest successful scheduler tick, and whether writes may have been accepted.
3. Preserve logs and the newest backup metadata without exposing connection
   strings or credentials.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m postgres api worker
curl --fail --silent --show-error http://127.0.0.1:3000/demo >/dev/null
docker compose exec -T postgres pg_isready -U relantern -d relantern
```

## Recovery

1. Recover capacity, networking, credentials, or the database process before
   restarting application services.
2. If integrity is uncertain, follow `restore-postgres.md` in an isolated
   database first. Restore production only when corruption is proven.
3. Start API, then worker, then web; confirm readiness and one scheduler tick.
4. Re-enable intake gradually and external delivery last.

## Data-integrity checks

- Goose version equals the release migration version.
- Schedule occurrence, River job, outbox, audit, digest, and delivery ledgers
  have no broken foreign keys or duplicate idempotency identities.
- Object metadata points only to existing retained objects.

## Communication

Record outage start/end, owner-visible impact, recovery action, RPO/RTO, and
whether any write acknowledgement is uncertain. Never paste a database URL.

## Post-incident evidence

Retain bounded logs, health timestamps, backup checksum, restore report when
used, row-integrity query results, release SHA, and the regression prevention.
