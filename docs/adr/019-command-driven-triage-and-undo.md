# ADR-019: Command-driven triage, Undo, and bulk mutations

Status: Accepted
Date: 2026-08-29

## Context

Optimistic triage must feel immediate while remaining correct across retries,
multiple tabs, bulk actions, and accidental destructive commands. A client-only
Undo stack cannot recover after navigation and cannot prove which server state
it is reversing.

## Decision

- Every reading-state write is an explicit typed command with an owner-scoped
  idempotency key and expected state version.
- The server stores immutable before/after snapshots and a ten-second deadline
  in `app.item_state_mutations`. Semantic Undo restores the prior state and
  tags while advancing the version monotonically.
- Feedback created by Dismiss or Already Known links to its mutation and is
  removed when that mutation is undone.
- Bulk mutation and Later reordering accept an exact confirmed count, reject
  duplicate stories, and commit all rows or none in a serializable transaction.
- Bulk Undo addresses the server `bulk_id`, locks rows in deterministic item
  order, rejects partial/newer conflicts, and restores all members or none.
- The web predicts deterministic transitions, reconciles the server response,
  and restores its local snapshot on failure. High-volume or destructive bulk
  changes show their exact count before commit.
- Keyboard triage, range selection, visible selection, manual Later ordering,
  command navigation, and an accessible shortcut reference use the same server
  commands as pointer interactions.
- Relevance feedback uses a separate idempotent endpoint and record type.
  Reading actions never implicitly become ranking feedback except the explicit
  Already Known and Dismiss semantics defined by the product contract.

## Operational boundaries

- Mutation snapshots retain full before/after state for 30 days by product
  policy. A future retention job may compact older records only after Undo is
  impossible and audit requirements are preserved.
- Operators do not edit state or mutation rows directly. Use the recovery
  runbook to inspect conflicts and apply a new owner-authorized command.
- Metrics should track command count, conflicts, Undo outcomes, duration, and
  bulk sizes without recording notes, highlights, raw URLs, or state payloads.

## Rollback

Disable mutation entry points while preserving the mutation log. Do not run a
down migration in production. Existing state remains readable, and a corrected
forward release can safely resume command processing.

## Consequences

Undo is durable across navigation and retry, but it deliberately rejects a
mutation after a newer write rather than overwriting owner intent. Versions do
not rewind even when semantic state does.
