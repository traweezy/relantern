# Radar discovery stalled runbook

## Trigger

- `/radar` shows a run in `queued` or `running` beyond two minutes.
- The maintenance queue has retryable or discarded
  `run_weekly_radar_discovery` or `refresh_package_metrics` jobs.
- Evidence remains unprocessed after the worker is healthy.

## Impact

New package observations and comparison snapshots are delayed. Existing Radar
states, decision history, and review dates remain authoritative. No package is
installed, executed, or adopted by the recovery path.

## Immediate containment

1. Keep all live-provider and package-execution paths disabled.
2. Open `/ops` and record maintenance queue depth, job kind, attempt, and age.
3. Open `/radar` and record the run ID, state, requested time, and counts. Do
   not paste evidence payloads or owner profile data into incident logs.
4. If repeated evidence is malformed, stop its upstream adapter before retrying
   the worker job.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api
bash scripts/go-tool.sh test ./internal/radar/... ./internal/worker -count=1
make test-integration
```

List discarded jobs with the existing bounded operator command:

```sh
docker compose run --rm worker dead-letters -limit 50
```

Use the following only after fixing the cause and confirming the target is a
Radar job:

```sh
docker compose run --rm worker retry-job -id <job-id> \
  -actor-id <owner-id> -reason '<incident-linked reason>'
```

## Recovery

1. Correct malformed evidence or the deterministic assessment/storage defect.
2. Run unit and integration checks. Confirm 10 credible and 10 misleading
   fixtures retain their expected classification.
3. Retry the exact discarded job, or request a new owner discovery run if the
   old run completed or was permanently cancelled.
4. Confirm the run reaches `completed`, evidence rows receive `processed_at`,
   and one immutable metric/comparison pair exists per evidence row.
5. Confirm current owner decisions did not change unless the owner explicitly
   submitted a new decision.

## Data-integrity checks

- One `river_job_id` and one idempotency key identify each discovery run.
- No system decision has state `adopt`.
- Every comparison has seven dimensions.
- Every decision has a future review date and nonempty applicability,
  compatibility, and exit arrays.
- Reprocessing completed evidence does not create duplicate metric snapshots.

## Communication

Record the affected run/job IDs, queue delay, owner-visible staleness,
containment, recovery time, and whether any decision required review. Do not
include evidence payloads or owner profile data.

## Post-incident evidence

Retain the affected run/job IDs, bounded error code, recovery timestamp, queue
drain evidence, and regression-test commit. Never retain database credentials,
raw owner data, or unredacted third-party payloads in the incident record.
