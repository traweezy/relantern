# ADR-006: Next.js BFF authentication boundary

Status: Accepted
Date: 2026-08-29

## Context

The browser must never receive private-service credentials or directly reach
the Go API over Railway private networking. Proxy redirects improve navigation
but cannot establish authorization, and the public demo must remain independent
of authenticated data and private infrastructure.

## Decision

Use Next.js as the only public BFF. Better Auth 1.7.1 uses the PostgreSQL adapter,
GitHub OAuth, database sessions, and the existing `app.users` identity table.
The exact numeric `AUTH_ALLOWED_GITHUB_USER_ID` is checked against the fresh raw
provider profile before create, link, and returning OAuth sign-in. Session
creation independently verifies the persisted user's numeric GitHub ID.

Protected server components and future BFF data functions query the database-
backed session and recheck the persisted owner ID. Next.js Proxy checks only for
the presence of the opaque session cookie and provides optimistic redirects or
401 Problem Details. It is deliberately not an authorization decision.

The public route allowlist is exact: `/login`, Better Auth endpoints, the
verified OpenAI webhook, existing health/static assets, `/demo`, and later known
demo story fixtures. A prefix below `/demo` is not implicitly public.

The per-request nonce CSP makes every document route request-rendered. The root
layout calls `connection()` above the document shell and exports
`instant = false`; nonce CSP is incompatible with Cache Components partial
prerendering because build-time shell scripts cannot carry a request nonce.
This deliberately trades CDN-cached HTML and instant partial prerenders for a
strict script policy. Static fixture isolation describes the demo's data and
module boundary, not static HTML generation.

Better Auth uses:

- `relantern` cookie names with HttpOnly, SameSite=Lax, path `/`, and Secure on
  HTTPS deployments;
- seven-day database sessions, a 15-minute fresh-session window, and no cookie
  session cache, so revocation is immediately visible at data boundaries;
- database-backed rate limiting, with five social sign-in attempts per minute;
- hashed verification identifiers, encrypted OAuth tokens, PKCE, exact trusted
  origins, and the framework's CSRF/origin checks;
- verified synthetic internal email addresses derived from the GitHub numeric
  ID, minimizing retained provider PII while safely linking the seeded owner.

The default local stack registers a GitHub-shaped authorization-code fixture at
`fake-source`. It requires S256 PKCE, exact client/callback values, a one-use
five-minute code, a bounded in-memory token store, and a shared local-only
secret. Staging and production reject fixture mode and require independent real
GitHub OAuth credentials. Local live OAuth additionally requires the explicit
`ALLOW_LIVE_EXTERNAL_APIS=true` fuse.

Migration `000011_owner_auth.sql` expands `app.users` and adds dedicated
`auth_sessions`, `auth_accounts`, `auth_verifications`, and `auth_rate_limits`
tables. Production migration is forward-only. Rollback is a deployment rollback
that retains the additive columns/tables; the Goose down section is local-test
support, not a production rollback procedure.

## Consequences

Only `web` is publicly routed. Web now requires database access in addition to
the private API credential. Rotating the Better Auth secret invalidates browser
cookies, while server-side session rows can be revoked independently. The local
fixture cannot validate GitHub availability or consent UX, so staging must run a
real-provider smoke before production promotion.

The fixture-only `/demo` module imports no auth code and remains renderable when
the database, auth provider, and private services are unavailable.

## Verification

- `make auth-smoke` proves redirect, PKCE callback, cookie attributes, persisted
  owner session, protected workspace, sign-out, and matching request nonces on
  every login document script against the disconnected production-like stack.
- Unit tests cover exact route classification, environment fail-closed behavior,
  owner ID normalization, and provider PII minimization.
- The owner-auth runbook defines rotation, lockout, and revocation procedures.
