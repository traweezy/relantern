# ADR-007: Bounded OpenAI pipeline

Status: Accepted
Date: 2026-08-29

## Context

Models improve extraction and synthesis but cannot own coverage, spending,
evidence, scheduling, or external side effects.

## Decision

Use the Responses API with strict structured outputs and evidence validation.
Normal AI research and synthesis runs once daily before the digest. A bounded
critical-security lane is separate. The API project has an independent USD 25
soft alert and USD 50 hard stop.

## Consequences

ChatGPT Pro or Codex entitlements are not treated as API credit. Deterministic
ingestion continues when OpenAI is unavailable or the budget is exhausted.
