# OpenAI outage runbook

## Trigger

- Structured extraction or research error rate exceeds 10 percent for 15
  minutes, provider availability is degraded, or requests time out repeatedly.

## Impact

AI enrichment and research pause. Deterministic ingestion, security matching,
ranking, search metadata, and digest fallback must continue within budget.

## Immediate containment

1. Disable new AI work before increasing retries or timeouts. Keep the hard
   cost cap authoritative.
2. Record run states, bounded error codes, attempts, reserved/actual cost,
   provider status, and last successful run. Never log prompts with private
   content or credentials.
3. Keep critical deterministic advisory matching active.

## Exact verification commands

```sh
make test-extraction
make test-research
make digest-preview
docker compose logs --since=30m worker fake-openai
```

## Recovery

1. Wait for provider health or correct credentials/configuration; do not switch
   to an unreviewed model or endpoint.
2. Reconcile background responses and retry only durable retryable runs within
   budget.
3. Verify deterministic digest delivery without an executive overview before
   restoring AI traffic gradually.

## Data-integrity checks

- Provider response IDs map to one run and duplicate webhooks transition once.
- Failed/partial outputs never publish as validated claims.
- Budget reservations reconcile and deterministic processing continued.

## Communication

Record provider window, affected run counts, cost exposure, fallback behavior,
and recovery. Distinguish provider status from application inference.

## Post-incident evidence

Retain provider notice, redacted error-rate metrics, run transitions, fallback
digest evidence, reconciliation result, and regression coverage.
