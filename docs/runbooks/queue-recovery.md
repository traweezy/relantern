# Queue recovery runbook

Use this runbook only for a private worker environment after the underlying
failure is understood and corrected. A manual retry can repeat an external side
effect, so verify the occurrence or delivery idempotency key before proceeding.

## Trigger

- A River job is cancelled or discarded after its bounded attempts.
- Queue age or depth remains above the documented alert threshold.

## Impact

The job's ingestion, parsing, AI, maintenance, or delivery outcome is delayed.
An unsafe retry could duplicate a downstream side effect.

## Immediate containment

- Stop broad retries and correct the underlying provider, data, or code fault.
- Preserve job, occurrence, request, and idempotency identifiers without job
  arguments or error payloads.
- Disable the affected intake or delivery lane when idempotency is uncertain.

## Exact verification commands

List up to 50 discarded jobs without exposing encoded arguments or error
payloads:

```sh
docker compose run --rm worker dead-letters --limit 50
```

Record the job ID, kind, queue, attempt count, finalization time, related
incident/request ID, and the corrected root cause. Do not update River tables
directly. Do not retry a completed, available, scheduled, retryable, or running
job.

## Recovery

Retry one cancelled or discarded job with an owner identity and required audit
reason:

```sh
docker compose run --rm worker retry-job \
  --id 123 \
  --actor-id owner-github-id \
  --request-id incident-or-request-id \
  --reason 'Root cause corrected; downstream idempotency verified'
```

The state change and `app.audit_events` record commit in the same serializable
transaction. If either write fails, neither is retained. Retrying an ineligible
state is rejected.

For `parse_raw_document`, first check whether a current raw failure marker
remains (including `source_handoff_failed`). Fix the parser, storage, or enqueue
cause before retry. Raw-document replay through source reconciliation requires
an active, unpaused source and the live-source polling fuse. Enabling an AI
worker alone can reconcile stored current revisions without a new fetch.

For `reembed_entity`, `extract_item`, and `research_story`, confirm the job's
revision is still the current primary revision. For re-embedding, confirm the
current search document is pinned to an embedding with the configured model;
an immutable vector may be reused across identical normalized revisions.
Research also requires verified T0/T1 claims, owner digest admission before
cutoff, and a matching validated research input hash. A supporting
source can change those claims without changing the primary revision, and the
new input hash can queue fresh research. A superseded revision or claim-set job
may finish as obsolete; do not force it onto newer inputs. The bounded
capability-aware reconciler queues eligible new work when its worker is enabled,
but it does not automatically retry a cancelled or discarded job with the same
identity. Use the audited command above only after correcting its root cause
and checking idempotency. A T2/T3 current primary stays in review and is not
eligible for automatic extraction.

## Data-integrity checks

- Confirm the job moves through available/running to completed or returns to a
  bounded retry/discarded state.
- Confirm the associated occurrence reaches its expected terminal state.
- Confirm downstream capture or delivery contains only one idempotency key.
- Confirm a recovered source item has at most one effective downstream result
  for its current revision; stale revision jobs cannot publish current results.
- Confirm the worker readiness timestamp remains within three minutes and note
  the reported oldest overdue occurrence.

Escalate instead of retrying when the payload is invalid, authorization has
changed, the target operation is no longer safe, or downstream idempotency
cannot be proven.

## Communication

Record queue/kind, bounded job ID, attempts, corrected root cause, retry actor,
owner-visible delay, and recovery time. Never include encoded arguments.

## Post-incident evidence

Retain sanitized dead-letter output, audit event, idempotency verification,
terminal job/occurrence state, queue recovery metrics, and release SHA.
