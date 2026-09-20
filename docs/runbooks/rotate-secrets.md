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
3. On Railway, never reference a sealed variable from another service; sealed
   values do not resolve through cross-service references and cannot be
   unsealed. Enter the same rotated `DATABASE_URL` separately for `postgres`,
   `migrate`, `api`, `worker`, and `web`, and enter the same rotated
   `WEB_INTERNAL_SERVICE_TOKEN` separately for `api` and `web` and the same
   rotated `OPENAI_API_KEY` separately for `api` and `worker`. Seal every copy
   only after all values and the PostgreSQL role are synchronized.
4. Rotate owner sessions after OAuth/Better Auth changes and reject prior
   cookies. Restart only services that consume the changed secret.
5. Verify a bounded operation, then re-enable one provider/channel at a time.

## Data-integrity checks

- Staging and production values, recipients, OAuth apps, and webhook endpoints
  remain distinct.
- Railway environment configuration reports every hosted secret copy as
  sealed, with no cross-service reference used for a sealed value.
- No secret appears in git, logs, shell history, screenshots, build artifacts,
  `NEXT_PUBLIC_*`, or client bundles.
- Durable jobs retry without duplicate delivery or regenerated payloads.

## Communication

Record secret type, environments, rotation/revocation times, service restart
SHAs, verification result, and session impact. Redact all values.

## Post-incident evidence

Retain provider audit events, redacted variable-name screenshots, restart and
smoke evidence, gitleaks result, and follow-up cadence. Never retain old values.
