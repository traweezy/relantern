# ADR-027: Confirmed watched-dependency critical alerts

Status: Accepted
Date: 2026-09-20

## Context

A security story is not itself an urgent alert. The owner needs a prompt alert
only when a published critical advisory from an official GitHub source names an
active watched package and proves that its installed version is affected. The
default local stack must keep external delivery disconnected.

## Decision

The episode behavior below is the cutover target. Migrations 32 through 35
add observation and episode evidence while the original one-alert uniqueness
constraint remains in force; a corrected-to-critical return cannot admit an
episode-two alert during that additive stage.

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
- Persist an immutable alert episode per owner, GHSA, ecosystem, package, and
  episode number. A later validated return to critical after a correction
  opens a new episode with a new delivery identity. The dashboard is mandatory.
  Discord and email are optional owner settings
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
  labels the correction and episode number, while Today counts only the latest
  active episode for each owner and affected package. A still later validated
  correction updates that episode's displayed correction reason and revision
  without changing its original alert. A correction from a
  different source entry does not overturn the admitted evidence without a
  reviewed authority rule. A later critical observation from that other entry
  is recorded and acknowledged without reopening the corrected alert, so it
  cannot block subsequent observations from its source.
- Record every official collection fetch as a source-ordered observation,
  including pages whose retained bytes match earlier raw evidence. Record each
  child entry in page order before marking the collection processed. A
  collection is processed only after every entry assessment completes; an empty
  validated page is processed after its split is recorded. A failed assessment
  holds later observations for that source pending, preserving critical to
  corrected to critical transitions even when an entry reappears byte-for-byte.
  Existing raw and delivery ledgers remain immutable evidence; a new episode
  receives a fresh provider idempotency key.

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
The private `advisory_observation_pending` and
`advisory_observation_oldest_age_seconds` gauges expose collection assessments
that remain incomplete, without source, advisory, or owner labels. An oldest
pending observation older than ten minutes raises
`advisory_observation_stalled`. The age starts at the original fetch time,
including byte-identical repeats.
Split work has its own single-worker River queue because it holds an
observation transaction while recording children with a second database
connection. Measure a full advisory page against hosted object storage before
raising that concurrency or the two-minute split timeout.
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

Apply migrations 26 through 35 as an additive release, then deploy compatible
workers, API, and owner reads together. The observation ledger records every
page and ordered child entry, but a corrected-to-critical return remains
pending behind the legacy uniqueness guard. It must not be marked processed or
sent as a reused delivery. Verify a confirmed fixture, a rumor fixture,
quiet-hour behavior, safe capture, and a paused A to B to A replay at this
stage. Only after ordered assessment and owner-read replay tests pass should a
separate forward cutover migration remove the legacy uniqueness constraint.
Then enable episode-two admission and verify the full A to B to A replay with
distinct delivery identities.

Retention holds a source row lock through raw-object deletion so a concurrent
identical capture cannot lose its evidence; this requires
`DATABASE_MAX_CONNS` to be at least 2 (the default is 20), and startup fails
otherwise. Before cutover, a rollback to a worker that predates observations
must disable critical assessment until a compatible worker resumes pending
observations in order. After cutover, do not roll back to a worker that assumes
one alert per owner and advisory package; disable critical assessment and
external delivery until a compatible worker is restored. A rollback to a
worker that predates migration 29 must also disable external critical alert
delivery: the older claim path does not recognize `suppressed`. Retain the
additive ledgers and repair schema through a later forward migration. Do not
delete evidence or ledger rows to replay a delivery.
