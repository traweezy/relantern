# Queue recovery runbook

Use this runbook only for a private worker environment after the underlying
failure is understood and corrected. A manual retry can repeat an external side
effect, so verify the occurrence or delivery idempotency key before proceeding.

## Inspect

List up to 50 discarded jobs without exposing encoded arguments or error
payloads:

```sh
docker compose run --rm worker dead-letters --limit 50
```

Record the job ID, kind, queue, attempt count, finalization time, related
incident/request ID, and the corrected root cause. Do not update River tables
directly. Do not retry a completed, available, scheduled, retryable, or running
job.

## Retry

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

## Verify

- Confirm the job moves through available/running to completed or returns to a
  bounded retry/discarded state.
- Confirm the associated occurrence reaches its expected terminal state.
- Confirm downstream capture or delivery contains only one idempotency key.
- Confirm the worker readiness timestamp remains within three minutes and note
  the reported oldest overdue occurrence.

Escalate instead of retrying when the payload is invalid, authorization has
changed, the target operation is no longer safe, or downstream idempotency
cannot be proven.
