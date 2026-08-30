# Digest duplicate prevention runbook

## Trigger

- Two visible provider messages appear to represent the same owner, local
  date, and channel.
- A provider timeout leaves acceptance uncertain.
- A delivery retry is requested while another attempt or River job is active.

## Impact

The owner may receive duplicate notifications and lose confidence in the
delivery ledger. Immutable dashboard history and source evidence remain
authoritative.

## Immediate containment

1. Disable delivery before retrying or restarting workers.
2. Capture the digest ID, occurrence ID, channel, payload SHA-256,
   idempotency key, attempt count, provider ID if present, and River job state.
3. Do not delete provider messages or database rows until the comparison is
   recorded.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api fake-delivery
curl --fail --silent --show-error http://127.0.0.1:8092/captures
make test-integration
```

For local evidence, inspect the loopback-only rendered capture viewer at
`http://127.0.0.1:8092/` and compare capture idempotency keys and hashes.

## Recovery

1. Identify whether duplication occurred in occurrence creation, immutable
   rendering, River enqueue, transport, or the provider.
2. Confirm there is only one digest row and one delivery-attempt row for the
   channel. If constraints are intact, do not create replacement rows.
3. Retry only a `failed` delivery through the owner Digest history control.
   This reuses the original payload, hash, and idempotency key.
4. If provider acceptance is ambiguous, verify the destination before an
   owner retry. Do not assume a client timeout means the provider rejected the
   message.
5. Re-enable delivery only after the idempotency integration test and a local
   duplicate-capture rehearsal pass.

## Data-integrity checks

- `digests_user_date_channel_key` and `digests_occurrence_channel_key` exist.
- One digest/channel has one `delivery_attempts` row.
- Every attempt retains the digest payload SHA-256 and idempotency key.
- Fake delivery captures one record across the forced identical-payload retry.
- The owner-visible attempt count equals the ledger attempt count.

## Communication

Record whether a duplicate was observed or only suspected, the affected local
date/channel, containment time, and confirmation method. Redact provider
destinations, credentials, and content.

## Post-incident evidence

Retain constraint verification, bounded IDs and hashes, provider response IDs,
queue transitions, screenshots with private content redacted, and the fix.
