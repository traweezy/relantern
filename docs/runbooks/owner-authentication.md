# Owner authentication runbook

Use this runbook when owner sign-in fails, an OAuth credential must rotate, or a
browser session must be revoked. Never print OAuth secrets, Better Auth secrets,
session tokens, authorization codes, or raw provider profiles into a terminal,
ticket, or log.

## Triage

1. Confirm `web`, PostgreSQL, and the provider are healthy. In local development,
   the provider is `fake-source`; in staging and production it is GitHub.
2. Confirm the environment has an exact public `BETTER_AUTH_URL`, numeric
   `AUTH_ALLOWED_GITHUB_USER_ID`, database URL, and the provider credentials for
   that environment. Do not display their values.
3. Confirm the GitHub OAuth callback is exactly
   `${BETTER_AUTH_URL}/api/auth/callback/github`.
4. Inspect structured web logs for `auth_api_error`. Provider errors are
   intentionally neutral and do not disclose whether another GitHub account was
   recognized.
5. If the sign-in button renders but does not respond, inspect the login response
   CSP and script tags. Every script must carry the request's nonce; a partially
   prerendered shell is invalid under the strict nonce policy.
6. In local development, run `make auth-smoke`. It must remain disconnected from
   live GitHub unless the live-provider fuse was separately authorized.

## Owner lockout

- Verify the numeric GitHub user ID, not the mutable login or email address.
- Verify the OAuth app belongs to the current environment and its callback URL
  exactly matches the deployed public origin.
- Verify migration 11 is applied and the owner row has a verified synthetic
  email ending in `@github.relantern.local`.
- Do not weaken the allowlist, enable another provider, disable CSRF checks, or
  make an application route public to recover access.
- If GitHub is unavailable, wait for provider recovery. The local fixture is not
  an acceptable hosted bypass.

## Rotate the GitHub client secret

1. Create a new secret in the environment-specific GitHub OAuth app.
2. Seal it as `GITHUB_OAUTH_CLIENT_SECRET` in the matching Railway environment.
3. Redeploy only `web` and complete one owner sign-in.
4. Revoke the prior GitHub secret after the new deployment passes.
5. Record the change and evidence without recording either secret.

Existing Relantern sessions remain valid; revoke them too when rotation responds
to suspected account or token compromise.

## Rotate the Better Auth secret

1. Generate at least 32 random bytes in the environment's secret manager.
2. Seal the new value as `BETTER_AUTH_SECRET` and redeploy `web`.
3. Expect all existing cookies and in-progress OAuth state to become invalid.
4. Revoke database sessions as described below if the rotation responds to
   suspected compromise.
5. Complete a new owner sign-in and verify `/`, `/login`, and sign-out.

## Revoke sessions

Count sessions first without selecting tokens:

```sql
select count(*) from app.auth_sessions;
```

For an incident requiring global logout, use an attended transaction after
capturing the count and change record:

```sql
begin;
delete from app.auth_sessions;
commit;
```

This is intentionally destructive to login state but not owner or product data.
Do not delete accounts or users. Verify an old browser is redirected to
`/login`, then complete a fresh owner sign-in.

## Local verification

With the production-like stack running:

```sh
make auth-smoke
```

The check validates the signed-out redirect, exact public routes, protected API
401, PKCE fixture callback, HttpOnly SameSite=Lax session cookie, private shell,
and session revocation on sign-out. It never contacts GitHub.
