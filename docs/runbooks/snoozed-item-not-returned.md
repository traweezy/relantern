# Snoozed item not returned runbook

Use this runbook when a story remains hidden after its expected local return
time or appears more than once.

## Trigger

- A story remains snoozed after its expected owner-local return time.
- A returned story appears more than once or returns to the wrong collection.

## Impact

One or more owner stories may be hidden, duplicated, or misplaced. Tags,
annotations, progress, and star state must remain unchanged.

## Immediate containment

- Stop manual state edits and preserve story, owner, request, mutation, and job
  identifiers.
- Pause only the snooze-return job if repeated processing changes state more
  than once.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m worker api
make test-integration
```

- Record owner, story, expected local time, owner timezone, and request ID.
- Compare `snoozed_until` in UTC with database time. Verify
  `snoozed_from_location` is Inbox or Later and equals the current location.
- Read the state version and recent mutation metadata. Do not log annotations,
  raw content, or state snapshots.
- Check worker/database readiness, the `return_snoozed_items` River job, and
  maintenance-queue latency. The periodic job runs once per minute and on
  worker start; no delivery provider is involved.
- Confirm a successful return has one `return_snoozed` row in
  `app.item_state_mutations` and one `reading_state.snooze_returned` row in
  `app.outbox_events`. Inspect identifiers and timestamps only; do not copy
  state snapshots or annotation content into tickets.

## Recovery

- Retry the failed River job through the documented dead-letter operation. A
  due row should atomically clear both snooze columns, become unread, advance
  its version, and write its mutation plus outbox event.
- If the return time is still future, verify the owner timezone and leave the
  row unchanged unless the owner chooses Unsnooze.
- If constraints are inconsistent, stop writes for the affected owner and ship
  a reviewed forward repair migration. Do not patch the row ad hoc.

## Data-integrity checks

- The story appears in exactly its recorded Inbox or Later location.
- It is unread, has no snooze fields, and has one monotonic version advance.
- Repeated jobs and reads do not advance the version again, duplicate the
  outbox event, or duplicate the story.
- Star, tags, annotations, and progress remain unchanged.

## Communication

Record expected and actual local return times, owner-visible impact, affected
count, containment, and recovery. Do not include story or annotation content.

## Post-incident evidence

Retain bounded identifiers, UTC/local-time comparison, mutation/outbox counts,
job state, sanitized logs, release SHA, and the regression test.
