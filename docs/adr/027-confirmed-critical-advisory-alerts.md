# ADR-027: Confirmed watched-dependency critical alerts

Status: Accepted
Date: 2026-09-20

## Context

A security story is not itself an urgent alert. The owner needs a prompt alert
only when a published critical advisory from an official GitHub source names an
active watched package and proves that its installed version is affected. The
default local stack must keep external delivery disconnected.

## Decision

- Keep advisory metadata in bounded, immutable source-entry child objects.
  Verify the registered HTTPS GitHub endpoint, source trust tier, child object
  key and digest, GHSA identity, and matching public advisory URL before
  matching. A supporting item source remains eligible; a superseded source
  entry does not.
- Poll the reviewed, update-ordered GitHub Advisory Database feed every five
  minutes, along with enabled repository advisory endpoints. Fetch the pinned
  first page on every poll, then at most two validated continuation pages.
  Commit each page, its raw evidence, and the next cursor together. The root
  page alone uses its conditional validators; continuation pages cannot claim
  a 304 as coverage. After a complete pass, force an unconditional first page
  at least daily to start another pass even when its ETag has not changed. A
  persisted cursor survives worker restart, and a failed page leaves the last
  committed cursor available for the next poll. The feed's
  [CC BY 4.0 license](https://github.com/advisories)
  permits the retained excerpt policy with source attribution; release notes
  keep their separate metadata-only policy. Enabling live source polling
  requires a worker-only GitHub read token; the default local profile does not
  load one.
- Require an exact ecosystem and package name, a reviewed global advisory or
  published repository advisory, critical severity, no withdrawal, and an
  affected installed version. The urgent matcher currently supports Go, npm,
  Rust, and pub with simple numeric comparator ranges. Other ecosystems and
  unsupported version syntax remain review-only. Legacy watches with no
  proven ecosystem cannot trigger an urgent alert.
- Persist one immutable alert per owner, GHSA, ecosystem, and package. The
  dashboard is mandatory. Discord and email are optional owner settings
  independent of digest channels. Queue selected external deliveries in the
  same transaction as alert admission, with one stable idempotency key and
  payload hash per alert/channel. Default local delivery uses the fake capture.
- Bypass quiet hours only when the owner permits it; otherwise defer external
  delivery until the next local quiet-hour end. The dashboard record appears
  immediately. River retries transient provider failures and records each
  attempt. A bounded minute-by-minute reconciliation sweep restores missing
  jobs for due pending and failed deliveries and interrupted sends. Exhausted
  transient attempts have a five-minute cooldown before replay, preserving the
  delivery identity and provider idempotency key. Genuine permanent failures
  retain the alert and delivery ledger for reviewed intervention.
- Reassess already ingested current advisories after relevant active watch
  changes using bounded keyset jobs. The assessor remains the single matcher.
- When a newer validated child of the same official source entry explicitly
  withdraws or closes an advisory, or changes its severity away from critical,
  retain the original alert and record the correction revision. Suppress pending
  and retryable external deliveries before another attempt. A send already in
  flight may complete; retain its provider receipt, and suppress a failed send
  instead of retrying.
  An omitted severity alone is insufficient correction evidence. Owner history
  labels the correction, while Today counts only currently confirmed critical
  alerts. A still later validated correction updates the displayed correction
  reason and revision without changing the original alert. A correction from a
  different source entry does not overturn the admitted evidence without a
  reviewed authority rule.
- Correction suppression is terminal in this staging slice. A later return to
  critical cannot reuse the original alert or provider idempotency key. Before
  production rollout, add a new immutable alert episode with a fresh key and
  handle byte-identical advisory content reappearing after a correction;
  source-entry raw digest deduplication currently hides that transition.

## Reliability and security

The objective is for 95% of confirmed watched-dependency critical advisories
to appear within ten minutes of first observation. Track `observed_at` to
`created_at` in `app.critical_alerts`, pending/failed delivery age in
`app.critical_alert_deliveries`, and attempts in `app.critical_alert_attempts`.
The private `critical_alert_undelivered_count` metric and
`critical_advisory_undelivered` signal expose permanently failed deliveries
and unsent external deliveries older than ten minutes after due or first
attempt. Quiet-hour deferrals are excluded until due. The reconciler also logs
the overdue and recovery counts. Corrected alerts and suppressed deliveries
are excluded from overdue counts; their original evidence and attempts remain
available in owner history. Private `critical_alert_corrected_count` and
`critical_alert_delivery_suppressed_count` gauges expose correction volume
without advisory IDs or owner labels.
The private `reviewed_advisory_scan_pending` and
`reviewed_advisory_scan_age_seconds` gauges expose incomplete pagination. A
scan still pending after 24 hours raises `reviewed_advisory_scan_stalled`.
The bounded recent-page fingerprint window and 10,000-page scan ceiling stop
cursor cycles and expose `reviewed_advisory_scan_invalid` for review while the
first page continues refreshing.
GitHub's update-ordered cursor is not a stable snapshot: a completed pass
means the Link chain ended, while the repeated root poll and later passes
provide overlap as advisories move between pages.
Keep the existing critical River queue and worker metrics under observation;
use `docs/runbooks/security-advisory-missed.md` and
`docs/runbooks/duplicate-alert.md` for incident handling. Provider acceptance
after a network timeout can be ambiguous, particularly for Discord; keep the
same key on retry and verify provider records before manual replay.

The official source URL is validated before it becomes a browser or message
link. The dashboard still requires an owner session, and delivery payloads are
HTML escaped or Discord escaped with mentions disabled. Local, staging, and
production delivery fuses remain in force.

## Rollout and rollback

Apply migrations 26 through 31 before the API and worker. Deploy the API and web
with the worker so new queue kinds have registered consumers. Verify a
confirmed fixture, a rumor fixture, quiet-hour behavior, and the safe capture
viewer. A rollback to a worker that predates migration 29 must disable external
critical alert delivery until the compatible worker is restored: the older
claim path does not recognize `suppressed`. Retain the additive ledgers and
repair schema through a later forward migration. Do not delete evidence or
ledger rows to replay a delivery.
