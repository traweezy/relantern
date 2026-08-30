# Manual capture and OPML recovery runbook

Use this runbook when a URL capture remains queued, fails, produces uncertain
lineage, or an OPML preview/import is rejected. Never enable an owner source,
edit a preview, or mutate a River row directly.

## Trigger

- A capture has not reached `completed` or `failed` within five minutes.
- `process_manual_capture` repeatedly retries or becomes discarded.
- An approved OPML commit returns conflict/not-found, or an imported source is
  unexpectedly enabled.
- Exported OPML or story metadata does not reflect the owner-scoped database.

## Impact

Capture failures affect only the requested owner URL. OPML commit failures may
delay the selected source additions, but must never partially enable them.

## Immediate containment

1. Stop submitting new captures/import commits if multiple records show the
   same database, object-storage, SSRF-policy, or parser failure.
2. Keep the source polling fuse off for every new owner source.
3. Preserve the original capture ID, OPML preview ID, request ID, and River job
   ID. Do not log the OPML body, article body, annotations, or credentials.
4. If the failure is external-policy related, leave the source disabled and do
   not bypass robots, content-type, size, redirect, or address validation.

## Exact verification commands

List bounded discarded work without printing job arguments:

```sh
docker compose run --rm worker dead-letters --limit 50
```

Inspect only capture metadata and owner-source safety state:

```sh
docker compose exec -T postgres sh -lc 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "
select id, state, error_code, source_id, endpoint_id, item_id, cluster_id,
       created_at, started_at, completed_at
from app.manual_captures
order by created_at desc
limit 20;

select id, origin, validation_state, enabled, reviewed_at
from app.sources
where origin = '\''owner'\''
order by reviewed_at desc nulls last
limit 50;"'
```

Review worker/API logs by capture or request ID. Classify the failure as
configuration, transient dependency, source policy, unsupported connector,
invalid content, parser drift, or code defect before retrying.

## Recovery

- For an eligible cancelled/discarded capture after its root cause is fixed,
  use the audited `retry-job` command from the queue recovery runbook. Verify
  the canonical URL and downstream idempotency before retrying.
- For a terminal policy/content failure, leave it failed. A changed owner URL
  is a new capture with a new idempotency key.
- For an expired, committed, or rejected OPML preview, create a fresh preview
  from the original local document and explicitly reselect valid candidates.
  Never edit the stored candidate JSON or `committed_at`.
- A generic connector that fails bounded fetch/parse validation remains
  disabled until a fixture and reviewed parser change pass normal gates.

## Data-integrity checks

- A successful capture has one item and story cluster, one terminal capture
  record, content-addressed raw/normalized objects, and no duplicate River job.
- Extraction/research publication follows source-tier evidence rules; a T2
  owner capture cannot become the sole support for a factual release claim.
- Every OPML-created source remains `origin = 'owner'`, `validation_state =
  'pending'`, and `enabled = false` until the later owner validation workflow.
- Preview replay returns conflict, duplicate/invalid candidates cannot commit,
  and exports contain only owner-authorized records.
- API/worker error rate and queue age return to baseline before intake resumes.

## Communication

Record capture/preview identifiers, failure class, whether polling stayed off,
owner-visible impact, and recovery time. Do not include source or OPML bodies.

## Post-incident evidence

Retain bounded logs, job and request IDs, idempotency results, source safety
state, export verification, release SHA, and the regression test.
