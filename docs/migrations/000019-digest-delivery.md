# Digest delivery migration 000019

## Scope

Migration `000019_digest_delivery.sql` additively introduces:

- `app.digest_candidates`, an immutable per-occurrence eligibility snapshot.
- `app.digests`, the owner/date/channel payload, SHA-256 hash, state, and
  provider idempotency identity.
- `app.digest_items`, the selected order, score, category, rationale, and
  evidence snapshot.
- `app.delivery_attempts`, the bounded external-attempt ledger.

Constraints bound payload sizes, scores, item counts, states, error codes, and
candidate shapes. Unique keys enforce one channel payload per owner and local
date, one payload per occurrence/channel, and one delivery identity per
digest/channel.

## Forward rollout

1. Keep `DELIVERY_MODE=disabled` in hosted environments.
2. Apply migration 19 before starting the updated API or worker.
3. Deploy the private API and worker before the web application.
4. Run `make test-integration`; confirm stage replay, immutable retry, one
   provider capture, and owner-scoped digest reads pass.
5. Run `make digest-preview`; confirm JSON renders without adding a digest or
   provider capture.
6. Run `make digest-run`; confirm a dashboard-only run completes. Use
   `make digest-run deliver=true` only with the reviewed local capture sink or
   separately approved hosted delivery configuration.
7. In staging, enable one test recipient at a time and verify the stored hash,
   attempt count, provider ID, and received payload before enabling the next
   channel.

## Rollback

Hosted rollback is application-first and forward-only. First set
`DELIVERY_MODE=disabled`, restart the worker, and verify no delivery job is
running. Roll back web, API, and worker while retaining migration 19 and its
evidence. A follow-up additive migration corrects schema defects.

The Goose `down` block is restricted to disposable local/test databases. It
deletes digest and delivery history and must never run in staging or
production.

## Variables and permissions

Worker configuration adds `DELIVERY_MODE`, `ALLOW_LIVE_DELIVERY`,
`DELIVERY_REQUEST_TIMEOUT`, `DISCORD_ENABLED`, `RESEND_ENABLED`, and
`PUBLIC_BASE_URL`. Local capture mode also requires `FAKE_DELIVERY_URL`.

Discord live delivery requires sealed `DISCORD_WEBHOOK_URL`. Resend live
delivery requires sealed `RESEND_API_KEY` plus `DELIVERY_EMAIL_FROM` and the
staging- or production-specific `DELIVERY_EMAIL_TO`; `RESEND_API_URL` remains
pinned to `https://api.resend.com/emails`.

The worker needs outbound HTTPS only to `discord.com` when Discord is enabled
and `api.resend.com` when email is enabled. No public service or GitHub write
permission is added. Only `web` remains public.
