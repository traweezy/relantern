# AI extraction operations

Use this runbook for `extract_item` failures, budget blocks, schema review, or
prompt/model registry drift. Fast extraction never authorizes web search, shell
execution, file writes, deployments, or delivery.

## Safety boundary

- The default local stack uses only `http://fake-openai:8091`.
- Do not place a production OpenAI key in Compose, GitHub Actions, logs, shell
  history, or the browser.
- Do not edit an active prompt or schema digest to force a retry.
- Do not raise or bypass the hard cap during incident response without owner
  cost review.
- Production migrations are forward-only. Never run `migrate down` outside a
  disposable local or test database.

## Inspect current state

Use read-only queries first:

```sql
select
    run.id,
    run.item_id,
    run.revision_id,
    run.state,
    run.attempt_count,
    run.input_tokens,
    run.cached_input_tokens,
    run.output_tokens,
    run.estimated_cost_usd,
    run.error_code,
    run.started_at,
    run.completed_at
from app.ai_runs run
order by run.started_at desc
limit 50;

select
    attempt.ai_run_id,
    attempt.attempt_number,
    attempt.state,
    attempt.reserved_cost_usd,
    attempt.estimated_cost_usd,
    attempt.budget_soft_alert,
    attempt.error_code,
    attempt.started_at,
    attempt.completed_at
from app.ai_run_attempts attempt
order by attempt.started_at desc
limit 100;
```

Monthly ledger total:

```sql
select coalesce(sum(estimated_cost_usd), 0)::numeric(14, 8) as month_cost_usd
from app.ai_run_attempts
where started_at >= date_trunc('month', now())
  and started_at < date_trunc('month', now()) + interval '1 month';
```

## Triage by error

| State/error | Meaning | Action |
|---|---|---|
| `failed_retryable` / `provider_error` | Timeout, transport, or provider failure | Check provider status and worker network, then use the audited River retry flow after recovery. |
| `budget_blocked` / `budget_exceeded` | The next worst-case reservation exceeds the monthly hard cap | Confirm the monthly ledger and wait for reset, reduce approved workload, or complete an owner-reviewed cap change. Never bypass the reservation. |
| `needs_review` / `schema_invalid` | Both strict-output attempts failed | Inspect prompt/schema compatibility and the sanitized fixture; create a new version rather than mutating the active one. |
| `needs_review` with no error | Output is span-valid but empty, or a T2/T3 source made a material claim | Hold publication and obtain T0/T1 evidence or owner review. |
| `needs_review` / `configuration_drift` | File and registry model/prompt/schema identity differ | Stop workers, compare migration 8 registry values to the versioned files, and deploy a reviewed forward migration. |
| `needs_review` / `revision_integrity` | Object bytes, digest, UTF-8, or outline offsets differ | Quarantine the revision and investigate object-storage or parser integrity. Do not retry the provider. |
| `obsolete` | A newer revision became current during extraction | No action; enqueue the current revision identity. |

Inspect discarded jobs with `docker compose run --rm worker dead-letters` and
follow `queue-recovery.md` for audited retries. Job retry does not erase attempt
history or budget reservations.

## Containment and recovery

To contain provider calls in a hosted environment, set
`OPENAI_FAST_ENABLED=false` in that environment and roll the worker back through
the normal deployment path. Deterministic ingestion and search remain usable.
Do not delete AI runs or claims during containment.

Before re-enabling:

1. Run `make test-extraction` and `make prepush` on the exact release.
2. Confirm model, reasoning, verbosity, max tokens, prompt hash, and schema hash.
3. Confirm the monthly ledger is below the reviewed hard cap.
4. Verify the provider key belongs to the correct isolated environment project.
5. Retry only affected River jobs through the audited operation.
6. Watch retry rate, schema failures, material evidence coverage, and cost.

Escalate immediately if a provider call occurs after a hard-cap rejection, a
material claim lacks a persisted evidence span, local/test traffic reaches a
live provider, or prompt-injection text affects tool authority.
