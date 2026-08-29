# ADR-007: Bounded OpenAI pipeline

Status: Accepted
Date: 2026-08-29

## Context

Models improve extraction and synthesis but cannot own coverage, spending,
evidence, scheduling, or external side effects. Fast extraction must tolerate
untrusted document instructions, provider failures, schema drift, concurrent
workers, retries, and source revisions without publishing unsupported facts or
exceeding the owner's budget.

## Decision

### Fast extraction boundary

- Use `github.com/openai/openai-go/v3` 3.52.0 and the
  [Responses API](https://developers.openai.com/api/reference/cli/resources/responses/methods/create).
- Send one permanent developer instruction followed by JSON under the explicit
  `UNTRUSTED_EVIDENCE` marker. The evidence is always last.
- Use strict [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
  with schema `relantern_structured_extraction`, `store=false`, no tools,
  parallel tool calls disabled, truncation disabled, and a stable prompt-cache
  key.
- Pin the fast role to `gpt-5.6-luna`, low reasoning, low verbosity, and 4,096
  output tokens. The model, prompt, schema, and limits are revalidated against
  their database registry rows before every run.
- Reject invalid output and retry once. A second schema failure is terminal for
  the job and puts the item in `needs_review`.

Normalized input is capped at 100,000 bytes. Deterministic code creates at most
200 UTF-8-safe evidence spans of at most 1,600 bytes. Output is capped at 50
claims. Every claim must reference one through eight supplied span IDs. Date,
enum, identifier, duplicate, array, and string constraints are rechecked by Go
after provider output parsing.

### Cost and concurrency boundary

Before every provider request, the worker locks the AI run and a monthly
PostgreSQL advisory-lock namespace. PostgreSQL `numeric` arithmetic reserves
the configured worst-case input and output cost before the call. No money is
converted to binary floating point. The reservation is replaced with actual
input, cached-input, and output token cost after the response. Interrupted
reservations remain charged conservatively.

The independent default budget is USD 25.00 soft and USD 50.00 hard per month.
Crossing soft sets an observable attempt flag. A call that would cross hard is
not sent and the run becomes `budget_blocked`. Retrying a job never bypasses the
same check.

### Provider isolation and work scheduling

Local and test environments accept only `fake-openai` or loopback HTTP and use
a derived non-secret fake credential. Hosted environments accept only
`https://api.openai.com`; enabling extraction there requires an explicit API
key or secret file. The default local Compose stack never calls live OpenAI.

River runs `extract_item` on `ai_fast` with five bounded attempts, uniqueness by
item and revision, exponential retry for provider failures, and a worker timeout
of at most ten minutes. Integrity, target, registry, schema, and hard-budget
errors cancel instead of retrying. Item/revision/model/prompt identity makes
completed work idempotent, and completion becomes `obsolete` if the source
revision changed during the provider call.

PR 9 intentionally did not add web search, research/deep roles, background
responses, or webhooks. PR 10 adds the separately bounded research authority in
[ADR-017](017-bounded-research-background-webhooks.md); it does not expand the
fast extraction tool boundary.

### Evaluation and service goals

`make test-extraction` is a zero-network, budget-free promotion gate over
reviewed fixtures. It enforces the specification's schema-validity, material
evidence coverage, unsupported-material-claim, priority-topic, critical
security, classification, and prompt-injection thresholds. A model, prompt, or
schema change requires updating its semantic version and fixture evidence; a
digest cannot be silently rewritten in place.

Initial operating goals are: no provider call beyond the hard cap, 100 percent
of persisted material claims with evidence, no duplicate completed run for one
identity, and extraction completion within the configured ten-minute job
timeout. The AI run and attempt ledgers expose duration, retry, token, cached
token, cost, error, and budget-alert state for later metrics and alerts.

## Consequences

ChatGPT Pro or Codex entitlements are not treated as API credit. Deterministic
ingestion continues when OpenAI is unavailable or the budget is exhausted.
Provider output is never trusted merely because the API accepted its schema.
Worst-case reservation can temporarily reduce available budget, but makes
concurrent enforcement fail closed and recover conservatively after crashes.
