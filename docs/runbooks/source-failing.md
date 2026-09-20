# Source failing runbook

## Trigger

- Operations reports a source error budget as `degraded` or `exhausted`.
- A reviewed endpoint has repeated bounded fetch failures or stale success.
- Owner validation fails before an imported source can be approved.

## Impact

Freshness and source coverage can fall below target. Stored documents,
revisions, evidence, and previously published stories remain intact. Muting a
source changes ranking/digest behavior; pausing it changes polling behavior.

## Immediate containment

1. Open `/sources`, filter to the source, and record its source ID, endpoint,
   last success, error code, and error-budget state.
2. Pause polling with an incident-linked reason. Do not mute it as a substitute
   for containment and do not enable a pending imported source.
3. If several sources fail together, keep all live-provider fuses closed and
   investigate shared DNS, egress, credential, or publisher availability.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api
make sources-verify
make test-integration
```

Use the private `/ops` queue and source panels to compare attempt/failure
counts. `make sources-verify` is deterministic and zero-network; it proves the
committed registry and fixture contract, not publisher availability.

## Recovery

1. Correct the endpoint configuration or connector defect through normal code
   review and deterministic fixtures.
2. Run the source Test action. Approval requires a recent passing validation.
3. Resume a previously active built-in source only after the cause is fixed.
   Resume re-arms endpoints stopped by a permanent fetch error; the next
   reconciliation queues their first retry. Approval of an imported source
   does not activate external polling.
4. Confirm `ALLOW_LIVE_EXTERNAL_APIS=true` only in the environment approved
   for live read-only polling. The reviewed registry must also be enabled;
   either fuse being off keeps built-in sources dormant.
5. Let durable checkpoints continue from their last committed cursor; do not
   delete checkpoints, raw documents, or revisions.
6. Check the worker's separate `pending source intelligence reconciled` and
   `due source polling reconciled` job counts. The former runs with a bounded
   100-job batch whenever an embedding, fast-extraction, or research worker is
   enabled, including when `ALLOW_LIVE_EXTERNAL_APIS=false`. It can restore
   downstream work from stored current revisions without fetching a source.
   New polls and replay of raw documents marked with `source_handoff_failed`
   still require the live-source fuse and an active, unpaused source.

## Data-integrity checks

- Confirm no source ID or endpoint identity changed unintentionally.
- Confirm revision history and raw object keys remain present.
- For feeds and structured APIs, confirm each child raw document retains its
  parent fetch ID and stable source/external ID. A replay should reuse the
  child's object key and create a revision only when its entry payload changes.
- Confirm resumed polling does not create duplicate canonical items.
- Confirm the `reconcile_sources`, `poll_source_endpoint`, and
  `parse_raw_document` River jobs drain in order. A stored fetch and its parse
  job are committed in the same database transaction.
- Confirm a successful leaf parse either queues eligible current-primary
  `reembed_entity`/`extract_item` jobs or leaves the stored revision for later
  capability-aware reconciliation. Raw parse-marker clearance and handoff
  enqueue commit together; a failed handoff leaves `source_handoff_failed` for
  replay. An older or nonprimary revision must not enqueue current-story work.
- If a new primary revision has T2/T3 trust, confirm the item moves to
  `needs_review` and no `extract_item` job is queued for that revision. Restoring
  T0/T1 eligibility requires a reviewed source correction or later eligible
  revision, not an operator-forced extraction job.
- Check extraction and research only after the corresponding workers are
  enabled. Research requires verified T0/T1 claims on the current story and
  admission to an enabled owner's bounded, high-value digest selection before
  its cutoff. Already queued work may start through the ten-minute completion
  grace. A changed verified claim set from a supporting source can start new
  synthesis even if the primary revision did not change; a replay with
  identical claim inputs must not create another effective run.
  Cancelled or discarded jobs require audited operator review and retry under
  the queue recovery runbook; reconciliation does not retry them.
- Confirm audit events record pause, validation, review, and resume actors and
  reasons.

## Communication

Record the affected source, freshness window, incident/request ID, containment,
owner-visible impact, and recovery time. Do not include source payloads,
credentials, or owner profile data.

## Post-incident evidence

Retain the passing validation ID, relevant error-budget window, recovery
timestamp, queue drain evidence, and regression-test commit. Update fixtures or
the registry review date when the publisher contract changed.
