# Disable all external fetching runbook

## Trigger

- SSRF suspicion, DNS/network policy failure, compromised source credential,
  unsafe content-policy behavior, or uncontrolled external cost.

## Impact

Live source polling, OpenAI, and other external fetches stop. Existing private
evidence, deterministic reads, local fixtures, and dashboard history remain.

## Immediate containment

1. Set `ALLOW_LIVE_EXTERNAL_APIS=false`; disable hosted OpenAI and every source
   polling override. Disable delivery separately when message safety is also in
   doubt.
2. Restart the worker and verify the old instance exits within its graceful
   timeout.
3. Revoke any suspected source/provider credential after worker shutdown.

## Exact verification commands

```sh
make doctor
make config-check
make sources-verify
docker compose config | rg 'ALLOW_LIVE_EXTERNAL_APIS|OPENAI_.*_ENABLED'
docker compose logs --since=15m worker
```

## Recovery

1. Correct URL policy, resolver/redirect validation, source registry, provider
   credentials, or budget controls.
2. Run zero-network fetcher fuzz, source fixtures, extraction, and research
   suites.
3. Enable one reviewed staging source group at a time; OpenAI and other live
   APIs remain separate gates.

## Data-integrity checks

- No worker request reaches unlisted external hosts while disabled.
- Local/test uses only loopback or Compose fake providers.
- Checkpoints and retry state remain durable for later catch-up.

## Communication

Record environment, disabled adapters, reason, release SHA, revocations, owner
impact, and re-enable scope. Never publish blocked URLs containing credentials.

## Post-incident evidence

Retain egress/worker logs with URLs redacted, configuration state, policy test
results, provider revocation times, and staged recovery evidence.
