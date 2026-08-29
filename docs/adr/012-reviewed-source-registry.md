# ADR-012: Reviewed source registry and deterministic connector fixtures

Status: Accepted
Date: 2026-08-29

## Context

The product needs measurable Priority 0 coverage without claiming to crawl the
whole web. Built-in URLs, polling cadence, content policy, body limits, source
identity, and parser behavior must remain code-reviewable. A configuration
file is still untrusted input: accepting misspelled fields or an unsafe URL
would silently weaken the ingestion boundary.

Live connector code, DNS resolution, redirects, and response decompression
belong to the separately reviewed PR 4 fetcher. PR 3 must therefore prove the
configuration and fixture contracts without making CI depend on external
networks.

## Decision

- Keep the built-in registry in `sources/registry.yaml` and reject unknown YAML
  fields, multiple documents, malformed durations/dates, duplicate IDs or
  URLs, non-HTTPS URLs, user information, nonstandard ports, fragments,
  unsupported connectors, missing policy metadata, stale reviews, oversized
  response limits, and missing fixture suites.
- Expand 27 official feed/page/API records plus 32 maintainer repository
  watches into 91 concrete endpoints. Every repository watch enables both the
  release and security-advisory paths and stores GitHub's immutable repository
  node ID alongside its current owner and name.
- Treat the `react/react` owner as the canonical successor to the historical
  `facebook/react` watchlist entry after verifying the GitHub API redirect and
  node identity.
- Support Atom, RSS, JSON Feed, page, GitHub releases, GitHub repository
  advisories, package registries, and structured APIs as the complete initial
  connector allowlist.
- Apply one reviewed HTTP scenario profile to every connector. It covers a
  normal document, empty and malformed documents, redirect, 304, 429 with
  `Retry-After`, 500, oversized and compression-ratio inputs, changed revision,
  duplicate story, prompt injection, and character-encoding edge case.
- Generate oversized and compression-bomb inputs from bounded metadata during
  tests; never commit or expand a dangerous payload merely to exercise a body
  limit.
- Make `make sources-verify` deterministic and zero-network. Endpoint probes
  are an explicit review-time audit and will move behind the PR 4 SSRF-safe
  client before they become an automated network check.
- Mirror the validated registry into PostgreSQL in a serializable transaction
  after every successful forward-migration run. This keeps local, staging, and
  production built-ins aligned without allowing owner/demo seed data outside
  local and test. Use deterministic `registry_id` keys for endpoint upserts.
  While the top-level network fuse is false, sources remain `paused` and every
  `next_poll_at` remains null.
- Require re-review at least every 90 days. CI fails when committed review
  dates age beyond that boundary.

## Endpoint verification

On 2026-08-29, every GitHub repository identity was read through the GitHub
REST API, all 64 derived release/advisory endpoints returned HTTP 200, and all
27 official endpoints resolved successfully with a bounded HTTPS GET. The
registry stores canonical final URLs for renamed paths and keeps a publisher's
stable redirector when its destination is version-specific.

Primary review references:

- [GitHub repository REST API](https://docs.github.com/en/rest/repos/repos)
- [GitHub releases REST API](https://docs.github.com/en/rest/releases/releases)
- [GitHub repository security advisories REST API](https://docs.github.com/en/rest/security-advisories/repository-advisories)
- [Go Blog Atom feed](https://go.dev/blog/feed.atom)
- [GitHub Changelog feed](https://github.blog/changelog/feed/)
- [PostgreSQL news feed](https://www.postgresql.org/news.rss)
- [Railway incident-management API endpoints](https://docs.railway.com/platform/incident-management)
- [OpenAI developer changelog](https://developers.openai.com/api/docs/changelog)

## Consequences

Coverage and review age are measurable, fixture gaps fail before runtime, and
the database contains no independently drifting built-in configuration. CI
remains fast and reproducible because it never probes publishers. This PR does
not fetch, parse, schedule, or publish live content; those capabilities remain
closed behind the registry fuse and later security review.
