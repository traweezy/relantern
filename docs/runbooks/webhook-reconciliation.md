# OpenAI webhook reconciliation

Use this runbook when an OpenAI background response appears stuck, a webhook is
duplicated or rejected, or `reconcile_openai_background` reports failures.

## Safety boundary

- Never parse or forward a webhook before the public Next.js route verifies its
  signature against the untouched raw body.
- Never treat a webhook payload as model output. Workers must retrieve the
  response by provider response ID.
- Do not delete webhook events, AI runs, attempts, or River jobs to force
  progress. Do not replay an event from another environment.
- `OPENAI_WEBHOOK_SECRET` and `WEB_INTERNAL_SERVICE_TOKEN` are distinct per
  environment and must not be printed, committed, or exposed to the browser.

## Inspect durable state

```sql
select
    webhook_id,
    event_id,
    event_type,
    response_id,
    event_created_at,
    received_at,
    processed_at,
    processing_error
from app.openai_webhook_events
order by received_at desc
limit 100;

select
    id,
    cluster_id,
    provider_response_id,
    state,
    attempt_count,
    error_code,
    updated_at
from app.ai_runs
where background
order by updated_at desc
limit 100;

select id, state, attempt, max_attempts, scheduled_at, errors
from river.river_job
where kind in ('poll_openai_background', 'reconcile_openai_background')
order by id desc
limit 100;
```

Expected behavior:

- An exact duplicate webhook has one event row and is acknowledged as a
  duplicate without another effective transition.
- A received but unprocessed event has a poll job pending, available, running,
  retryable, or discarded with an auditable error.
- A running background AI run without a webhook is picked up by the periodic
  reconciler within 15 minutes.
- Completion records `processed_at`; a permanent validation failure also records
  `processing_error` and moves the run to review or another terminal state.

## Triage

| Symptom | Likely cause | Action |
|---|---|---|
| Public route returns 400 | Signature, timestamp, event type, or identifier invalid | Confirm the provider endpoint and environment secret. Do not bypass verification. |
| Public route returns 502 | Private API unavailable, timeout, or service-token mismatch | Restore private routing and token parity; OpenAI will retry delivery. |
| Event persists without a job | Transaction or River schema failure | Check API logs and database health; the event insert and enqueue should roll back together. |
| Run stays `running` without an event | Missed or delayed webhook | Confirm the 15-minute reconciler is scheduled; inspect its River job and worker health. |
| Poll repeatedly snoozes | Provider response remains queued/in progress | Check provider status and request age; do not start duplicate manual work. |
| `provider_id_mismatch` | Poll identity does not match the durable run | Quarantine the event and investigate routing or provider-project isolation. |
| `schema_invalid` or `configuration_drift` | Output or registry identity failed closed | Follow `ai-extraction.md`; do not mutate active digests. |
| `attempts_exhausted` | Five provider attempts failed | Leave in review, investigate provider health, and retry only through the audited queue flow after recovery. |

## Recovery

1. Verify `web`, private `api`, PostgreSQL, and `worker` health.
2. Confirm the webhook secret, internal service token, OpenAI project, and base
   URL all belong to the same environment without displaying their values.
3. Confirm `reconcile_openai_background` has run in the last 15 minutes.
4. Let reconciliation enqueue the missing poll. For a discarded job, follow
   `queue-recovery.md` and retry only after the cause is fixed.
5. Verify one terminal AI state, one processed webhook event when present, and
   the expected usage/tool ledger.

Escalate immediately if an unverified event reaches the private API, one
response ID updates multiple runs, a duplicate causes two completed briefs, or
local/test traffic reaches the live OpenAI API.
