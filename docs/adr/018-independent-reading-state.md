# ADR-018: Independent reading and relevance state

Status: Accepted
Date: 2026-08-29

## Context

Relantern needs a high-volume reading workflow without collapsing several
different owner decisions into one overloaded saved flag. A story can be read,
important, deferred, annotated, and no longer active at the same time.
Relevance feedback is an input to ranking; reading state is not.

## Decision

- Every published story has exactly one location: `inbox`, `later`, or
  `archive`. Missing owner state projects as an unread Inbox default.
- Read state, star, snooze, progress, tags, notes, and highlights are
  orthogonal to location.
- Archive clears active snooze and Later ordering but preserves read state,
  stars, progress, tags, notes, and highlights.
- Snooze is valid only from Inbox or Later. It retains that location, hides the
  story from active views, marks it unread, and restores it once when due. A
  bounded `ReturnSnoozedItems` River job performs the return with an item plus
  snooze-timestamp idempotency key, mutation record, and outbox event.
- Dismiss archives with an optional bounded reason. Already Known marks read
  and records novelty feedback without changing source trust.
- Notes and highlights bind to the current normalized content revision.
  Highlight creation verifies the Unicode-code-point span and SHA-256 quote
  hash against stored normalized content. A later revision makes the original
  annotation visibly orphaned; the system never silently moves it.
- Today obtains owner states with one bounded ordered batch query. Archived and
  actively snoozed stories are excluded before rendering.

## Reliability, security, and performance

- State writes use serializable transactions, optimistic versions, bounded
  inputs, and owner-scoped identifiers.
- Collection reads are cursor bounded to 100 rows; browser reads request 50.
- The private API and authenticated Next.js BFF remain the only reading-state
  transport. No state or annotation enters the anonymous fixture-only demo.
- SLO: a single state command should complete within 300 ms p95 and a 50-row
  collection within 500 ms p95 on production-like data.

## Rollback

Production migrations are forward-only. Disable the reading-state routes and
UI first if rollback is required; retain tables and mutation history. A later
forward migration may remove data only after an explicit retention and export
decision.

## Consequences

The schema is more explicit than a saved/read flag, but transitions remain
small, testable, and reversible. Ranking can consume explicit feedback without
learning false negatives from ordinary reading or archiving behavior.
