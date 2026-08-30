# Restore PostgreSQL runbook

## Trigger

- Monthly automated restore rehearsal or quarterly disaster-recovery exercise.
- Proven database corruption or unrecoverable volume loss.
- Backup/restore integrity alert or a release gate requiring fresh evidence.

## Impact

A rehearsal creates and drops only an isolated database. A production restore
can replace authoritative state and is destructive; it requires owner approval
after corruption is proven.

## Immediate containment

1. Stop worker intake and external delivery for a real incident.
2. Record the candidate backup timestamp/checksum, incident time, current
   release SHA, stated RPO/RTO, and database region/version.
3. Never restore over the current database or delete it as a rehearsal.

## Exact verification commands

```sh
make backup
make restore-drill
find dist/restore-drills -type f -name '*.json' -print
docker compose exec -T postgres psql -U relantern -d relantern -c 'select max(version_id) from goose_db_version where is_applied;'
```

## Recovery

1. Verify the checksum and custom-format archive list before restore.
2. Restore to a new isolated database and validate migration version, critical
   table counts, constraints, readiness queries, and application smoke tests.
3. For production corruption only, create a new database/volume, restore there,
   validate it, then perform a controlled connection cutover. Keep the corrupt
   source read-only for investigation.
4. Resume API reads, then worker intake, then delivery. Reconcile durable jobs.

## Data-integrity checks

- Report result is `pass`, RPO is at most 86,400 seconds, and RTO is at most
  14,400 seconds.
- Migration version matches the application release.
- Owner, sources, schedules, audit, digests, and durable jobs are consistent.
- No production credential or private content is present in the report.

## Communication

Record backup age, checksum prefix, restore start/end, objectives, validation,
cutover decision, and data-loss estimate. Do not attach the dump itself.

## Post-incident evidence

Retain the encrypted backup in its controlled destination, checksum, redacted
restore report, health checks, release SHA, integrity queries, and approvals.
