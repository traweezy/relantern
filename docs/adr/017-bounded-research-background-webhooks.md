# ADR-017: Bounded research, background responses, and webhooks

Status: Accepted
Date: 2026-08-29

## Context

High-value stories sometimes need current primary-source context beyond stored
documents. That work is slower and more expensive than structured extraction,
and provider webhook delivery is duplicated, delayed, or missed in normal
operation. Untrusted retrieved pages must not gain authority over tools,
budgets, evidence, or side effects.

## Decision

### Research boundary

- Run the research role with `gpt-5.6-terra`, medium reasoning, low verbosity,
  8,192 output tokens, background storage, and at most four web-search calls.
- Expose only the Responses API web-search tool. Apply reviewed allowed and
  blocked domain filters in the provider request and revalidate every returned
  HTTPS URL in Go.
- Send only evidence-verified claims from current cluster revisions and T0/T1
  sources under the `VALIDATED_FACTS` envelope. Retrieved pages remain
  untrusted context and cannot add shell, code-execution, file, deployment, or
  write authority.
- Require strict versioned JSON output. Every assertion references supplied
  claim IDs; every cited URL exactly matches the provider's complete returned
  source list. Invalid output receives one new-response retry and then moves to
  `needs_review`.

The research run identity includes the validated-input digest. A changed claim
set creates new work rather than reusing a completed brief. Completion locks
and rechecks the current cluster, primary item, revision, and claim digest; a
changed target becomes `obsolete` without publishing output.

### Cost and retry boundary

Before a provider call, PostgreSQL serializes the monthly cost ledger and UTC
daily web-search ledger with advisory locks. The reservation includes estimated
input, maximum output, and all allowed search calls using fixed-point `numeric`
prices. A monthly hard-cap or daily search-cap rejection occurs before network
use. Interrupted attempts retain their full reservation and search allowance.

Actual token and tool usage replace the reservation only after a validated
response. Provider attempts are capped at five. Exhaustion is terminal for
automatic work and requires review; retries never erase the ledger.

### Webhook and reconciliation boundary

Only `web` is public. `POST /api/webhooks/openai` reads at most 16 KiB and uses
OpenAI JavaScript SDK 7.5.0 to verify the signature against the untouched raw
body before inspecting the event. It accepts only terminal Response events and
forwards a minimal envelope to the private API over an authenticated
server-to-server request.

The private API validates and persists the event before transactionally
enqueuing `poll_openai_background`. `webhook_id` and provider event ID are
unique; exact duplicates are successful no-ops and identifier collisions fail
closed. The worker never trusts webhook content as the model result. It fetches
the stored Response by response ID with the Go SDK, validates it, and commits
brief, assertion, claim, source, usage, and cost records atomically.

A maintenance job scans running background responses every 15 minutes and
enqueues missing polls. Poll uniqueness is based on response ID, so webhook and
reconciliation delivery converge on the same idempotent state transition.

### Service goals

- No provider call after either hard limit rejects its reservation.
- Duplicate terminal webhooks create one durable event and one effective state
  transition.
- A missed webhook is discovered within 15 minutes plus queue delay.
- Every persisted material assertion retains a claim or returned-source link;
  unsupported references fail the transaction.
- Local and test environments remain connected only to `fake-openai`.

## Consequences

Background responses may remain visible as running until webhook delivery or
the next reconciliation tick. Conservative crash accounting can temporarily
consume more allowance than final provider usage, but cannot overspend the
configured boundary. Webhook success acknowledges durable acceptance, not
research completion. Operations use the AI budget and webhook reconciliation
runbooks rather than deleting runs, attempts, events, or River jobs.
