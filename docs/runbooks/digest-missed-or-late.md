# Digest missed or late runbook

## Trigger

- No dashboard digest is visible by 08:05 owner-local time.
- `/ops` shows a due occurrence in `enqueued`, `preparing`, `ready`, or
  `delivering` for more than fifteen minutes.
- A missed occurrence did not catch up inside the six-hour grace window.

## Impact

The owner briefing is delayed or absent. Continuous ingestion and immutable
prior digests remain available. Recovery must not create a second payload for
the same owner, local date, and channel.

## Immediate containment

1. Disable live delivery if occurrence uniqueness or provider acceptance is
   uncertain; follow `disable-delivery.md`.
2. Record release SHA, schedule and occurrence IDs, local date, state, stage,
   queue depth, oldest job age, and last successful scheduler tick.
3. Do not update the occurrence, digest, attempt, or River tables manually.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api
make test-scheduler
make test-integration
```

Render the current evidence window without persistence or delivery:

```sh
make digest-preview
```

List bounded discarded work:

```sh
docker compose run --rm worker dead-letters -limit 50
```

## Recovery

1. Restore PostgreSQL, object storage, or worker health.
2. Restart the worker through the normal supervisor. Startup reconciliation
   creates one catch-up occurrence only while the grace window remains open.
3. If a typed stage was discarded, fix the cause and use the audited
   `retry-job` command from the queue recovery runbook.
4. After the grace window, leave the durable occurrence `missed` and use the
   owner Run Now preview. Set `deliver=true` only after checking same-date
   channel history and delivery fuses.
5. Confirm the occurrence and dashboard digest reach `delivered` and Today
   displays the immutable coverage window.

## Data-integrity checks

- One schedule and scheduled instant map to one occurrence.
- One owner/local-date/channel maps to one digest.
- Candidate capture precedes finalization and the rendered hash never changes.
- Catch-up occurs once within six hours and remains missed afterward.
- Source ingestion timestamps continue before and after the digest window.

## Communication

Record the expected and actual local delivery times, affected channels,
whether catch-up or Run Now was used, and owner impact. Never include recipient
addresses, webhook URLs, provider authorization, or payload bodies.

## Post-incident evidence

Retain bounded occurrence/job/digest IDs, stage timestamps, error codes,
recovery time, duplicate checks, and the regression test or configuration
change.
