# Queue backlog runbook

## Trigger

- Oldest available/retryable job exceeds 15 minutes, queue depth grows for
  three intervals, or Priority 0/digest work waits behind lower priority work.

## Impact

Freshness, critical alert, research, and digest objectives may be missed.
Retries must not create duplicate externally visible effects.

## Immediate containment

1. Disable new non-critical intake if depth continues growing; preserve the
   critical queue and delivery kill switch.
2. Record per-queue state counts, oldest age, failed kinds, worker SHA, database
   health, provider status, and resource saturation.
3. Do not bulk-delete or manually mark River rows complete.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker
curl --fail --silent --show-error http://127.0.0.1:8081/metrics
docker compose run --rm worker dead-letters -limit 50
```

## Recovery

1. Correct database, storage, provider, configuration, or worker capacity.
2. Restart under the supervisor; River rescues stuck work after the bounded
   interval.
3. Retry discarded jobs only through the audited command after verifying each
   downstream idempotency identity.
4. Resume source groups gradually and confirm age decreases.

## Data-integrity checks

- One logical input has one active job per unique identity.
- Occurrence/digest/outbox/provider ledgers contain no duplicate effect.
- Critical work was not cancelled to improve aggregate depth.

## Communication

Record affected queues, oldest age, delayed objectives, containment, recovery
rate, and any discarded work. Redact job arguments containing private IDs.

## Post-incident evidence

Retain queue metrics, bounded dead-letter list, resource/provider timelines,
retry audit, and capacity or backpressure change.
