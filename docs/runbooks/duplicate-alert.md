# Duplicate alert runbook

## Trigger

- The owner receives two urgent alerts for one advisory or two provider
  messages share the same logical event, channel, or payload hash.

## Impact

Urgency and owner trust are degraded. Repeated messages may hide a more serious
idempotency or provider-acceptance ambiguity.

## Immediate containment

1. Disable external delivery and preserve both messages.
2. Record advisory/source identity, occurrence/digest IDs, channel, payload
   hash, idempotency key, attempt count, provider IDs, and timing.
3. Do not delete ledger rows or regenerate the content.

## Exact verification commands

```sh
make test-scheduler
make test-integration
curl --fail --silent --show-error http://127.0.0.1:8092/captures
docker compose logs --since=30m worker fake-delivery
```

## Recovery

1. Locate duplication in deterministic matching, occurrence creation, queue
   insertion, transport retry, or provider behavior.
2. Correct the unique/idempotency boundary and replay the same fixture until
   one visible capture remains.
3. Re-enable one staging-only channel before production consideration.

## Data-integrity checks

- One advisory identity maps to one urgent alert identity.
- One owner/date/channel maps to one digest and one immutable payload hash.
- Ambiguous provider acceptance is manually verified before retry.

## Communication

Record observed versus suspected duplication, affected channels, containment,
owner impact, and resolution without copying private content.

## Post-incident evidence

Retain bounded ledger rows, capture hashes, provider acknowledgement, failing
fixture, and duplicate-prevention regression test.
