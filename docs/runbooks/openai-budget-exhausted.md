# OpenAI budget exhausted

Use this runbook for `budget_blocked`, `budget_exceeded`, or
`web_search_limit`. Limits are safety controls; incident response must not
bypass them.

## Inspect the ledgers

```sql
select coalesce(sum(estimated_cost_usd), 0)::numeric(14, 8) as month_cost_usd
from app.ai_run_attempts
where started_at >= date_trunc('month', now())
  and started_at < date_trunc('month', now()) + interval '1 month';

select coalesce(sum(
    case when state = 'reserved' then reserved_tool_calls else tool_calls end
), 0) as utc_day_web_search_calls
from app.ai_run_attempts
where started_at >= date_trunc('day', now())
  and started_at < date_trunc('day', now()) + interval '1 day';

select id, purpose, state, estimated_cost_usd, tool_calls, error_code, updated_at
from app.ai_runs
where state = 'budget_blocked'
order by updated_at desc;
```

Reserved attempts are intentionally counted at worst case until a response is
recorded. Interrupted attempts stay conservatively charged.

## Recovery

1. Confirm the configured monthly soft/hard USD limits and UTC daily search
   limit match the reviewed environment configuration.
2. Check for an unusual retry rate or long-lived reserved attempt before
   considering a configuration change.
3. Wait for the UTC day or calendar-month reset, reduce the authorized research
   workload, or prepare an owner-reviewed limit change with cost impact and
   rollback notes.
4. Retry blocked work only through the audited River flow after capacity is
   available. Never edit ledger rows or run state by hand.

Deterministic ingestion, ranking, and delivery continue without research.
Escalate if any provider call occurs after a hard-limit rejection or if actual
usage exceeds its reservation.
