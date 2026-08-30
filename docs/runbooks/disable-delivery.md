# Disable delivery runbook

## Trigger

- A wrong recipient, duplicate, compromised webhook/API key, or unsafe payload
  is suspected.
- Provider behavior is degraded or a release rollback is beginning.
- Staging/production recipient isolation cannot be proven.

## Impact

Discord and email stop. Dashboard digests, continuous ingestion, ranking,
search, and immutable history continue. Existing in-flight provider requests
may finish before the worker stops.

## Immediate containment

1. Set the hosted worker `DELIVERY_MODE` to `disabled`.
2. Set `ALLOW_LIVE_DELIVERY`, `DISCORD_ENABLED`, and `RESEND_ENABLED` to
   `false`.
3. Restart only the worker and wait for the old instance to terminate within
   its graceful-shutdown timeout.
4. Revoke a suspected Discord webhook or Resend key at the provider after the
   worker is stopped. Do not paste the old value into logs or incident notes.

## Exact verification commands

For the disconnected local profile:

```sh
make config-check
docker compose config | rg 'DELIVERY_MODE|ALLOW_LIVE_DELIVERY|DISCORD_ENABLED|RESEND_ENABLED'
make digest-run
curl --fail --silent --show-error http://127.0.0.1:8092/captures
```

The ordinary `make digest-run` must complete dashboard-only and add no capture.
Hosted verification uses the same four variable names in the Railway worker
service and confirms the worker restart SHA before any retry.

## Recovery

1. Correct recipient isolation, payload validation, or provider credentials.
2. Rotate affected secrets and complete the secret-rotation runbook.
3. Validate in staging with a staging-only recipient and one reviewed failed
   digest. Never copy production destinations into staging.
4. Re-enable one channel at a time. Set `DELIVERY_MODE=live` and
   `ALLOW_LIVE_DELIVERY=true` only in the same reviewed change.
5. Confirm a new delivery stores one provider ID and no duplicate capture.

## Data-integrity checks

- Dashboard digests continue while external delivery is disabled.
- Ready or failed external digests retain immutable payloads and hashes.
- No credential or destination appears in application logs, API responses, or
  capture headers.
- Staging and production hold distinct webhook, API-key, sender, and recipient
  values.

## Communication

Record disabled channels, environment, release SHA, time, owner impact, and
whether secret revocation occurred. Never record secret values or recipient
addresses.

## Post-incident evidence

Retain variable-state screenshots with values redacted, worker restart
evidence, provider revocation timestamps, affected digest IDs, and safe
re-enable verification.
