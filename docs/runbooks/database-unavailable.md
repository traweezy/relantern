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

## API and worker schema gate

Each API and worker binary embeds the committed SQL migrations. On startup it
waits at most eight minutes for every version in that build to be marked
applied in Goose, checks the catalog footprint of migration 22, and requires
the post-success migration completion row for its exact full release SHA and
Goose version. The one-shot migrator records this row only after Goose, River,
and source-registry synchronization succeed. A failed run for a new SHA leaves
that release unready. A failed repeat of an already successful SHA retains its
valid marker; use the failed deployment status and logs to diagnose that run.
Local and test runs use `GIT_SHA=unknown`; hosted runs require a full lowercase
SHA. `migrate up` holds a database advisory lock across all three steps and
marker recording. A second migrate job waits for that lock for at most eight
minutes; inspect its logs if the wait expires. The migrator needs a direct
PostgreSQL session or session pooling. Transaction-pooled PgBouncer cannot
hold this session advisory lock across the job.
The worker performs this check before River starts. API and worker `/readyz`
repeat the check with a short request deadline, so readiness fails after a
database restore or later schema change. The API ready response includes its
`gitSha` for the private web-to-API release check. `/healthz` remains process
health.

If logs say `database migrations pending`, inspect the one-shot migrate job,
its release SHA, and its completion log. A Goose version alone does not mean
River and the source registry have finished. Do not start application traffic
until the marker is recorded. If logs say `database schema differs from an
applied migration`, preserve the database and migration evidence. The gate
checks for the five source-entry columns and six valid indexes introduced by
migration 22, including the
source-fetch object-key index. A recorded Goose version alone cannot prove
that a pre-commit SQL draft applied the committed schema. Repair drift with a
reviewed forward migration or restore from a verified backup; do not reset a
database or edit Goose history to make readiness pass.

This catalog check targets a known historical drift. It does not prove every
constraint or data invariant from all migrations. Continue the integrity
checks above and the isolated restore drill for release evidence.

## Communication

Record outage start/end, owner-visible impact, recovery action, RPO/RTO, and
whether any write acknowledgement is uncertain. Never paste a database URL.

## Post-incident evidence

Retain bounded logs, health timestamps, backup checksum, restore report when
used, row-integrity query results, release SHA, and the regression prevention.
