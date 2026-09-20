# Security advisory missed runbook

## Trigger

- A confirmed watched-dependency critical advisory was not visible within ten
  minutes of first observation or did not bypass quiet hours.
- `critical_advisory_undelivered` fires, or
  `critical_alert_undelivered_count` is above zero in private metrics. This
  counts permanently failed external deliveries and deliveries still unsent
  ten minutes after they became due or were first attempted. A delivery
  waiting for its configured quiet-hour end is excluded until due.
- `reviewed_advisory_scan_stalled` fires after the reviewed global feed has an
  unfinished page cursor for more than 24 hours.
- `reviewed_advisory_scan_invalid` fires when the validated cursor chain
  repeats a recent page or exceeds its 10,000-page safety ceiling.

## Impact

The owner may act late on a material security issue. Ordinary ranking and
digest delivery must not hide the failure.

## Immediate containment

1. Verify the advisory from an official vendor or maintainer source; do not
   amplify an unconfirmed community claim.
2. Disable automated claims that lack primary evidence and record source,
   first-observed time, watched dependency match, and expected delivery path.
3. If delivery is unsafe, use the dashboard record and owner communication
   path without bypassing the immutable ledger.

## Exact verification commands

```sh
make sources-verify
make test-extraction
make test-scheduler
make digest-preview
docker compose logs --since=30m worker api fake-delivery
```

The reviewed global GitHub feed polls the first 100 advisories ordered by
`updated` descending every five minutes. [GitHub caps `per_page` at 100 and
exposes pagination cursors](https://docs.github.com/en/rest/security-advisories/global-advisories).
Each poll follows up to two additional official, validated `Link` pages. The
next cursor is committed with each stored page and survives worker restart.
Until the cursor clears, older coverage is incomplete; the live first page
continues to refresh. A completed scan starts a new unconditional root pass at
least daily. GitHub pagination is not a snapshot, so compare completed passes
if advisories moved during traversal.

Inspect the private checkpoint without copying the opaque cursor to an incident
ticket or logs:

```sql
select endpoint.registry_id, endpoint.health_state,
       checkpoint.cursor is not null as scan_pending,
       checkpoint.provider_state->>'advisoryScanStartedAt' as scan_started_at,
       checkpoint.provider_state->>'advisoryScanCompletedAt' as scan_completed_at,
       checkpoint.provider_state->>'advisoryScanIssue' as scan_issue,
       endpoint.last_success_at
from app.source_endpoints endpoint
left join app.source_checkpoints checkpoint on checkpoint.endpoint_id = endpoint.id
where endpoint.registry_id = 'github-global-advisories';
```
Repository advisories use retained source-entry evidence; repository release
endpoints remain metadata-only.

Check the immutable admission and delivery records in the private database:

```sql
select advisory_id, ecosystem, package_name, observed_at, created_at,
       created_at - observed_at as admission_delay
from app.critical_alerts
order by created_at desc
limit 20;

select id, channel, state, next_attempt_at, last_attempt_at,
       attempt_count, delivered_at
from app.critical_alert_deliveries
order by created_at desc
limit 20;

select delivery_id, attempt_number, outcome, error_code, attempted_at,
       completed_at
from app.critical_alert_attempts
order by attempted_at desc
limit 20;
```

## Recovery

1. Correct source polling, deterministic package matching, classification,
   critical queue priority, quiet-hour bypass, or delivery configuration.
   For a stalled global scan, inspect failed `app.source_fetches` attempts and
   provider `Retry-After`; do not paste or edit the opaque cursor. Correct a
   rejected page URL, repeated cursor, or provider response before resuming the
   source. The first page remains hot when a cursor cycle is held for review.
   After confirming a `cursor_cycle` was corrected upstream, an attended
   operator may clear only the issue and recent-page fingerprints to retry the
   retained cursor; keep the cursor and raw evidence unchanged:

   ```sql
   update app.source_checkpoints checkpoint
   set provider_state = checkpoint.provider_state
       - 'advisoryScanIssue' - 'advisoryScanRecentPages',
       updated_at = now()
   from app.source_endpoints endpoint
   where checkpoint.endpoint_id = endpoint.id
     and endpoint.registry_id = 'github-global-advisories'
     and checkpoint.provider_state->>'advisoryScanIssue' = 'cursor_cycle';
   ```

   A `page_limit` requires a reviewed coverage plan before changing the cap.
2. Reprocess the durable source revision; do not create a synthetic claim.
3. Verify one critical fixture reaches the safe capture path within target and
   one unconfirmed rumor does not.
4. If a watch was added after source ingestion, inspect the bounded watch
   reassessment jobs and the critical queue before replaying source evidence.
5. The worker reconciles due pending and failed deliveries, and interrupted
   sends, once per minute in bounded pages. It restores a missing River job
   using the ledger's original delivery identity and provider idempotency key.
   Exhausted transient provider failures wait five minutes before replay.
   Verify that the next sweep enqueues recovery and the ledger reaches `sent`.
6. A `permanent` delivery requires correction of the configuration or payload
   problem and an incident review before any replay. Provider acceptance can
   be ambiguous after a timeout, especially for Discord; check provider
   records before manual intervention. Never delete or rewrite ledger rows to
   force a resend.

## Data-integrity checks

- Advisory identity, affected package/range, severity, and primary source are
  preserved.
- One confirmed event maps to one urgent alert/delivery identity.
- Quiet-hour bypass is owner-enabled and applies only to confirmed criticals.

## Communication

Record verified facts, delay, owner exposure, affected dependency, remediation
status, and confidence. Link the primary advisory; do not overstate impact.

## Post-incident evidence

Retain fixture, timing trace, source revision/claim IDs, queue/delivery ledger,
root cause, and regression test.
