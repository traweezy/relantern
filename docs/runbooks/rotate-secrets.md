# Rotate secrets runbook

## Trigger

- Scheduled credential rotation, suspected exposure, staff/device loss, or a
  provider security event.
- Authentication/service-token failures indicate possible credential drift.

## Impact

OAuth login, private API calls, source access, AI, storage, or delivery may be
temporarily unavailable. Rotation must not cross staging/production identities.

## Immediate containment

1. Disable the affected feature and external delivery/fetching before revoking
   a credential used by in-flight work.
2. Identify the smallest affected secret and environments. Never copy a
   production value into staging or local.
3. Revoke confirmed exposed credentials at the provider and preserve only the
   revocation timestamp and credential identifier, not its value.

## Exact verification commands

```sh
make config-check
make auth-smoke
make digest-run
curl --fail --silent --show-error http://127.0.0.1:8080/readyz >/dev/null
```

## Recovery

1. Create a new credential with least privilege in the correct environment.
2. Update every consumer in one reviewed maintenance window. For dual-key
   providers, add new, restart/verify, then revoke old.
3. Rotate owner sessions after OAuth/Better Auth changes and reject prior
   cookies. Restart only services that consume the changed secret.
4. Verify a bounded operation, then re-enable one provider/channel at a time.

## Data-integrity checks

- Staging and production values, recipients, OAuth apps, and webhook endpoints
  remain distinct.
- No secret appears in git, logs, shell history, screenshots, build artifacts,
  `NEXT_PUBLIC_*`, or client bundles.
- Durable jobs retry without duplicate delivery or regenerated payloads.

## Communication

Record secret type, environments, rotation/revocation times, service restart
SHAs, verification result, and session impact. Redact all values.

## Post-incident evidence

Retain provider audit events, redacted variable-name screenshots, restart and
smoke evidence, gitleaks result, and follow-up cadence. Never retain old values.
