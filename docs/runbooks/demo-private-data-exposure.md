# Demo private-data exposure runbook

## Trigger

- `/demo` emits a protected request, session-dependent data, private hostname,
  owner identifier, credential, note, recipient, provider ID, or current private
  operational value.
- A normalization attack crosses from a demo path into a protected route.

## Impact

The public security boundary is broken. Treat any exposed private data or
credential as compromised even if access logs appear limited.

## Immediate containment

1. Disable public demo routing or roll back `web`; keep the private application
   authenticated.
2. Preserve response/request graph, release SHA, URL bytes, headers, cache/CDN
   state, and first/last access. Do not share exposed content further.
3. Rotate any secret and invalidate any session that may have been serialized.

## Exact verification commands

```sh
make demo-audit
pnpm --filter @relantern/web test -- demo-sanitizer.test.ts route-policy.test.ts
rg -n '@/server/|/api/v1|/api/auth|process\.env' 'apps/web/src/app/(public)/demo' apps/web/src/features/demo
make prodlike-smoke
```

## Recovery

1. Remove the dependency/data path and replace any fixture with reviewed
   synthetic public-source data.
2. Add the exact encoded/normalized route and forbidden material to tests.
3. Build once, inspect the signed-out request graph with private services down,
   then restore demo routing.

## Data-integrity checks

- Demo renders with PostgreSQL, API, worker, auth provider, OpenAI, delivery,
  and object storage unavailable.
- Anonymous and owner responses are semantically equivalent except CSP nonce.
- No demo request reaches protected API/auth/SSE/provider/storage origins.

## Communication

Record exposed data class, URL, access window/count, containment, rotations,
owner impact, and notification decision. Do not reproduce private payloads.

## Post-incident evidence

Retain redacted request graph, route bytes, build SHA, cache purge evidence,
rotation records, corrected bundle analysis, and regression tests.
