# ADR-026: Public source, private application data

Status: Accepted
Date: 2026-09-06

## Context

The owner requested public source access so employers can evaluate Relantern.
The original private-repository decision in ADR-001 conflated source visibility
with application privacy. The source review found no live credential in the
reviewed reachable history, current source, retained Actions logs, or artifacts.

## Decision

Publish `traweezy/relantern` for source inspection, retaining all rights rather
than introducing a permissive open-source license. This supersedes older
private-repository wording only. Historical documents describe their original
decisions; checked-in schedules and budget limits are configuration defaults,
not credentials or a record of actual reading activity or spending.

Keep owner-only authentication, private application state and operations,
provider credentials, staged release gates, and fixture-only demo boundaries.
Exclude environment secrets and private-key files from Docker build contexts.
Enable the available public-repository security checks and use feature pull
requests into `staging`; production promotion still requires its own evidence.

## Consequences

Employers can inspect architecture, tests, and history and try the isolated
[public demo](https://web-demo-ce4e.up.railway.app/demo). Repository publication
also exposes retained GitHub Actions history; reviewed scan detections were
test values, schema hashes, image tags, and module checksums rather than live
credentials. This is an evidence-backed review, not a guarantee that automated
scanning detects every possible disclosure. No history rewrite is required by
the credential review, and no private service is enabled by this decision.
