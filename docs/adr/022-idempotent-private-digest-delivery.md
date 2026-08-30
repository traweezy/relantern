# ADR-022: Immutable private digest preparation and delivery

Status: Accepted
Date: 2026-08-29

## Context

PR 17 turns the durable daily occurrence ledger into an owner-visible digest.
The worker can restart between candidate capture, final ranking, provider
acceptance, and response persistence. Owner state can also change after the
eligibility cutoff. Rebuilding a payload during retry would make the audit
record ambiguous and could send different content under one logical delivery.

The default local environment must remain disconnected from Discord and
Resend while still exercising the complete request and retry shape.

## Decision

- A daily occurrence moves through typed River jobs for Priority 0 preflight,
  candidate preparation, finalization, and delivery. Every stage is
  transactionally enqueued and safe to replay.
- Preparation freezes a deterministic evidence window ending fifteen minutes
  before the scheduled instant. The prior delivered dashboard cutoff is the
  next window start; the first run uses twenty-four hours. Windows over
  thirty-six hours carry an explicit backlog summary.
- Preflight records stale and due Priority 0 endpoint counts on the occurrence.
  It does not invent polling work where no reviewed connector worker exists;
  continuous ingestion remains the source adapter's authority.
- Candidate snapshots are immutable. Finalization rechecks archive, snooze,
  Later, source-mute, digest-exclusion, suppression, and Radar-reject state,
  then applies deterministic score, category quota, and tie-break rules.
- One immutable rendered payload and SHA-256 digest is stored per owner, local
  date, and channel. Dashboard delivery completes in the same transaction.
  Discord and email receive a separate delivery ledger and River job.
- Retried delivery reuses the same payload bytes, payload hash, and provider
  idempotency key. It never reranks or regenerates the digest.
- Run Now always creates an idempotent occurrence. It defaults to
  dashboard-only; the caller must set `deliver=true`, and the configured
  environment delivery fuse must also pass, before an external channel is
  eligible.
- Local/test delivery mode posts only to the loopback or `fake-delivery`
  capture endpoint. Hosted environments reject capture mode. Live mode
  requires `ALLOW_LIVE_DELIVERY=true` and at least one explicitly enabled
  channel.
- Discord uses a private incoming webhook, disables all mentions, escapes
  untrusted text, waits for the provider response, and caps the payload at ten
  items. Resend receives accessible HTML and text without tracking pixels.

## Reliability and observability objectives

- Ninety-nine percent of daily digests should be visible by 08:05 local time
  over a rolling thirty-day window.
- A duplicate occurrence or stage replay creates no second digest or provider
  capture.
- An ordinary provider failure is retried with River backoff up to eight
  attempts. Permanent provider rejection is discarded and shown as failed.
- Digest history exposes channel state, immutable window, generated time,
  attempt count, and bounded error code. Provider credentials, authorization
  headers, and destination addresses are never returned or logged.
- Operations counts delivery attempts and retains the occurrence and River job
  correlation needed for recovery.

## Security and rollback

Only the authenticated owner can list digests or request a failed-delivery
retry. The web BFF adds the private service credential and owner identity; the
Go API is not public. Delivery URLs are exact-host validated, timeouts are
bounded, responses are size-limited, and secrets remain server-side.

Hosted rollback is application-first and forward-only. Retain migration 19,
immutable payloads, candidate snapshots, attempts, and audit evidence. Disable
delivery before rolling back the worker. Correct schema defects with a new
additive migration.

## Consequences

The owner can inspect exactly what was delivered and safely retry a failed
attempt. The local capture viewer exercises provider payloads without granting
network authority. Priority-source preflight is observable now; adding a
reviewed continuous polling worker remains separate connector work.
