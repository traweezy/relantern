# Personal Developer Intelligence Platform

## End-to-end product, architecture, implementation, deployment, and operating specification

Document status: implementation-ready specification
Baseline date: 2026-08-29
Revision: 3 — public employer demo, Android follow-up, and final cross-platform audit
Audience: owner, implementation agent, reviewer, and operator
Working product name: Developer Intelligence
Repository placeholder: developer-intelligence
Primary timezone: America/New_York
Production cloud: Railway
Source control: GitHub

---

## 1. Executive decision

Build a private, single-owner developer-intelligence platform that continuously monitors an explicit, audited registry of software sources and turns new material into short, personalized, evidence-backed briefs.

The system must answer:

- What changed?
- Is it stable, experimental, deprecated, or security-sensitive?
- Why does it matter to this owner and the technologies they actually use?
- What concrete example best demonstrates the change?
- Does it replace or improve something in the current stack?
- Should the owner ignore it, watch it, try it, adopt it, or upgrade immediately?
- What are the risks, migration costs, compatibility requirements, and primary sources?

It must cover both newly published stable changes and genuinely upcoming work:

- Proposals and RFCs.
- Experimental features.
- Alpha, beta, preview, and release-candidate builds.
- Announced release dates and support-window changes.
- Deprecation deadlines and end-of-life dates.

An upcoming date must come from a cited maintainer or vendor source. The model must never invent a release date or convert a roadmap aspiration into a commitment.

The platform must not claim to scan the entire internet. No product can prove that. It must instead provide:

- Measured coverage of an explicit source registry.
- Source-health and last-successful-poll evidence.
- First-seen and last-verified timestamps.
- Deterministic ingestion before AI processing.
- Primary-source preference.
- Separate community-signal and official-evidence lanes.
- Citation and claim verification.
- A feedback loop that improves relevance without creating a filter bubble.

The authenticated product is for one owner. Version 1 also includes a deliberately isolated public employer-demonstration route backed only by sanitized fixtures. It is not a public news publisher, social network, autonomous package manager, code-execution service, or generalized web crawler.

---

## 2. Product outcome

The desired daily experience is:

1. The worker continuously performs inexpensive source polling, parsing, deduplication, and precomputation throughout the day.
2. A database-backed schedule prepares and delivers the private morning digest at the owner's configured local time.
3. The owner opens a polished Today screen or reads the same ranked morning digest in Discord or email.
4. The system presents only the highest-value changes since the previous successful digest.
5. Each card has a one-sentence change summary, a personalized reason to care, examples, a recommended action, risk, effort, and primary links.
6. Related posts and release notes are clustered into one story instead of repeated.
7. Security advisories and breaking changes affecting watched technology bypass the normal digest queue and receive an urgent alert.
8. A Technology Radar tracks libraries, frameworks, tools, and practices across Adopt, Trial, Assess, Hold, and Reject.
9. The owner can mark an item read or unread, move it to Read Later, star it, archive it, snooze it, tag it, annotate it, dismiss it with a reason, or undo an accidental action.
10. The system uses explicit relevance feedback to adjust bounded topic and source weights without confusing reading state with ranking feedback.
11. Every summary remains traceable to stored evidence, model version, prompt version, schema version, and source revision.
12. A public /demo route shows the product's real interaction and visual quality to potential employers without creating a demo account or exposing authenticated data.
13. After Version 1 is stable, a native Android client uses the same account, backend, stories, state, schedules, and notification records on both phone and tablet.

Success is not measured by article volume. Success means the owner learns important changes earlier, spends less time scanning repetitive sources, and can act with more confidence.

---

## 3. Owner interest profile

The seed interest profile must be editable from the UI and versioned in the database.

### 3.1 Priority 0: always monitor

- Go language, toolchain, runtime, standard library, modules, profiling, testing, and major ecosystem libraries.
- React, React Server Components, rendering, state, forms, tables, virtualization, animation, accessibility, and performance.
- TypeScript, Next.js, Node.js LTS, browser APIs, frontend build tools, and package management.
- OpenAI APIs, models, Responses API, agents, structured outputs, web search, embeddings, evals, and safety guidance.
- PostgreSQL, pgx, sqlc, query design, migrations, full-text search, vector search, backups, and observability.
- Docker, Docker Compose, BuildKit, image security, local development, and supply-chain controls.
- GitHub Actions, repository security, release automation, provenance, SBOMs, and Dependabot.
- Railway platform capabilities, deployment behavior, networking, variables, databases, storage, backups, and limits.
- Software security, dependency compromise, authentication, secrets, SSRF, prompt injection, and secure agent architecture.
- Real-time browser delivery including SSE, fetch streaming, WebSockets, and event replay.

### 3.2 Priority 1: monitor for stack opportunities

- TanStack Query, Table, Virtual, and Form.
- shadcn/ui, Radix UI, Tailwind CSS, Zustand, Motion, Biome, pnpm, Vite, Vitest, Playwright, and Storybook.
- Kubernetes, OpenTelemetry, Grafana, Valkey, Redis, NATS, Kafka, Temporal, GraphQL, and API-contract tooling.
- Rust language and ecosystem, especially where it affects JavaScript tooling, systems utilities, or Go interoperability.
- Go libraries for HTTP APIs, background jobs, parsing, scraping, observability, security, and data access.

### 3.3 Priority 2: watchlist

- Java and JVM platform changes.
- Deno and Bun.
- Browser standards, web platform proposals, MDN, Mozilla, WebKit, and Chrome platform changes.
- Cloudflare platform changes.
- Major Linux tooling and developer-experience improvements.
- Emerging databases, runtimes, deployment systems, and observability tools.

### 3.4 Negative preferences and explicit constraints

- MUI is prohibited.
- Prefer TypeScript over untyped JavaScript.
- Use Biome; do not introduce ESLint or Prettier.
- Use shadcn/ui generated components on Radix primitives.
- Use TanStack tools when they are the best stable owner for the capability.
- Use Zustand only for transient client/workspace state, not server data.
- Prefer the newest stable, secure, compatible release set. Do not float dependencies to an unpinned latest tag.
- Do not use an alpha, beta, release candidate, canary, or nightly build in production merely because it is newer.
- Do not automatically install, update, or execute software based on an AI recommendation.

---

## 4. Scope

### 4.1 Version 1 includes

- Private GitHub OAuth login with an explicit owner allowlist.
- Configurable interest profile.
- Curated source registry.
- RSS, Atom, JSON Feed, page, GitHub Release, GitHub Security Advisory, registry, and structured-API connectors.
- Conditional fetching, source checkpoints, retry, backoff, and source-health monitoring.
- Article extraction and source revision storage.
- Exact and semantic duplicate detection.
- Story clustering across multiple sources.
- OpenAI structured extraction and personalized synthesis.
- Evidence spans and citations.
- Hybrid keyword and semantic search.
- Today, Inbox, Live, Read Later, Starred, Archive, story detail, release tracker, technology radar, sources, settings, cost, and operations screens.
- Read/unread, Read Later, star, archive, snooze, tags, notes, highlights, bulk actions, undo, and keyboard shortcuts.
- Timezone-aware morning and weekly schedules with preview, Run now, Skip next, pause, catch-up, and delivery history controls.
- Daily private Discord digest.
- Optional Resend email digest.
- Critical security alerts.
- Local development with Make and Docker Compose.
- GitHub Actions CI and security scanning.
- Railway staging and production.
- Metrics, logs, traces, audit records, restore tests, and operating runbooks.
- Public /demo route with a guided employer experience, immutable sanitized data, responsive layouts, accessibility, and strict isolation from authenticated services.

### 4.2 Version 1 excludes

- A public multi-tenant SaaS product.
- Billing and subscriptions.
- A native mobile application.
- Anonymous access to any live owner data, API operation, database record, delivery endpoint, or operations record; /demo is fixture-only.
- Direct publishing of complete copyrighted articles.
- Paywall bypass.
- Browser automation for sources that disallow automated retrieval.
- Arbitrary code execution.
- Autonomous dependency upgrades.
- Automatic pull requests based only on model output.
- Automatic decisions that can modify production repositories.
- A general-purpose search engine.
- A Kafka, Elasticsearch, Redis, Temporal, or Kubernetes dependency without measured need.
- Full private-repository analysis; this is a separately gated follow-up.

### 4.3 Post-Version 1 roadmap

- A committed native Android application track defined in Section 39. It uses the same backend and data, ships from this GitHub monorepo, and produces installable phone/tablet APKs.
- Read-only analysis of the owner's repositories, dependency manifests, SBOMs, and runtime versions.
- Personalized migration checklists tied to real repositories.
- Draft dependency-upgrade pull requests requiring owner review.
- A browser extension and operating-system share-to-inbox workflow. Version 1 still supports secure manual URL capture.
- PWA offline reading.
- An iOS build from the same Expo application only after the Android track passes and the owner explicitly approves the additional signing, testing, and distribution scope.
- Calendar-aware or project-aware digest prioritization.
- Additional notification channels.
- Team workspaces and role-based access.

---

## 5. Non-negotiable product principles

### 5.1 Evidence before prose

Every material claim must link to one or more sources. Stable release claims, security claims, deprecations, breaking changes, and migration instructions require an official source or maintainer-controlled repository.

### 5.2 Deterministic completeness, model-assisted understanding

The model must not decide whether a source was polled, whether a document changed, whether a retry is due, or whether an alert was delivered. Deterministic code owns those operations.

### 5.3 Primary sources first

Official documentation, release notes, security notices, standards, and maintainer repositories are the source of record. Engineering blogs and community discussions add context but cannot replace primary evidence.

### 5.4 Freshness is observable

Every source has a target polling interval, last attempted time, last successful time, latest content time, error budget, and current health state.

### 5.5 Personalization is explainable

The UI must show why an item ranked highly, such as:

- Directly affects React 19.2.
- Security issue in a watched dependency.
- Replaces an existing library.
- Matches the Go and PostgreSQL interest profile.
- High-confidence stable release.

### 5.6 AI is bounded

Source text is untrusted input. It cannot grant the model additional tools, change system instructions, trigger code execution, install a package, publish content, or modify infrastructure.

### 5.7 Reproducible builds

Every deployable web, service, and Android artifact is generated from an immutable commit with exact dependencies, recorded tooling, and verifiable checksums. Local development may be fast; release inputs may not float.

Select the latest stable compatible versions, test them as a set, then pin exact versions, container digests, tool checksums, and GitHub Action commit SHAs.

### 5.8 Demonstration is a security boundary

The employer demo is not an authenticated user, special owner, copied production database, or proxy to live services. It may reuse presentation components, commands, and design tokens only through a demo-specific data adapter backed by reviewed static fixtures. It must remain useful when the API, database, worker, OpenAI, and authentication provider are unavailable.

---

## 6. Intelligence output contract

Every published item must conform to a versioned JSON schema and render the following fields.

| Field | Requirement |
|---|---|
| Headline | Concrete, non-clickbait, maximum 120 characters |
| What changed | One direct sentence |
| Status | proposal, preview, beta, RC, stable, deprecated, withdrawn, security |
| Why you should care | Personalized to the saved stack and interests |
| Highlights | Three to five factual bullets |
| Example | Short source-derived or clearly labeled synthesized example |
| Recommended action | ignore, watch, try, adopt, plan-upgrade, upgrade-now |
| Action rationale | Evidence-backed explanation |
| Effort | tiny, small, medium, large |
| Risk | low, moderate, high, critical |
| Affected technology | Typed identifiers and version ranges |
| Replaces or competes with | Existing stack or candidate tools |
| Migration notes | Compatibility, prerequisites, and rollback |
| Caveats | Unknowns, unstable behavior, licensing, benchmarks, or missing evidence |
| Confidence | high, medium, low with machine-readable reasons |
| Primary sources | Official source URLs |
| Secondary sources | Reputable analysis |
| Community discussion | Clearly separated |
| Dates | published, effective, first-seen, updated, last-verified |
| Provenance | model, prompt, schema, source-revision, response, and run identifiers |

### 6.1 Example product brief

Title: Go 1.27 is available

What changed: Go 1.27 adds generic methods, graduates goroutine leak profiling, and expands standard-library capabilities.

Why you should care: The generic-method change may simplify reusable APIs in the Go services you build, while the goroutine leak profile can improve production debugging without adding a third-party profiler.

Action: Plan-upgrade.

Effort: Small for development tooling; medium if production dependencies reveal compatibility failures.

Concrete next step:

1. Update the toolchain in a branch.
2. Run unit, integration, race, fuzz, benchmark, and static-analysis suites.
3. Compare binary size, startup time, allocations, and p95 latency.
4. Review release notes for changed defaults.
5. Promote only after the production-like Compose rehearsal passes.

This example describes the target product format. The live item must cite the exact release notes and label any synthesized code.

### 6.2 Code-example policy

- Prefer a short official example with a source link when quotation and license policy permit.
- Otherwise synthesize the smallest example that demonstrates one documented behavior.
- Label origin as source-derived, adapted, or synthesized.
- Store language, declared runtime version, source URL, prompt/model provenance, and verification state.
- Version 1 may apply syntax parsing and static analysis with pinned tools.
- Version 1 must not execute downloaded or model-generated code in the application or worker containers.
- Mark examples static-checked or unverified; never imply that static checking proves runtime behavior.
- A future execution sandbox requires a separate threat model, network denial, read-only filesystem, syscall policy, CPU/memory/process/time limits, disposable state, and an independent promotion phase.

### 6.3 Personal reading-state contract

Reading state and relevance feedback are separate concepts.

Every published story starts in Inbox unless it is below the publish threshold, suppressed, or part of an already-seen cluster. The location is exactly one of:

- inbox: new or untriaged material.
- later: an intentional active reading queue.
- archive: retained and searchable but removed from active queues.

Orthogonal state can coexist with any location:

- unread/read.
- starred/unstarred.
- snoozed_until.
- reading progress.
- tags.
- notes and highlights.

Semantics:

- Mark read changes consumption state only. It does not lower topic relevance.
- Read Later moves the item to later and preserves read/star state.
- Star is a durable importance marker, not a synonym for Read Later.
- Archive removes the item from Today, Inbox, and Later but retains it in search, tags, notes, and Starred.
- Snooze hides the item from active queues until the selected local date/time, then restores its prior location and unread state.
- Snooze applies only to Inbox or Later. An archived item must be restored before it can be snoozed.
- Dismiss archives the item and requires an optional reason such as irrelevant topic, duplicate, too promotional, low quality, or already known.
- Already known marks the item read and records novelty feedback; it does not penalize source trust.
- Delete is reserved for owner-created manual captures, notes, or a privacy deletion workflow. Ingested evidence is archived rather than casually deleted.

An item may be both starred and in Read Later. Starred includes starred items from every location, including Archive.

Default read behavior:

- Opening a story for two seconds marks it read.
- The owner can change this to manual-only or mark-on-scroll-complete.
- Every state mutation offers a ten-second Undo action.
- Bulk destructive or high-volume changes show the exact affected count before commit.

### 6.4 Reading and knowledge controls

Every card and story view exposes:

- Mark read/unread.
- Add/remove Read Later.
- Star/unstar.
- Archive.
- Snooze until tonight, tomorrow morning, this weekend, next week, or a custom time.
- Add/remove tags.
- Add a document note.
- Highlight a normalized source span and attach a note.
- Mark useful, already known, irrelevant, too shallow, too verbose, or incorrect.
- Mute source, topic, entity, or exact story cluster.
- Copy private app link.
- Open primary source.
- Export the brief, citations, notes, and highlights as Markdown.

List controls:

- Multi-select with a checkbox or X shortcut.
- Shift-select a range.
- Select all visible.
- Select all matching the current server-side filter only after showing the total.
- Bulk read/unread, Later, star, archive, snooze, tag, and dismiss.
- Undo the last bulk mutation from the server-backed mutation log.

Keyboard defaults:

| Shortcut | Action |
|---|---|
| J / K | Next / previous item |
| Enter or O | Open / close selected story |
| V | Open primary source in a new tab |
| M | Toggle read |
| L | Toggle Read Later |
| S | Toggle star |
| E | Archive and advance |
| Z | Open snooze menu |
| T | Tag |
| N | Add note |
| X | Toggle selection |
| Shift+A | Mark all visible read after confirmation |
| G then T | Today |
| G then I | Inbox |
| G then L | Read Later |
| G then S | Starred |
| G then R | Technology Radar |
| Command/Ctrl+K | Command palette |
| ? | Shortcut reference |

Single-letter shortcuts are disabled while typing, selecting text, using a screen-reader virtual cursor where detectable, or interacting with a form control. Shortcuts are owner-customizable and stored durably.

### 6.5 Manual capture and portability

Version 1 includes:

- Paste URL into Add to Inbox.
- Process the URL through the same SSRF, content-policy, parsing, dedupe, and provenance pipeline as monitored sources.
- OPML source import with preview, duplicate detection, and per-source approval.
- OPML source export.
- JSON and CSV metadata export.
- Markdown export for selected briefs, notes, highlights, and citations.

A browser extension remains a follow-up because it adds a separate permission and security surface. The URL-capture API is designed so a reviewed extension can use it later.

---

## 7. Source strategy

### 7.1 Source trust tiers

| Tier | Definition | Permitted use |
|---|---|---|
| T0 | Official release, security notice, standard, language or vendor documentation | Source of record |
| T1 | Maintainer-controlled repository, changelog, issue, discussion, or package metadata | Source of record when official docs are absent |
| T2 | Reputable engineering publication or expert analysis | Context, examples, implications |
| T3 | Community discussion, Hacker News, Lobsters, Reddit, social post | Discovery and sentiment only |

Rules:

- A stable release, vulnerability, deprecation, or breaking-change claim requires T0 or T1 evidence.
- A T3 source is never the sole evidence for a published factual claim.
- Conflicting sources remain visible. The system must not silently average contradictions.
- A source can be demoted or paused after repeated parsing, quality, or policy failures.

### 7.2 Initial official source registry

Built-in system sources are code-reviewed YAML under sources/registry.yaml and mirrored into the database. A verification command probes every built-in endpoint before merge.

Owner-added sources from manual entry or OPML are stored in the database with origin=owner and state=pending. They do not rewrite registry.yaml. Before activation, the server runs connector detection, URL/SSRF validation, robots and content-policy checks, one bounded fetch, parse validation, duplicate detection, and an owner approval screen. Only generic, already-tested connectors can be selected. A source requiring new parser code remains disabled until it receives a fixture and normal code review.

Seed groups:

- Go: go.dev blog feed, release history, security announcements, proposal repository, and selected official repositories.
- React: official blog, release posts, RFCs, and facebook/react releases.
- Next.js and Vercel: official blog, security posts, changelog, and vercel/next.js releases.
- TypeScript: official development blog, roadmap, release notes, and microsoft/TypeScript releases.
- Node.js: release posts, security releases, LTS schedule, and nodejs/node releases.
- OpenAI: official developer changelog, model catalog, API documentation, cookbook, and openai/openai-go releases.
- PostgreSQL: official news RSS, release notes, security notices, commitfest, and extension releases.
- Docker and Kubernetes: official blogs, release notes, security advisories, Compose and Buildx releases.
- GitHub: changelog RSS, security advisories, Actions releases, and platform documentation updates.
- Railway: official changelog, documentation updates, status, and repository releases when applicable.
- Frontend ecosystem: pnpm, Biome, Tailwind, shadcn/ui, Radix, TanStack, Vite, Vitest, Playwright, Storybook, Zustand, and Motion.
- Rust: official blog, release notes, security response, rust-lang/rust, Cargo, Tokio, and high-value tooling.
- Web platform: MDN updates, web.dev, Chrome for Developers, Mozilla Hacks, and WebKit.
- Security: OSV, GitHub Security Advisories, OpenSSF, NVD only as a corroborating or identifier source, and official vendor advisories.

Known feed examples:

- https://go.dev/blog/feed.atom
- https://devblogs.microsoft.com/typescript/feed/
- https://github.blog/changelog/feed/
- https://blog.cloudflare.com/rss/
- https://kubernetes.io/feed.xml
- https://www.postgresql.org/news.rss

Do not guess feed URLs. Built-in endpoints must pass make sources-verify and a recorded-fixture review. Owner-added generic feeds must pass the equivalent server-side validation and explicit approval; parser changes still require a fixture and pull request.

Initial coverage target:

- At least 60 T0/T1 endpoints across Priority 0 technologies.
- At least two independent discovery paths for every Priority 0 technology when available.
- Every watched runtime or framework has an official release path and a security path.
- Every endpoint is reviewed at least every 90 days.

Default polling:

| Source class | Interval |
|---|---:|
| Official security/advisory feed | 5 minutes |
| Official stable release feed or API | 15 minutes |
| Maintainer repository releases | 15 minutes |
| Official blog or changelog | 30 minutes |
| Normal engineering publication | 60 minutes |
| Community discovery API/feed | 10 minutes |
| Package and dependency metrics | 24 hours |
| New-library discovery | 7 days |

Apply provider rate limits and conditional requests even when a configured interval is shorter. Add jitter so all endpoints do not fire on the same boundary.

### 7.3 Maintainer repository watchlist

Start with releases and security events for:

- golang/go
- facebook/react
- vercel/next.js
- microsoft/TypeScript
- nodejs/node
- pnpm/pnpm
- biomejs/biome
- TanStack/query
- TanStack/table
- TanStack/form
- TanStack/virtual
- shadcn-ui/ui
- radix-ui/primitives
- tailwindlabs/tailwindcss
- vitejs/vite
- vitest-dev/vitest
- microsoft/playwright
- storybookjs/storybook
- pmndrs/zustand
- motiondivision/motion
- postgres/postgres
- pgvector/pgvector
- sqlc-dev/sqlc
- jackc/pgx
- riverqueue/river
- docker/compose
- docker/buildx
- kubernetes/kubernetes
- open-telemetry/opentelemetry-go
- openai/openai-go
- rust-lang/rust
- tokio-rs/tokio

The registry must store repository node ID, owner, name, topic mapping, trust tier, polling cadence, and enabled event types.

### 7.4 Ecosystem metadata sources

- GitHub REST API for releases, tags, repository metadata, and advisories.
- npm registry API for package versions and metadata.
- Go module index for discovery of new module versions.
- deps.dev v3 for package dependencies, licenses, versions, and project mappings.
- OSV API for vulnerability queries and batch checks.
- OpenSSF Scorecard API for individual security-practice signals.
- Hacker News official Firebase API for near-real-time community discovery.
- OpenAI web search for bounded enrichment and weekly source discovery.

The Go module index and npm registry are discovery firehoses. Do not send every version to OpenAI. Apply deterministic topic, maintainer, dependency, popularity, and novelty filters first.

### 7.5 Community sources

Version 1 supports:

- Hacker News official API.
- Lobsters RSS or permitted feed.
- Selected Reddit communities only after API credentials, rate limits, and terms are documented.

Community signals can raise a candidate into review but cannot turn an unsupported claim into fact.

### 7.6 Source registry record

Each source endpoint contains:

~~~yaml
id: go-blog
name: Go Blog
trust_tier: T0
connector: atom
url: https://go.dev/blog/feed.atom
topics:
  - go
poll_interval: 15m
priority: critical
robots_policy: feed
content_license: link-and-excerpt
enabled: true
owner: system
expected_content_types:
  - application/atom+xml
max_response_bytes: 5242880
~~~

Required validation:

- Globally unique ID.
- HTTPS URL.
- Approved connector.
- Explicit trust tier and content policy.
- Poll interval within connector limits.
- Fixture available.
- DNS and SSRF policy passes.
- At least one topic.
- Owner and review date present.

---

## 8. Ingestion and publication pipeline

~~~text
Source registry
  -> due-source scheduler
  -> connector fetch
  -> immutable raw snapshot
  -> parse and normalize
  -> revision and fingerprint
  -> exact duplicate check
  -> semantic candidate match
  -> story clustering
  -> deterministic relevance gate
  -> structured AI extraction
  -> evidence validation
  -> optional deep research
  -> deterministic score
  -> publish
  -> live update and digest selection
  -> feedback and evaluation
~~~

### 8.1 Fetching

Every HTTP connector must:

- Set a descriptive contact-bearing User-Agent.
- Use If-None-Match and If-Modified-Since when available.
- Record ETag, Last-Modified, response status, content type, bytes, duration, and final URL.
- Respect Retry-After.
- Apply exponential backoff with jitter.
- Enforce per-host concurrency and token-bucket rate limits.
- Enforce connect, header, body, and total deadlines.
- Limit redirects to five.
- Revalidate every redirect target against SSRF policy.
- Cap compressed and decompressed sizes.
- Stream to object storage while hashing instead of holding an unbounded body in memory.
- Preserve an immutable content hash and fetch metadata.
- Treat 304 as a successful poll with no new revision.

Default limits:

| Setting | Default |
|---|---:|
| Connect timeout | 5 seconds |
| Response-header timeout | 10 seconds |
| Total request timeout | 30 seconds |
| Feed maximum decompressed body | 5 MiB |
| Article maximum decompressed body | 15 MiB |
| JSON API maximum body | 10 MiB |
| Redirects | 5 |
| Per-host concurrent requests | 2 |
| Global fetch concurrency | 20 |
| Retry attempts | 5 |
| Robots cache | 24 hours |

Source-specific values can be lower. Increases require evidence and review.

### 8.2 Time semantics

Store separate timestamps:

- source_published_at: timestamp asserted by the publisher.
- source_updated_at: update timestamp asserted by the publisher.
- content_effective_at: release or policy effective time when known.
- first_seen_at: first time any connector observed the item.
- first_fetched_at: first successful body retrieval.
- revision_seen_at: observation time for a content revision.
- processed_at: completion of deterministic processing.
- published_at: time the brief became visible.
- last_verified_at: most recent successful evidence verification.

Never replace first_seen_at with a publisher timestamp. Historical replay must not pretend a document was available before it could have been observed.

### 8.3 Raw storage

Raw documents go to private S3-compatible object storage:

~~~text
raw/{source_id}/{yyyy}/{mm}/{dd}/{sha256}.{extension}
~~~

PostgreSQL stores metadata and object keys, not multi-megabyte source bodies.

Store:

- Original bytes where terms permit.
- Sanitized extracted text.
- HTTP headers on an allowlist.
- SHA-256.
- Parser version.
- Extraction warnings.
- Content-policy classification.

Never return raw third-party HTML directly to a browser.

### 8.4 Parsing and normalization

- Parse RSS, Atom, and JSON Feed with gofeed.
- Extract readable page content through a pinned readeck/go-readability v2 adapter.
- Strip scripts, style, forms, hidden controls, comments, SVG scripts, event handlers, and embedded instructions.
- Preserve headings, paragraphs, lists, tables, code blocks, and source-relative anchors.
- Normalize Unicode and line endings.
- Preserve an offset map from normalized spans to source text.
- Detect language.
- Reject empty or navigation-only extraction.
- Keep parser warnings visible in Source Health.

### 8.5 Revision handling

The same URL can change. A new normalized hash creates a content_revision. The system must:

- Preserve every revision used to create a published brief.
- Diff material changes.
- Re-run extraction when a release note changes after publication.
- Mark a brief Updated when conclusions change.
- Record the reason and notify only when the update is material.

### 8.6 Deduplication

Apply in order:

1. Canonicalize URL and remove known tracking parameters.
2. Honor rel=canonical only when the destination remains within an approved registrable domain or explicit alias.
3. Compare exact raw and normalized SHA-256 hashes.
4. Compare normalized title, author, release identifier, package, and version.
5. Use SimHash for near-identical text.
6. Query embedding neighbors for semantic candidates.
7. Apply a deterministic cluster decision using dates, entities, version, source tier, and similarity.
8. Send only ambiguous high-value cases to the classifier.

Multiple reports about the same event become one story cluster. The primary source is T0 or T1; other sources are supporting perspectives.

### 8.7 Failure states

Every item has an explicit state:

- discovered
- fetched
- normalized
- duplicate
- clustered
- awaiting_ai
- extracting
- needs_review
- ready
- published
- suppressed
- failed_retryable
- failed_terminal

No silent drops. A terminal failure requires a reason code and operator visibility.

---

## 9. OpenAI architecture

### 9.1 API and SDK

Use:

- Official OpenAI Go SDK.
- Official OpenAI JavaScript SDK only in the public Next.js webhook verifier.
- Responses API.
- Structured Outputs with strict JSON Schema.
- Web search only in approved research stages.
- Background responses for long synthesis.
- OpenAI webhooks for completion notification.
- Batch API for non-urgent backlog and embeddings when appropriate.
- Prompt caching through a stable prefix.

Do not add a TypeScript or Python AI worker merely to obtain an agent SDK. The Go worker can directly use the Responses API and the tools this product actually needs.

### 9.2 Model roles

The model configuration is data-driven and revalidated at bootstrap.

| Role | Baseline | Use |
|---|---|---|
| Fast | gpt-5.6-luna | classification, entity extraction, bounded relevance, short summaries |
| Research | gpt-5.6-terra | important story synthesis, comparisons, migration implications |
| Deep | gpt-5.6-sol | manually approved or scheduled high-value weekly analysis |
| Embedding | text-embedding-3-small | semantic clustering and search |

Rules:

- Store provider, model ID, reasoning level, verbosity, token limits, and enabled tools in model_configs.
- Exact model IDs are environment variables and database configuration, never scattered through code.
- A model change requires eval comparison, cost review, prompt compatibility, and rollback configuration.
- Use the least expensive model that meets the quality threshold.
- Deep-model use must be rare, budgeted, and observable.

### 9.3 AI stages

Stage A: classification

Input:

- Sanitized title.
- Short source excerpt.
- Source identity and trust tier.
- Interest taxonomy.
- Known technology entities.

Output:

- Topic IDs.
- Event type.
- Lifecycle state.
- Relevance component scores.
- Candidate entity/version links.
- Whether deep extraction is justified.

Stage B: structured fact extraction

Input:

- Sanitized normalized text.
- Document outline.
- Versioned JSON schema.
- No tools.

Output:

- Atomic claims.
- Evidence span identifiers.
- Version values.
- dates.
- breaking/deprecation/security flags.
- migration prerequisites.
- claimed examples.
- extraction uncertainty.

Stage C: evidence validation

Deterministic code verifies:

- Every claim references a valid span.
- The span belongs to an input document revision.
- Version and date formats are valid.
- URLs are from the supplied source set.
- Enums and ranges comply with schema.
- Unsupported claims are rejected or moved to review.

Stage D: research synthesis

Only for high-value stories. The research model can use web search with:

- Approved and blocked domain filters.
- Full returned source list recorded.
- A maximum search-call budget.
- A primary-source-first instruction.
- No shell, code execution, file modification, or external write tool.

Stage E: personalized brief

The model receives:

- Validated fact records.
- Owner interest profile.
- Current stack profile.
- Existing radar decision.
- Desired concise format.

It does not receive arbitrary raw web text when validated facts are sufficient.

### 9.4 Structured-output requirements

Each schema:

- Has a semantic version.
- Uses additionalProperties false.
- Defines every enum.
- Caps array lengths and string lengths.
- Separates claims from commentary.
- Requires evidence_span_ids for factual claims.
- Allows unknown instead of invented values.

Reject and retry once on schema failure. A second failure moves the item to needs_review.

### 9.5 Prompt-injection defense

- Source text is wrapped as untrusted evidence.
- Instructions inside source text are data, never commands.
- Extraction calls have no tools.
- Research calls expose only web search.
- The permanent developer prompt precedes owner configuration and source text.
- Retrieved pages cannot modify allowed domains, token budgets, or tool policy.
- The output is schema-validated and policy-checked.
- URLs are revalidated by server code before display.
- Markdown is rendered through an allowlist sanitizer.

### 9.6 Background work and webhooks

Use background responses for deep research likely to exceed a normal request window.

Webhook receiver:

- Terminates at the public Next.js route /api/webhooks/openai because the Go API is private.
- Reads the raw request body before parsing.
- Verifies the signature with the official OpenAI JavaScript SDK before parsing or forwarding.
- Deduplicates on webhook-id.
- Forwards only the verified event through the authenticated private API and persists it before processing.
- Returns success quickly.
- Fetches the completed response by ID.
- Is safe under duplicate and out-of-order delivery.
- Reconciles pending responses every 15 minutes in case a webhook is missed.

### 9.7 Batch usage

Use Batch for:

- Backfilling newly added sources.
- Re-embedding after an evaluated embedding-model migration.
- Nightly low-priority extraction backlog.
- Periodic reclassification after taxonomy changes.

Do not use Batch for urgent security alerts or daily-digest items near the delivery deadline.

### 9.8 Prompt caching

The stable prefix contains:

- Product rules.
- Output schema.
- Taxonomy.
- Evidence policy.
- Style rules.
- Owner interest profile version.

Dynamic source content goes last. Record cached and uncached input tokens.

### 9.9 Cost controls

Deterministic gates run before any model call:

- Duplicate suppression.
- Topic allowlist.
- Minimum source trust.
- Minimum novelty.
- Maximum document age.
- Maximum token estimate.
- Daily and monthly budget.

Default owner-adjustable budgets:

- Monthly soft alert: 25 USD.
- Monthly hard stop: 50 USD.
- Daily web-search calls: 100.
- Deep-model stories: 3 per day and 10 per week.

At the hard stop:

- Continue deterministic ingestion.
- Mark eligible items awaiting_ai_budget.
- Continue security-advisory matching using deterministic metadata.
- Alert the owner.
- Never silently exceed the cap.

Track:

- Input, cached input, output, reasoning, embedding, tool, and web-search usage.
- Estimated cost using a versioned pricing table.
- Provider-reported cost when available.
- Cost per published brief, topic, source, and model.

Prices must be refreshed from official pricing at bootstrap and reviewed monthly. Do not encode the specification's snapshot prices as permanent truth.

### 9.10 Model-output authority

Model output may:

- Classify.
- Extract.
- Summarize.
- Compare.
- Recommend.
- Populate a review queue.

Model output may not:

- Install or update a dependency.
- Merge a pull request.
- Change infrastructure.
- Add a permanent source without review.
- Execute downloaded code.
- Publish outside private owner channels.
- Increase its budget or tool authority.

---

## 10. Recommendation and technology-radar engine

### 10.1 Radar states

- Adopt: validated and preferred for new work.
- Trial: use in a bounded real project.
- Assess: promising; research and prototype.
- Hold: do not add to new work without an exception.
- Reject: fails a documented requirement or risk threshold.

Every state has:

- Decision date.
- Review date.
- Owner.
- Evidence.
- Incumbent comparison.
- Applicable project types.
- Compatibility requirements.
- Exit conditions.

### 10.2 New-library discovery

Run weekly. Candidate sources:

- GitHub releases and repository search.
- Maintainer announcements.
- npm and Go package metadata.
- Hacker News and approved community sources.
- OpenAI web search with bounded domains.
- Dependency graph signals from deps.dev.

Do not process the entire npm registry or Go module index with AI.

JavaScript-only source policy:

- Version 1 does not run a general Playwright browser inside the ingestion worker.
- Prefer a publisher feed, API, repository release, sitemap, or server-rendered page.
- If a page requires browser execution and automation is permitted, create a separately reviewed connector with strict domain scope and resource limits.
- Until that connector exists, mark the source unsupported or degraded and use bounded web-search discovery as a non-complete fallback.
- Never hide degraded coverage behind a successful source-health state.

### 10.3 Candidate assessment

Assess:

- What problem it solves.
- What current tool it replaces or complements.
- API and architecture fit.
- Stable release status.
- License and license compatibility.
- Maintainer and organization identity.
- Release cadence and recency.
- Contributor concentration and bus-factor warning.
- Issue and security response quality.
- Supported runtimes and browsers.
- Type quality.
- Bundle size using reproducible methods.
- Performance using credible, comparable benchmarks.
- Download trend and dependent-project signal.
- OpenSSF Scorecard checks, not just the aggregate score.
- OSV and GitHub advisories.
- npm provenance or equivalent release provenance.
- Signed tags/releases where available.
- Documentation and migration quality.
- Lock-in and exit cost.

Stars and downloads are discovery signals, not proof of quality.

### 10.4 Comparison output

Every recommendation table compares:

| Dimension | Candidate | Current choice | Evidence |
|---|---|---|---|
| Capability | What it adds | Existing capability | Primary link |
| Stability | Stable/pre-release | Current stability | Release source |
| Maintenance | Activity and concentration | Current project | Repository data |
| Security | Practices and advisories | Current posture | OSV/Scorecard |
| Performance | Reproducible result | Current result | Method |
| Migration | Required changes | No-change baseline | Migration guide |
| Reversibility | Exit path | Current lock-in | Design assessment |

The owner makes the final adoption decision.

---

## 11. Ranking

The final score is calculated by deterministic Go code.

Default 100-point model:

- Interest fit: 0–30.
- Current-stack impact: 0–20.
- Urgency: 0–15.
- Maturity and actionability: 0–10.
- Novelty: 0–10.
- Source confidence: 0–10.
- Community signal: 0–5.

Penalties:

- Duplicate or derivative reporting: 0 to -20.
- Stale information: 0 to -15.
- Unsupported material claims: 0 to -30.
- Pre-release without owner interest: 0 to -10.
- Promotional or benchmark-quality concern: 0 to -15.

The model can propose bounded components and reasons. Go code validates ranges, applies source policy, and computes the score.

Critical override:

- A confirmed security advisory affecting a watched dependency bypasses ranking.
- A release withdrawn by its maintainer bypasses ranking.
- A source-health outage affecting Priority 0 coverage produces an operational alert.

Feedback adjustments:

- Useful: increase topic/source weights by at most 0.02.
- Irrelevant: decrease by at most 0.02.
- Already known: lower novelty weighting, not source trust.
- Too shallow or too verbose: change presentation settings, not topical importance.
- Incorrect: creates an evaluation case and review task.

Global adjustment is capped at 20 percent from the manually configured base. The UI exposes and can reset learned adjustments.

---

## 12. Search

Use PostgreSQL hybrid search in Version 1:

- Generated tsvector over title, summary, entities, and normalized content.
- GIN full-text index.
- pg_trgm for typo-tolerant title and package search.
- pgvector cosine search for semantic retrieval.
- Reciprocal-rank fusion to combine keyword and vector results.
- Filters for topic, source tier, lifecycle state, date, radar state, action, and saved status.

Do not add Elasticsearch or another search service initially.

Measured replacement trigger:

- p95 search latency remains above 300 milliseconds at expected concurrency after query/index tuning; or
- relevance evaluation fails its target and PostgreSQL cannot support the required analyzers; or
- corpus size and write rate create unacceptable database contention.

Embeddings are stored in a model-versioned table. A model migration builds a parallel index, runs retrieval evals, then atomically changes the active model. Never overwrite the only copy.

---

## 13. Live update transport

Use a protocol-neutral LiveTransport interface.

Version 1 implementation:

- Server-Sent Events over fetch, ReadableStream, AbortController, and eventsource-parser.
- Explicit authentication and HTTP-status handling.
- Exponential retry with jitter.
- Resume cursor.
- Visibility recovery after a hidden tab.
- Heartbeat every 15 seconds.
- Event coalescing to avoid excessive React renders.

Backend:

- Durable outbox_events table with monotonic bigint event IDs.
- PostgreSQL LISTEN/NOTIFY wakes stream processes.
- NOTIFY is not the durable payload.
- Clients replay outbox rows after their last cursor.
- Retain replay events for seven days.
- If a cursor is older than retention, send reset_required and refetch current queries.

Headers:

~~~text
Content-Type: text/event-stream
Cache-Control: no-cache, no-transform
Connection: keep-alive
~~~

Do not use native EventSource as the only client because it cannot meet the required status inspection, explicit abort, custom retry, and flexible cursor/auth contract.

Do not add Socket.IO or a WebSocket library in Version 1. This is a measured default, not a permanent ban. Re-evaluate WebSockets if the product adds sustained bidirectional collaboration, client-to-server streaming, or measured SSE connection limits.

---

## 14. System architecture

~~~text
Internet sources
      |
      v
Go worker -> private object storage
      |              |
      v              v
PostgreSQL + pgvector + River
      |
      +-> OpenAI Responses API
      |
      +-> Discord / Resend
      |
      v
Private Go API
      |
      v
Next.js BFF and UI
      |
      +-> authenticated owner browser
      |
      +-> public /demo from static fixtures only

Post-Version 1 Android client
      |
      v
Next.js OAuth/resource BFF -> private Go API
~~~

### 14.1 Railway services

| Service | Exposure | Responsibility |
|---|---|---|
| web | Public | Next.js UI, auth, protected BFF, SSE proxy, isolated static /demo, and post-Version 1 mobile OAuth/resource boundary |
| api | Private | Huma/chi API, search, stories, settings, live stream |
| worker | Private | Polling, parsing, clustering, OpenAI, digests, alerts |
| migrate | Private one-shot | Database migrations |
| postgres | Private | PostgreSQL 18 with pgvector |
| bucket | Private credentials | Raw snapshots and exports |

Only web receives public application traffic. The browser and later Android client do not receive database, internal-service, OpenAI, GitHub-ingestion, Discord, Resend, FCM-server, or object-storage credentials.

### 14.2 Backend capability owners

- HTTP router and middleware: chi.
- Typed API and OpenAPI 3.1: Huma v2 with chi adapter.
- PostgreSQL driver: pgx/v5.
- Type-safe queries: sqlc.
- Background jobs: River OSS on PostgreSQL.
- Schedule expression parsing: robfig/cron v3 with embedded Go time/tzdata; durable schedule state remains in PostgreSQL application tables.
- Feed parsing: mmcdole/gofeed.
- Readable article extraction: readeck/go-readability v2 behind an adapter.
- OpenAI: official openai-go SDK.
- Logging: log/slog JSON.
- Traces and metrics: OpenTelemetry.
- IDs: PostgreSQL uuidv7() for persisted entity IDs.

Huma generates OpenAPI 3.1 and 3.0.3 representations. Commit the canonical generated 3.1 document and generate one platform-neutral TypeScript client in packages/api-client for the web and later Expo client. CI fails when handlers, the contract, or generated client drift. Browser and Android transports supply their own authentication and base URL adapters; generated endpoint and schema code remains shared.

### 14.3 Why no additional infrastructure in Version 1

| Technology | Initial decision | Re-evaluation trigger |
|---|---|---|
| Redis/Valkey | Not required | Proven cache or fanout pressure that PostgreSQL cannot meet |
| Kafka/NATS | Not required | Multiple independent consumers need high-throughput durable replay |
| Temporal | Not required | Multi-day workflows become difficult to reason about or recover in River |
| Elasticsearch | Not required | PostgreSQL hybrid search fails measured relevance or latency targets |
| GraphQL | Not required | Multiple clients need ad hoc graph-shaped reads that REST cannot serve cleanly |
| Kubernetes | Not required | Railway service limits, scale, policy, or portability requirements justify it |

These are capability decisions, not ideological bans.

---

## 15. Frontend architecture

### 15.1 Stable baseline

Use the newest security-patched, stable, compatible patch at bootstrap:

| Technology | Baseline on 2026-08-29 |
|---|---|
| Next.js | 16.3.3 security floor |
| React | 19.2 |
| TypeScript | 7 stable |
| Node | 26 Current, exact compatible patch |
| pnpm | 12 stable |
| Biome | 2.5 |
| Tailwind CSS | 4 |
| shadcn/ui CLI | 4 |
| Radix UI | current stable |
| TanStack Query | 5 |
| TanStack Table | 9 |
| TanStack Virtual | 3 |
| TanStack Form | 1 |
| Zustand | 5 |
| Motion | 13.1 |
| Better Auth | 1.7 |

TanStack Form 2 remains excluded while pre-release. Node 26 is the
owner-selected production baseline despite its Current-line status; pin the
exact compatible patch and reassess its status at each runtime upgrade.

### 15.2 State ownership

- React Server Components: authenticated initial reads and static shell.
- TanStack Query: server-owned data that refreshes or mutates in the browser.
- Zustand: ephemeral UI state such as panel layout, filters in progress, and command-palette state.
- URL search parameters: shareable filters and selected story.
- TanStack Form: validated forms.
- PostgreSQL: durable preferences.

Never duplicate server records into Zustand.

### 15.3 Component system

- Generate components with shadcn/ui CLI 4 using the Radix base.
- Start from the compact Rhea geometry.
- Tailwind 4 CSS-variable theming.
- Semantic OKLCH tokens.
- Radix primitives for accessible interaction behavior.
- Lucide icons unless a component requires a different audited icon.
- No MUI package, import, copied component, icon package, theme, or transitive styling dependency.

### 15.4 Visual language

The product should look like a professional research terminal, not a generic AI chatbot.

Themes:

- Night Index: deep neutral background, restrained blue/teal focus and confidence colors.
- Day Index: warm neutral surface, high-contrast ink, controlled accent.

Rules:

- Dense but readable information hierarchy.
- Tabular numerals for versions, costs, scores, and dates.
- Source-tier badges.
- Status colors must also have text/icons.
- No animated gradients.
- No excessive glass effects.
- No gamification.
- No slow entrance sequences.

Motion:

- Micro feedback: 80–120 ms.
- Menus and panels: 140–180 ms.
- Route/content transitions: 180–220 ms.
- Respect prefers-reduced-motion.
- Do not animate live numeric updates when it harms scanning.

### 15.5 Screens

#### Today

- Digest summary.
- Morning briefing header with coverage window, generated time, next scheduled run, and delivery state.
- Critical alerts.
- High-value release cards.
- New-library candidates.
- Items needing review.
- Source coverage status.
- Cost-to-date.
- One-click Preview next digest and Run morning brief now controls for the owner.

#### Public employer demo

- Exact route: /demo, with fixture story deep links at /demo/story/{fixture_id}.
- Immediate guided landing state that explains the problem, the evidence-first approach, and three high-value workflows.
- Demonstration versions of Today, Inbox, Story, Releases, Radar, Sources, and Operations using the real presentation components.
- Visible Demo data and snapshot-date label on every screen.
- Local-only read, Later, star, archive, filter, command-palette, and theme interactions with a Reset demo control.
- Architecture, testing, accessibility, and measurable-outcome panels written for senior engineering reviewers rather than marketing hype.
- Optional case-study and resume links; never link a private GitHub repository as if it were public.
- No login prompt, account creation, delivery, source test, import, external research, or mutation against the real API.

#### Live

- Continuously updated event stream.
- Filters by topic, tier, status, action, and confidence.
- Pause and resume.
- New-items indicator without forced scroll jumps.
- Read, Later, star, archive, snooze, and selection controls without opening the story.

#### Story detail

- Executive brief.
- Timeline.
- Primary source.
- Claim-to-evidence viewer.
- Related source tabs.
- Examples.
- Migration and compatibility.
- Model/run provenance.
- Feedback controls.
- Read state, star, Later, archive, snooze, tags, note, highlights, export, and source-mute controls.
- Reading progress that resumes at the last normalized source paragraph.

#### Releases

- Technology/version matrix.
- Stable, preview, security, and deprecation states.
- Watched current version and newest version.
- Upgrade status.
- Coming Soon lane for official proposals, previews, release candidates, deprecation deadlines, and support-window changes.
- Source-declared target dates with last-verification time.
- Calendar and timeline views without model-generated date predictions.

#### Technology Radar

- Adopt, Trial, Assess, Hold, Reject.
- Candidate comparison.
- Decision history.
- Review due dates.

#### Sources

- Registry.
- Health.
- Last success.
- Expected cadence.
- HTTP status and errors.
- Content count and duplicate rate.
- Pause, test, and review actions.

#### Search

- Hybrid results.
- Exact filters.
- Saved query.
- Search explanation.

#### Inbox

- All published, unarchived, unsnoozed items awaiting triage.
- Unread/all toggle and visible unread count.
- Sort by rank, newest, oldest, urgency, or estimated reading time.
- Group by day, topic, technology, or story cluster.
- Mark all visible read, never an unqualified mark-all database mutation.

#### Read Later

- Explicit active reading queue.
- Manual drag ordering plus oldest/newest/rank sorts.
- Shortlist filter for starred Later items.
- Aging indicator after 14 and 30 days.
- Optional weekly resurfacing; never auto-archive without owner approval.

#### Starred

- Durable high-value references independent of Inbox/Later/Archive.
- Filter and group by tags, technology, action, and date.
- Notes, highlights, Markdown export, and original-source access.

#### Archive

- Searchable history removed from active queues.
- Restore to Inbox or Later.
- Starred archived items remain visible in Starred.
- Dismiss reason and mutation history remain inspectable.

#### Snoozed

- Upcoming return time and prior location.
- Unsnooze now.
- Edit return time.
- Bulk reschedule.

#### Settings

- Interests.
- Current stack.
- Source preferences.
- Digest schedule.
- Weekday/weekend rules, maximum digest length, empty-digest behavior, catch-up window, Skip next, pause, Preview, and Run now.
- Quiet hours.
- Model and cost budgets.
- Data retention.

#### Operations

- Queue depth.
- Stuck jobs.
- OpenAI pending background jobs.
- Delivery attempts.
- Source error budgets.
- Recent deploy metadata.
- Restore status.
- Schedule definitions, next due times, occurrence ledger, missed/caught-up runs, and duplicate-suppression keys.

### 15.6 Layout and interaction contract

Desktop at 1280 pixels and wider:

- Persistent 240-pixel navigation rail.
- 420–520-pixel story list.
- Flexible reading/detail pane.
- Collapse either side pane with saved preference.
- Fixed one-line top bar to prevent layout movement while reading.

Tablet:

- Navigation becomes an overlay rail.
- Two-pane list/detail layout.
- Actions stay in a stable bottom or top command row.

Mobile:

- Single-pane navigation.
- Bottom navigation for Today, Inbox, Later, Starred, and Search.
- Swipe actions are optional and must have visible button equivalents.
- No hover-only function.

Card hierarchy:

1. Status, source tier, technology, and relative time.
2. Headline and What changed.
3. Why you should care.
4. Action, effort, risk, confidence, and estimated reading time.
5. Read/Later/star/archive controls.

Interaction requirements:

- Command/Ctrl+K command palette searches navigation and actions.
- The ? overlay shows current, owner-customized shortcuts.
- Context menus and toolbar actions call the same typed command handlers.
- Optimistic mutations display pending state and reconcile by mutation ID.
- A failed mutation restores prior state and explains the error.
- Undo is available for read, Later, star, archive, snooze, dismiss, tag, and bulk mutations.
- Filters are reflected in URL search parameters.
- List scroll position and selected item survive navigation.
- The reader uses adjustable font size, line height, line width, serif/sans choice, and light/dark/system theme.
- Source text and model commentary are visually distinct.

### 15.7 Accessibility

- WCAG 2.2 AA.
- Keyboard-complete navigation.
- Visible focus.
- Screen-reader labels and live-region discipline.
- Contrast tests for every theme.
- Minimum target sizes.
- Reduced motion.
- Dialog focus restoration.
- No color-only status.
- Accessible data tables with responsive card alternatives.

### 15.8 Performance budgets

At the 75th percentile:

- LCP at or below 2.5 seconds.
- INP at or below 200 milliseconds.
- CLS at or below 0.1.

Additional:

- Initial authenticated JavaScript below 220 KiB gzip for Today.
- Route-specific code splitting.
- Virtualize lists above 200 rendered rows.
- Images require explicit dimensions.
- No client-side syntax highlighter in the initial bundle.
- p95 API reads under 300 milliseconds excluding external research.
- Live update displayed within two seconds of publication.
- Public /demo initial JavaScript below 180 KiB gzip, LCP at or below 2.0 seconds at p75, no blocking third-party scripts, and no request-time database or model dependency.

### 15.9 Public employer-demo implementation contract

Route structure:

- apps/web/src/app/(public)/demo/layout.tsx
- apps/web/src/app/(public)/demo/page.tsx
- apps/web/src/app/(public)/demo/story/[fixture_id]/page.tsx
- apps/web/src/features/demo/demo-adapter.ts
- apps/web/src/features/demo/demo-snapshot.ts
- apps/web/src/features/demo/demo-sanitizer.test.ts

The demo snapshot is a typed, immutable module generated from reviewed fixtures during the build. It contains only public primary-source URLs, synthetic owner preferences, synthetic notes, synthetic operational metrics, and deliberately non-current example timestamps. It contains no database export, real owner identifier, real delivery address, secret, private note, source credential, provider response ID, actual cost ledger, or production hostname not already intended to be public.

Rendering and state:

- The public layout imports the demo adapter directly and never imports the authenticated repository, Better Auth session helper, private BFF client, server action, or live-transport implementation.
- Demo mutations run only in an in-memory store with an optional namespaced sessionStorage snapshot. They never use localStorage keys belonging to the authenticated app.
- Reset demo discards all simulated state and returns to the initial guided step.
- The route is request-rendered so the root layout can apply a unique strict CSP nonce to every executable script. "Static demo" means the data and dependency graph are immutable and fixture-only; it does not mean static HTML.
- The page content and fixture state remain session-independent whether or not the visitor already has an owner session. Per-request nonce bytes are expected to differ.
- Fixture IDs are allowlisted at build time; unknown IDs return notFound.
- The demo does not accept arbitrary URLs, HTML, Markdown, search queries sent to a server, or user-generated content.

Presentation:

- First viewport: one-sentence product value, one representative evidence-backed story card, and Start guided demo.
- Guided steps: scan the morning brief, inspect claim evidence, triage to Later/Starred, then view the technology-radar decision.
- An unguided Explore mode exposes the fixture screens without a tour overlay.
- Keyboard shortcuts, reduced motion, screen-reader announcements, themes, and responsive layouts match the authenticated product.
- Static metadata supplies title, description, canonical demo URL, and a reviewed Open Graph image.
- Set robots to noindex,nofollow so synthetic examples do not appear as current technology claims; employers reach it through the portfolio or direct link.
- A persistent banner says Demonstration — illustrative data and links to the snapshot methodology.

Isolation gates:

- The production build fails if the demo bundle imports any module under authenticated data, auth server, delivery, OpenAI, database, or private operations boundaries.
- Playwright records every request during the anonymous journey and fails on any /api/v1, /api/auth, SSE, database proxy, OpenAI, Discord, Resend, or object-storage request.
- An anonymous request to every non-demo application route redirects to login or returns 401/404 as appropriate.
- Encoded slash, backslash, duplicate slash, dot-segment, case-variant, and query-string path tests cannot turn a demo URL into an authenticated route.
- A content scanner checks the emitted demo HTML, React payloads, JavaScript, source maps when generated, and static assets for configured secret patterns and forbidden owner data.
- Production source maps remain private release artifacts rather than publicly served assets.

---

## 16. Data model

Use PostgreSQL 18.6 or the current stable 18.x security-patched minor at bootstrap. PostgreSQL 19 beta is not a production baseline. Enable:

- pgcrypto when required.
- pg_trgm.
- vector.

Use uuidv7() for externally exposed entity primary keys. Use bigint identity for high-volume ordered event and attempt tables when ordering is valuable.

### 16.1 Identity and configuration

#### users

- id uuid primary key default uuidv7()
- github_user_id bigint unique not null
- login text not null
- display_name text
- timezone text not null
- created_at timestamptz
- updated_at timestamptz

#### interest_profiles

- id uuid
- user_id uuid
- version integer
- name text
- is_active boolean
- created_at timestamptz
- profile_summary text

#### interest_topics

- profile_id uuid
- topic_id text
- priority smallint
- weight numeric
- keywords text[]
- exclusions text[]

#### technology_inventory

- id uuid
- user_id uuid
- technology_id text
- current_version text
- constraint text
- status text
- source text
- last_verified_at timestamptz

### 16.2 Sources

#### sources

- id text primary key
- name text
- trust_tier text
- owner text
- origin text check in system, owner
- validation_state text check in pending, active, degraded, paused, rejected
- homepage_url text
- content_policy text
- enabled boolean
- reviewed_at timestamptz

#### source_endpoints

- id uuid
- source_id text
- connector text
- url text
- poll_interval interval
- priority text
- config jsonb
- next_poll_at timestamptz
- last_attempt_at timestamptz
- last_success_at timestamptz
- health_state text

#### source_checkpoints

- endpoint_id uuid
- cursor text
- etag text
- last_modified text
- provider_state jsonb
- updated_at timestamptz

#### source_runtime_overrides

- source_id text primary key
- polling_enabled boolean
- poll_interval interval
- reason text
- updated_by uuid
- updated_at timestamptz

Built-in registry synchronization never overwrites this runtime operator override.

#### user_source_preferences

- user_id uuid
- source_id text
- muted boolean
- exclude_from_digest boolean
- relevance_adjustment numeric
- updated_at timestamptz

Muting affects ranking and delivery, not collection. Pausing polling is a separate operator action with an explicit source-health impact.

#### source_fetches

- id bigint identity
- endpoint_id uuid
- attempted_at timestamptz
- completed_at timestamptz
- status_code integer
- final_url text
- content_type text
- bytes bigint
- duration_ms integer
- error_code text
- retry_after timestamptz
- etag text
- last_modified text

#### raw_documents

- id uuid
- source_id text
- canonical_url text
- object_key text
- raw_sha256 bytea
- first_seen_at timestamptz
- source_published_at timestamptz
- content_policy text

#### content_revisions

- id uuid
- raw_document_id uuid
- normalized_sha256 bytea
- normalized_text_object_key text
- parser_name text
- parser_version text
- outline jsonb
- warnings jsonb
- observed_at timestamptz

### 16.3 Intelligence

#### items

- id uuid
- current_revision_id uuid
- title text
- slug text
- lifecycle_state text
- event_type text
- published_at timestamptz
- first_seen_at timestamptz
- status text

#### item_sources

- item_id uuid
- revision_id uuid
- source_role text
- source_tier text
- sort_order integer

#### story_clusters

- id uuid
- primary_item_id uuid
- cluster_key text
- title text
- first_seen_at timestamptz
- last_changed_at timestamptz

#### cluster_members

- cluster_id uuid
- item_id uuid
- similarity numeric
- method text

#### entities

- id uuid
- entity_type text
- canonical_name text
- ecosystem text
- external_ids jsonb

#### item_entities

- item_id uuid
- entity_id uuid
- relationship text
- version_range text

#### embeddings

- id uuid
- entity_type text
- entity_id uuid
- model_id text
- dimensions integer
- embedding vector
- content_sha256 bytea
- created_at timestamptz

#### claims

- id uuid
- item_id uuid
- claim_type text
- claim_text text
- normalized_value jsonb
- confidence text
- verification_state text

#### evidence_spans

- id uuid
- claim_id uuid
- revision_id uuid
- section_path text
- start_offset integer
- end_offset integer
- quote_hash bytea

#### briefs

- id uuid
- item_id uuid
- audience_profile_version integer
- schema_version text
- content jsonb
- score numeric
- action text
- risk text
- effort text
- published_at timestamptz

#### code_examples

- id uuid
- brief_id uuid
- language text
- title text
- code text
- origin text
- source_url text
- verification_state text

### 16.4 AI and evaluations

#### model_configs

- id uuid
- role text
- provider text
- model_id text
- reasoning text
- verbosity text
- max_output_tokens integer
- enabled boolean
- valid_from timestamptz

#### prompt_versions

- id uuid
- purpose text
- semantic_version text
- prompt_sha256 bytea
- schema_version text
- active boolean
- created_at timestamptz

#### ai_runs

- id uuid
- item_id uuid
- purpose text
- model_config_id uuid
- prompt_version_id uuid
- provider_response_id text
- background boolean
- state text
- input_tokens bigint
- cached_input_tokens bigint
- output_tokens bigint
- tool_calls integer
- estimated_cost_usd numeric
- started_at timestamptz
- completed_at timestamptz
- error_code text

#### eval_cases

- id uuid
- suite text
- input_fixture text
- expected jsonb
- source text
- active boolean

#### eval_runs

- id uuid
- git_sha text
- model_config_id uuid
- prompt_version_id uuid
- metrics jsonb
- result text
- created_at timestamptz

### 16.5 Radar and feedback

#### package_candidates

- id uuid
- ecosystem text
- package_name text
- repository_url text
- discovered_at timestamptz
- discovery_source text
- current_status text

#### package_metrics

- candidate_id uuid
- observed_at timestamptz
- release_version text
- license text
- contributor_count integer
- security jsonb
- popularity jsonb
- maintenance jsonb

#### radar_decisions

- id uuid
- candidate_id uuid
- state text
- rationale text
- evidence jsonb
- decided_at timestamptz
- review_at timestamptz

#### feedback

- id uuid
- user_id uuid
- target_type text
- target_id uuid
- feedback_type text
- note text
- created_at timestamptz

#### user_item_states

- user_id uuid
- item_id uuid
- location text check in inbox, later, archive
- is_read boolean
- read_at timestamptz
- starred_at timestamptz
- snoozed_until timestamptz
- snoozed_from_location text
- reading_progress numeric check from 0 through 1
- last_paragraph_id text
- later_position bigint
- dismissed_reason text
- version bigint for optimistic concurrency
- created_at timestamptz
- updated_at timestamptz

Primary key: user_id, item_id.

#### tags

- id uuid
- user_id uuid
- name text
- normalized_name text
- color_token text
- created_at timestamptz

Unique: user_id, normalized_name.

#### item_tags

- user_id uuid
- item_id uuid
- tag_id uuid
- created_at timestamptz

#### annotations

- id uuid
- user_id uuid
- item_id uuid
- revision_id uuid
- annotation_type text check in document_note, highlight, highlight_note
- start_offset integer
- end_offset integer
- quote_hash bytea
- body text
- created_at timestamptz
- updated_at timestamptz

Highlights retain their revision and quote hash. When the source changes, the UI attempts offset remapping and visibly marks an orphaned highlight instead of silently moving it.

#### item_state_mutations

- id uuid
- user_id uuid
- item_id uuid
- mutation_type text
- before_state jsonb
- after_state jsonb
- idempotency_key text
- undo_deadline timestamptz
- undone_at timestamptz
- created_at timestamptz

This table supports audited bulk changes and server-backed Undo. Retention default: 30 days for full before/after state, then retain compact audit metadata.

### 16.6 Delivery and operations

#### digests

- id uuid
- user_id uuid
- schedule_occurrence_id uuid
- local_digest_date date
- window_start timestamptz
- window_end timestamptz
- channel text
- state text
- item_limit integer
- empty_behavior text
- executive_summary text
- generated_at timestamptz

Unique: user_id, local_digest_date, channel.

#### digest_items

- digest_id uuid
- item_id uuid
- sort_order integer
- reason text

#### delivery_attempts

- id bigint identity
- digest_id uuid
- channel text
- provider_id text
- idempotency_key text unique
- state text
- attempted_at timestamptz
- completed_at timestamptz
- error_code text

#### schedule_definitions

- id uuid
- user_id uuid
- schedule_type text check in daily_digest, weekly_radar, maintenance
- timezone text
- local_time time
- days_of_week smallint[]
- enabled boolean
- catchup_policy text check in catch_up, skip
- catchup_grace interval
- next_due_at timestamptz
- skip_next_at timestamptz
- paused_at timestamptz
- config jsonb
- version bigint
- created_at timestamptz
- updated_at timestamptz

#### schedule_occurrences

- id uuid
- schedule_id uuid
- scheduled_for timestamptz
- local_date date
- local_offset_seconds integer
- state text check in due, enqueued, preparing, ready, delivering, delivered, skipped, missed, failed
- trigger_type text check in scheduled, catch_up, run_now, retry
- idempotency_key text unique
- started_at timestamptz
- completed_at timestamptz
- error_code text
- metadata jsonb

Unique: schedule_id, scheduled_for. The occurrence ledger is the durable schedule authority.

#### outbox_events

- id bigint generated always as identity primary key
- event_type text
- aggregate_type text
- aggregate_id uuid
- payload jsonb
- created_at timestamptz

#### audit_events

- id bigint identity
- actor_type text
- actor_id text
- action text
- target_type text
- target_id text
- request_id text
- metadata jsonb
- created_at timestamptz

### 16.7 Database rules

- Every table has explicit constraints and indexes.
- Core queryable fields are typed columns, not hidden in JSONB.
- JSONB is limited to provider payloads, flexible evidence, immutable audit snapshots, and versioned configuration; current queryable state remains typed.
- Migrations are forward-only in production.
- Destructive schema changes use expand, migrate, contract.
- sqlc queries are reviewed SQL.
- A transaction writes business state and outbox events together.
- Row retention jobs are observable and idempotent.
- Add a partial index on schedule_definitions(next_due_at) where enabled and paused_at is null.
- Index schedule_occurrences(state, scheduled_for) and enforce both occurrence unique keys.
- Index user_item_states(user_id, location, is_read, updated_at), user_item_states(user_id, snoozed_until) where snoozed_until is not null, and user_item_states(user_id, starred_at) where starred_at is not null.
- Use a fractional-position or spaced-bigint strategy for Later ordering and compact positions transactionally when gaps are exhausted.

---

## 17. Authentication and security

### 17.1 Authentication

Use Better Auth with GitHub OAuth.

- Create separate GitHub OAuth apps for staging and production. The safe local
  profile uses the reviewed disconnected OAuth fixture; an optional live local
  app remains behind the explicit external-provider fuse.
- Allow only the configured numeric GitHub user ID.
- Reject successful OAuth identities not on the allowlist.
- Secure, HttpOnly, SameSite=Lax cookies.
- Rotate session on login and privilege-sensitive changes.
- Require recent authentication to change delivery endpoints, budgets, or source credentials.

### 17.2 Browser boundary

The browser calls only the Next.js application origin. Next.js:

- Validates the session.
- Applies CSRF/origin checks.
- Calls the private Go API over Railway private networking with a rotated service credential.
- Proxies SSE without buffering.
- Never serializes server secrets into client bundles.

The Go API rejects every application request that lacks the expected internal service credential, except its minimal private health and readiness endpoints. Private networking reduces exposure but does not replace application authentication.

### 17.3 SSRF controls

For every fetched URL:

- HTTPS required, except explicit local test fixtures.
- Resolve DNS before connecting.
- Reject loopback, private, link-local, multicast, documentation, benchmark, and reserved address ranges.
- Pin the validated address for the connection where the client permits.
- Revalidate DNS and destination on redirects.
- Allow only ports 80 and 443.
- Reject userinfo in URLs.
- Cap redirects at five.
- Apply decompression-ratio and body limits.
- Disable local proxy environment inheritance in the fetch client.
- Maintain a source-domain allowlist for permanent endpoints.

### 17.4 Web security

- Content Security Policy with nonces.
- HSTS in production.
- frame-ancestors none.
- nosniff.
- strict referrer policy.
- Permissions Policy denying unused capabilities.
- HTML sanitization.
- URL allowlist.
- Rate limiting on auth, settings mutations, source test, and search.
- Request IDs and audit events.
- No public API documentation in production.

### 17.5 Secrets

- Local: Docker Compose secrets and ignored files generated by make secrets.
- Staging: Railway sealed variables with staging-only credentials.
- Production: independently created Railway sealed variables.
- Never copy production secrets into staging or local.
- Never expose secrets through NEXT_PUBLIC variables.
- Rotate GitHub, OpenAI, Discord, Resend, and OAuth credentials on a documented schedule and after suspected exposure.

### 17.6 Dependency and build security

- Exact dependency versions.
- pnpm minimum release age of seven days, with explicit reviewed exceptions for security fixes.
- Lockfile integrity.
- npm provenance and signature verification where available.
- govulncheck.
- OSV-Scanner.
- CodeQL.
- dependency-review.
- Trivy filesystem and image scans.
- gitleaks.
- zizmor for workflow analysis.
- OpenSSF Scorecard monitoring.
- SBOM generation in SPDX JSON.
- Build provenance attestation when supported by the repository plan.
- GitHub Actions pinned to full commit SHA.
- Container base images pinned by digest after bootstrap.

### 17.7 Content and copyright

- Link to the original.
- Store only what is required for private evidence, dedupe, and reprocessing.
- Never expose full stored source bodies as a publishing feature.
- Respect robots.txt for crawl connectors.
- Prefer official feeds and APIs.
- Do not bypass paywalls.
- Record terms and content-policy review for every source.
- Provide source disable and deletion workflows.

### 17.8 Public demo boundary

- Public routes are an exact allowlist: /demo, /demo/story/{known_fixture_id}, required static assets, health endpoints with their existing restrictions, and authentication endpoints.
- Next.js Proxy may perform an optimistic route redirect, but it is not authorization. Every protected server component, route handler, server action, and data-access function verifies the session at the data boundary.
- No /api/v1 business-data endpoint is anonymous.
- Demo code cannot receive an internal API credential, session cookie value, bearer token, or server-only environment object.
- The demo Content Security Policy excludes connect-src access to the private API, OpenAI, Discord, Resend, object storage, and unlisted origins.
- The demo emits no user-tracking cookie. Optional portfolio analytics must be first-party, aggregate, cookie-free, and separately documented.
- Demo fixtures are reviewed like source fixtures and retain their public-source licenses and attribution.

---

## 18. Observability and reliability

### 18.1 Telemetry

- OpenTelemetry traces and metrics.
- slog JSON logs to stdout with trace_id, span_id, request_id, job_id, source_id, item_id, and deploy SHA.
- Do not depend on OpenTelemetry Go log signal while it remains beta.
- Railway logs and metrics are the baseline.
- Optional Grafana Cloud OTLP export for production.

### 18.2 Service-level objectives

| Objective | Target |
|---|---:|
| Priority source successful poll freshness | 95% within 15 minutes |
| Normal source successful poll freshness | 95% within 60 minutes |
| Confirmed critical advisory alert | 95% within 10 minutes of first observation |
| Daily digest delivery | 99% per rolling 30 days |
| Published brief with primary source | 100% for release/security claims |
| Published factual claims with evidence | 100% |
| Today API availability | 99.5% monthly |
| Search p95 | 300 ms |
| Live publication-to-browser latency | 2 seconds p95 |

### 18.3 Key metrics

- source_poll_due_total
- source_poll_success_total
- source_poll_failure_total
- source_freshness_seconds
- fetch_bytes_total
- parse_failure_total
- duplicate_ratio
- cluster_ambiguous_total
- river_queue_depth
- river_job_latency_seconds
- ai_run_total
- ai_schema_failure_total
- ai_unsupported_claim_total
- ai_cost_usd
- ai_tokens_total
- search_latency_seconds
- digest_delivery_total
- live_connections
- outbox_lag_seconds
- feedback_total

Avoid high-cardinality labels such as raw URL, item title, or provider response ID.

### 18.4 Alerts

Critical:

- Production auth bypass suspicion.
- Secret leak.
- Database unavailable.
- Migration failure.
- Confirmed watched-dependency critical advisory not delivered.
- Backup/restore integrity failure.
- Budget hard cap exceeded.

Warning:

- Priority source freshness SLO breach.
- Queue age above 15 minutes.
- OpenAI error rate above 10 percent for 15 minutes.
- Digest delayed by 15 minutes.
- Object-storage failures.
- Repeated parser drift.

### 18.5 Data retention

Initial defaults:

- Raw source snapshots: 180 days, extend for published evidence when policy permits.
- Normalized revisions: 365 days.
- Published claims, briefs, citations, and audit: indefinite until owner deletes.
- Fetch attempts: 90 days.
- Detailed traces: 14 days.
- Application logs: 30 days.
- Outbox replay: 7 days.
- OpenAI raw response payloads: 30 days when needed for debugging; derived provenance remains.

Retention is configurable and must be documented per environment.

---

## 19. Notifications

### 19.1 Default channels

- Primary product: dashboard.
- Daily digest: private Discord webhook.
- Optional: Resend email.

Default owner-adjustable schedule:

- Daily digest at 08:00 America/New_York.
- Weekly radar review Saturday at 09:00 America/New_York.
- Quiet hours 22:00–07:00.
- Confirmed critical security alerts bypass quiet hours.

### 19.2 Scheduling architecture

The platform is not one large AI crawl that begins at 08:00.

- Source polling, normalization, dedupe, clustering, inexpensive classification, and normal brief generation run continuously.
- The morning schedule selects already-prepared evidence and briefs from a deterministic eligibility window.
- The research model is reserved for incomplete high-value stories before the cutoff.
- At delivery time, AI may write a short executive overview from validated briefs. It does not rescan the web.
- If OpenAI is unavailable, deliver the deterministic ranked digest without the executive overview.

The always-on worker owns application schedules because it is already required for ingestion. Do not create a Railway cron service for the morning digest.

Reason:

- Railway cron schedules are UTC-based rather than owner-timezone-based.
- A Railway cron execution can vary by a few minutes.
- Railway skips a new run if the prior execution remains active.
- The digest requires durable local-time semantics, catch-up, owner controls, and a complete occurrence history.

River OSS periodic schedules are used only to wake the schedule reconciler every minute. Their in-memory schedule is not the durable authority.

Durable algorithm:

1. River configures a one-minute periodic ScheduleReconciler with RunOnStart enabled and a minute-level unique key.
2. The reconciler opens a transaction and acquires a PostgreSQL advisory lock dedicated to schedule reconciliation.
3. It selects due schedule_definitions using next_due_at and locks the rows.
4. It creates missing schedule_occurrences with a unique schedule_id plus scheduled_for key.
5. It enqueues the typed occurrence job in the same transaction.
6. It calculates and persists the next local occurrence.
7. It releases the transaction and lock.
8. A second reconciler or restart sees the occurrence and cannot duplicate it.

Use IANA timezone data and robfig/cron v3 schedule parsing where a cron expression is required. The UI stores understandable local time and selected weekdays; server code derives the schedule.

Embed Go's time/tzdata package in api and worker images so minimal containers have the same reviewed timezone database. Record the Go/tzdata build version in deployment metadata and recompute future next_due_at values after an approved timezone-data upgrade.

Daylight-saving rules:

- Persist scheduled_for in UTC plus local_date and local_offset_seconds.
- An ambiguous fall-back local time runs once at the first occurrence.
- A nonexistent spring-forward local time runs at the first valid minute after the gap.
- Changing timezone or local time updates only future occurrences.
- Tests cover both America/New_York DST transitions for at least ten years.

### 19.3 Morning digest timeline

Default America/New_York timeline:

| Local time | Operation |
|---|---|
| Continuous | Poll sources and prepare normal briefs |
| 07:35 | Check Priority 0 source health and overdue work |
| 07:40 | Enqueue a bounded priority-source catch-up pass |
| 07:45 | Freeze the normal digest eligibility window |
| 07:45–07:55 | Complete already-authorized high-value research |
| 07:57 | Rank, select, and render deterministic channel payloads |
| 07:58 | Optionally generate the short executive overview |
| 08:00 | Deliver dashboard, Discord, and enabled email |
| 08:05 | Delivery SLO deadline and escalation point |

Window rules:

- window_start is the prior successfully delivered digest cutoff.
- On first run, window_start is 24 hours before the cutoff.
- window_end is the current 07:45 cutoff.
- A window longer than 36 hours selects the normal top set and links to a separate backlog view.
- A story updated after cutoff goes into the next digest unless it becomes a confirmed critical security event.
- Source/story eligibility freezes at 07:45, but owner read, archive, snooze, and mute state is rechecked during 07:57 finalization.
- After channel payloads are rendered, later state changes do not mutate or regenerate that occurrence.
- Duplicate story clusters appear once.

Default item allocation for a ten-item digest:

- Up to two critical or high security items.
- Four highest-impact stable releases or breaking changes.
- Two Coming Soon items.
- Two library/tool radar candidates.
- Empty categories give their capacity to the highest remaining score.

### 19.4 Schedule controls and recovery

Owner controls:

- Enabled/disabled.
- Timezone.
- Local delivery time.
- Weekdays.
- Weekend mode: normal, weekly-only, or off.
- Maximum items: 5, 10, 15, or 20.
- Minimum score.
- Include Coming Soon.
- Include radar candidates.
- Include Read Later reminders, off by default.
- Empty behavior: send nothing, send a short all-clear, or dashboard-only.
- Channels.
- Quiet hours.
- Preview next digest.
- Run now.
- Skip next.
- Pause until a selected date.
- Retry a failed delivery.

Run now:

- Creates a distinct occurrence with trigger_type run_now.
- Defaults to a preview and requires confirmation before external delivery.
- Uses an idempotency key so repeated clicks do not duplicate messages.
- If a daily digest has already been delivered for the same owner, local date, and channel, external Run now is disabled; Preview remains available. The owner may instead explicitly retry a failed occurrence or choose a different channel.

Recovery:

- Default catch-up grace is six hours.
- If the 08:00 occurrence was missed, worker startup creates one catch-up occurrence through 14:00 local time.
- After the grace window, mark the occurrence missed, show it in Operations, and offer Run now.
- Never send two daily digests for the same local date and channel unless the owner explicitly retries a failed delivery.
- A delivery failure retries with backoff but reuses the same rendered digest and provider idempotency key.
- Scheduler health alerts when no reconciliation tick succeeds for three minutes or a due occurrence remains unenqueued for five minutes.

### 19.5 Discord

Use a private-channel incoming webhook; a bot is unnecessary for one-way Version 1 delivery.

- Store the webhook URL only as a sealed server variable.
- Use allowed_mentions with all mentions disabled.
- Escape untrusted content.
- Include at most 10 items.
- Link every item back to the private app and primary source.
- Use an idempotency record before sending.
- Apply provider rate limits and retry.

### 19.6 Email

Use Resend only when enabled.

- Verify the sending domain.
- Use a per-digest idempotency key.
- Include text and accessible HTML.
- Do not embed full articles.
- Provide no tracking pixels by default.
- Keep staging delivery on a separate test address.

### 19.7 Alert fatigue

- Coalesce related advisories.
- Cap non-critical instant alerts to five per day.
- Move lower-priority alerts into the next digest.
- Expose why an alert bypassed the digest.
- Allow per-topic and per-severity preferences.

---

## 20. Repository layout

~~~text
/
  apps/
    web/
      src/
        app/
        components/
        features/
        lib/
        stores/
      public/
      package.json
    mobile/                       # added only in Android Phase A0
      app/
      src/
        components/
        data/
        features/
        storage/
        sync/
      assets/
      app.config.ts
      package.json
  packages/
    api-client/                   # generated, platform-neutral TypeScript
    domain/                       # pure TypeScript types, state commands, formatters
    design-tokens/                # semantic tokens; no DOM or native components
  cmd/
    api/
    worker/
    migrate/
    fake-source/
    fake-openai/
    fake-delivery/
  internal/
    api/
    auth/
    config/
    database/
    delivery/
    embedding/
    fetcher/
    jobs/
    live/
    models/
    openai/
    parsing/
    ranking/
    reading/
    scheduler/
    search/
    sources/
    storage/
    telemetry/
  contracts/
    openapi.yaml
    schemas/
  queries/
  migrations/
  prompts/
  evals/
    fixtures/
    golden/
  sources/
    registry.yaml
    fixtures/
  deploy/
    docker/
      web.Dockerfile
      api.Dockerfile
      worker.Dockerfile
      migrate.Dockerfile
    railway/
      parity.yaml
  scripts/
    demo/
    mobile/
  docs/
    adr/
    runbooks/
  compose.yaml
  compose.dev.yaml
  Makefile
  go.mod
  go.sum
  package.json
  pnpm-workspace.yaml
  pnpm-lock.yaml
  biome.json
  renovate.json
  AGENTS.md
  README.md
~~~

One root pnpm lockfile. One root Go module until a measured boundary requires more modules. Version 1 creates packages/api-client, packages/domain, and packages/design-tokens so the later Android application can share contracts and non-visual behavior without moving files out of the web app under pressure. apps/mobile is created only after Phase P10 passes; do not add an empty mobile scaffold to Version 1.

Never create a cross-platform components package. Next.js components use DOM, React Server Components, CSS, shadcn/ui, and Radix; Expo components use React Native primitives and native modules. Share semantic tokens, schemas, pure commands, formatters, and API clients—not rendering code.

---

## 21. Local development

Local development must mirror Railway service boundaries while remaining fast.

### 21.1 Required tools

- Docker Engine or Docker Desktop with Compose v2 and Compose Watch support.
- GNU Make.
- Git.
- Optional: Go, Node, and pnpm on the host for editor tooling and faster focused tests.

make doctor validates:

- Architecture.
- Docker and Compose versions.
- Compose Watch support.
- Available ports.
- Git version.
- Optional host tool versions.
- Disk space.
- Generated secrets.
- No forbidden production credential patterns.

### 21.2 Compose files

compose.yaml is the production-like base:

- postgres with pgvector.
- minio for local S3-compatible storage.
- fake-source server.
- fake-openai server.
- fake-delivery server that captures Discord and Resend-compatible requests.
- migrate.
- api.
- worker.
- web.

compose.dev.yaml is the developer overlay:

- Use Compose Watch for source synchronization and rebuild actions.
- Select development Docker stages.
- Enable Next.js fast refresh.
- Rebuild/restart Go services on relevant changes.
- Expose loopback-only ports.
- Add debugging profiles.
- Use safe local credentials.

Profiles:

- default: complete safe application with fake external providers.
- observability: pinned grafana/otel-lgtm development image, OTLP ingestion, traces, metrics, and logs.
- live-providers: explicitly authorized OpenAI and read-only GitHub access.
- live-delivery: explicitly authorized staging-only Discord/email delivery.
- tools: optional database and object-storage administrative UIs.

The observability image is development/test-only. It does not define the production telemetry backend.

Railway builds the same production Dockerfile stages used by make prodlike. Railway does not deploy from Compose.

Fake provider binaries and fixed-clock development routes exist only in development/test image stages. CI inspects production images and fails if a fake provider binary, test authentication route, CLOCK_MODE=fixed default, or live-delivery bypass is present.

### 21.3 Local ports

Bind only to 127.0.0.1:

| Service | Port |
|---|---:|
| web | 3000 |
| api debug access | 8080 |
| PostgreSQL | 5432 |
| MinIO S3 | 9000 |
| MinIO console | 9001 |
| fake source | 8090 |
| fake OpenAI | 8091 |
| fake delivery | 8092 |
| local Grafana observability profile | 3001 |
| OTLP gRPC/HTTP observability profile | 4317 / 4318 |

The normal web path still uses the internal api hostname. Direct API exposure is for debugging and tests.

### 21.4 Local secrets

make secrets creates ignored files under .local/secrets:

- database_password
- better_auth_secret
- local_oauth_stub_secret
- minio_access_key
- minio_secret_key
- fake_openai_secret
- fake_delivery_secret

The default local profile cannot call real OpenAI, GitHub write APIs, Discord, Resend, or Railway.

make dev-live explicitly enables real read-only provider calls and requires:

- OPENAI_API_KEY.
- GITHUB read credential.
- Acknowledgement flag ALLOW_LIVE_EXTERNAL_APIS=true.
- Local monthly/daily caps.

It still cannot send Discord/email unless ALLOW_LIVE_DELIVERY=true is separately set.

### 21.5 Health-gated startup

Order:

1. postgres healthy.
2. minio healthy and bucket bootstrap complete.
3. migrate exits successfully.
4. fake source, OpenAI, and delivery providers healthy.
5. api and worker start.
6. web starts after api health.

Use Compose health conditions. Do not use fixed sleep delays.

### 21.6 Makefile contract

| Target | Behavior |
|---|---|
| make doctor | Validate tools, ports, versions, and safe config |
| make bootstrap | Doctor, secrets, images, install, generate, migrate, seed |
| make secrets | Generate local-only ignored secrets |
| make dev | Start safe hot-reload stack |
| make dev-live | Start with explicitly approved real read-only providers |
| make ps | Show services and health |
| make logs | Follow structured logs |
| make stop | Stop without deleting data |
| make test | Unit and integration tests |
| make test-unit | Go and frontend unit tests |
| make test-integration | PostgreSQL, storage, connector, and API tests |
| make test-e2e | Playwright against local production-like stack |
| make lint | Biome, gofmt check, go vet, static checks |
| make typecheck | TypeScript and generated-client checks |
| make generate | sqlc, OpenAPI, schemas, and clients |
| make generate-check | Fail on generated drift |
| make migrate | Apply local migrations |
| make migration name=... | Create a timestamped migration |
| make seed | Idempotent local seed |
| make sources-verify | Probe and validate source registry |
| make fixtures-record source=... | Record sanitized fixture with explicit live acknowledgement |
| make eval | Run AI and ranking evaluation suite |
| make scheduler-tick | Run one safe local schedule reconciliation |
| make digest-preview | Render the next digest without external delivery |
| make digest-run | Create a local run-now preview; delivery still requires deliver=true and the delivery fuse |
| make test-scheduler | Run occurrence, catch-up, idempotency, and missed-run tests |
| make test-dst | Test scheduler behavior across timezone and DST fixtures |
| make time-travel at=... | Start an isolated disposable stack with a fixed injected clock |
| make time-travel-clean | Remove only the isolated time-travel project and volumes after confirmation |
| make observability | Start the optional local OpenTelemetry LGTM profile |
| make config-check | Validate config schema, env examples, Compose, Docker, and Railway parity |
| make demo | Start the safe local stack and open /demo without authentication |
| make demo-audit | Build and test the anonymous demo bundle, network isolation, forbidden imports, fixture sanitization, accessibility, and performance budget |
| make prepush | Required local fast release checks |
| make prodlike | Build and run exact production stages |
| make prodlike-smoke | Smoke test production-like stack |
| make sbom | Generate local SBOMs |
| make clean | Remove build outputs, preserve volumes |
| make reset | Confirm, then delete only this project's local volumes/data |

make reset must resolve the exact Compose project name and require interactive confirmation unless CI=true and the target is an ephemeral CI project.

### 21.7 Compose Watch rules

Web:

- Sync apps/web/src and public.
- Rebuild on package.json, root lockfile, Next config, TypeScript config, or generated client change.

Go:

- Rebuild/restart the affected service on .go changes.
- Rebuild all Go services on go.mod or go.sum.
- Regenerate before restart when queries, migrations, schemas, or contracts change.

Sources and prompts:

- Restart worker after source registry or prompt changes.
- Never mutate production prompt configuration from local files.

### 21.8 Production-like rehearsal

make prodlike:

- Builds production Docker stages with BuildKit.
- Uses no host source mounts.
- Runs compiled Go binaries as non-root.
- Runs the standalone Next.js production server.
- Applies migrations through the migrate image.
- Uses production-equivalent health checks and start commands.
- Uses fake external providers.
- Runs smoke and E2E tests.

The container image built locally must match the Railway Dockerfile path, stage, build arguments, and runtime command defined in deploy/railway/parity.yaml.

### 21.9 Deterministic clock and morning-job development

All scheduler and digest code receives a Clock interface.

Local and test modes:

- CLOCK_MODE=system uses the real clock.
- CLOCK_MODE=fixed requires TEST_NOW in RFC 3339 form.
- make time-travel at=2026-11-01T05:55:00Z starts an isolated Compose project with dedicated disposable database, object-storage, and provider-capture volumes.
- make test-dst uses isolated test databases and does not modify normal local state.
- make time-travel-clean removes only the validated time-travel Compose project and its dedicated volumes.

Production guard:

- APP_ENV=production refuses to start unless CLOCK_MODE=system and TEST_NOW is empty.

Required local scenarios:

1. Start before 07:35 and observe preflight, cutoff, assembly, and delivery capture.
2. Start at 08:20 and observe one catch-up occurrence.
3. Restart twice during the due minute and observe one occurrence.
4. Advance beyond the six-hour grace window and observe missed rather than delivered.
5. Exercise spring-forward and fall-back dates.
6. Preview, Run now, Skip next, pause, resume, and retry.
7. Force OpenAI failure and verify deterministic digest delivery.
8. Force Discord/Resend failure and verify identical-payload retry.

### 21.10 Local UX and delivery verification

- fake-delivery stores request headers, idempotency key, sanitized payload, attempt number, and received time.
- Its loopback-only viewer renders Discord cards and email HTML for inspection.
- Storybook includes all card states, list densities, reader typography, undo toasts, bulk confirmation, empty digest, overdue digest, and degraded-source states.
- Playwright runs keyboard-only triage from Inbox through Later, Starred, Snoozed, and Archive.
- Visual regression covers 360, 768, 1280, and 1600-pixel viewports in both themes.
- make config-check compares every required environment variable with the typed Go and TypeScript configuration schemas, .env.local.example, Compose, and deploy/railway/parity.yaml.
- make demo uses the same production web build with the static demo adapter; it does not start fake-openai merely to render the route.
- make demo-audit captures the anonymous request graph and proves that /demo has no application API, auth, SSE, database, provider, or delivery dependency.

---

## 22. Git workflow and GitHub setup

### 22.1 Branch model

- staging is the default integration branch.
- master is production releases only.
- Feature and dependency PRs target staging.
- Release PRs target master and must originate from staging.
- Emergency production fixes branch from master, merge to master, then immediately back-merge to staging.

### 22.2 Repository creation

1. Create a private GitHub repository.
2. Create staging and master.
3. Set staging as default.
4. Disable merge commits.
5. Enable squash merge for normal PRs.
6. Enable auto-delete head branches.
7. Create environments named staging and production.
8. If the repository plan supports required reviewers for private-repository environments, add them. Otherwise, do not claim that approval exists: require a manual workflow_dispatch from master with the exact signed release tag, full commit SHA, and a typed DEPLOY_PRODUCTION confirmation before production secrets are read.
9. Enable private vulnerability reporting.
10. Enable Dependabot alerts, security updates, dependency graph, secret scanning, and push protection where available.
11. Install only required GitHub Apps.

### 22.3 Ruleset for staging

- Require pull request.
- Require one approval after the project has another trusted reviewer; owner-only bootstrap can temporarily require zero but must still require checks.
- Dismiss stale approvals.
- Require conversation resolution.
- Require linear history.
- Block force pushes and deletion.
- Require status checks:
  - policy
  - frontend
  - go
  - db-integration
  - contract-drift
  - source-fixtures
  - security
  - container-build
  - e2e
  - demo
- Require branch up to date.
- Allow only reviewed bypass for emergency administration.

### 22.4 Ruleset for master

- Require pull request.
- Only release PR workflow can validate source.
- Require production environment approval when the repository plan supports it; otherwise require the attended workflow_dispatch contract from Section 22.2.
- Block force pushes and deletion.
- Require signed release tag.
- Require:
  - release-source
  - release-tree
  - release-evidence
  - all staging checks
- release-source proves the head ref is staging.
- release-tree proves the tree promoted to master exactly matches the approved staging tree, excluding permitted release metadata.
- master-integrity continuously verifies no commit bypassed the release contract.

### 22.5 GitHub Actions

At PR 0, select current official stable major versions and pin every action to a full commit SHA with a comment naming its release.

Current release lines observed during this audit include checkout and setup-node v7-era actions, setup-go v7, upload-artifact v7, and CodeQL v4. Recheck before writing workflow files.

Workflows:

#### ci.yml

Triggers on PRs to staging and master.

Jobs:

- policy: branch target, forbidden packages, generated artifacts, lockfile, action pins.
- frontend: pnpm install frozen, Biome, TypeScript, unit tests, bundle budget.
- go: gofmt, go vet, test, race test, govulncheck.
- db-integration: PostgreSQL 18 plus pgvector, migrations, sqlc query tests.
- contract-drift: generate OpenAPI and TypeScript client, require clean diff.
- source-fixtures: parse all recorded source fixtures and snapshot expected normalized output.
- eval-smoke: fixed zero-network fixtures with budget-free mock model.
- security: dependency review, OSV, gitleaks, zizmor, Trivy.
- container-build: build all production stages and generate SBOM.
- e2e: production-like stack and Playwright.
- demo: static build, forbidden-import scan, sanitized-fixture scan, anonymous request-graph test, Playwright guided journey, axe, and Lighthouse budgets.

#### nightly.yml

- Full fuzzing timebox.
- Extended eval suite.
- Source registry live probe.
- Dependency freshness report.
- Container and OS vulnerability refresh.
- Backup metadata check.
- Search relevance suite.

#### dependency-review.yml

- Review update PR.
- Enforce license policy.
- Enforce provenance/signature checks.
- Apply release-age rule.
- Block known critical vulnerabilities.

#### release.yml

- Validate exact staging source and tree.
- Run complete tests.
- Build once from an immutable git archive.
- Generate SBOM and provenance.
- Publish release evidence artifact.
- Create signed tag after master merge.
- Does not automatically deploy production.

#### master-integrity.yml

- Verify each master commit came through an approved release PR or documented hotfix.

#### demo-audit.yml

- Run on every change to shared UI, auth/proxy rules, public routes, demo fixtures, or environment schemas.
- Build with DEMO_ENABLED=true and no database, internal API, Better Auth, OpenAI, Discord, Resend, or object-storage secret.
- Fail if /demo cannot render, emits a protected-network request, contains a forbidden data pattern, or makes an authenticated route reachable anonymously.

### 22.6 Dependency updates

Use Renovate for controlled grouping and pnpm-aware minimum-release-age policy, or Dependabot if the implementation team accepts its reduced grouping flexibility. Do not run both for the same manifest.

Recommended:

- Renovate targets staging.
- Weekly routine updates.
- Security updates as soon as verified.
- Group patch updates by ecosystem.
- Separate major updates.
- Require changelog, compatibility, provenance, and eval evidence.
- Never auto-merge production dependency changes.

---

## 23. Railway architecture and setup

### 23.1 Project creation

1. Create one Railway project.
2. Create isolated staging and production environments.
3. Do not clone secrets between them.
4. Add PostgreSQL with pgvector support separately to each environment.
5. Add one private bucket per environment.
6. Add web, api, worker, and migrate services from the GitHub repository.
7. Set every service root directory to the repository root.
8. Set RAILWAY_DOCKERFILE_PATH for each service.
9. Use the same Dockerfile production stage as make prodlike.
10. Enable Railway private networking.
11. Give only web a public domain.
12. Set health endpoints and restart policies.
13. Configure staging to deploy from staging after GitHub CI succeeds.
14. Connect production to master but disable automatic deployment.
15. Require attended production deployment after release evidence review.

### 23.2 Service settings

#### web

- Dockerfile: deploy/docker/web.Dockerfile.
- Public health: /healthz.
- Internal API URL uses private DNS.
- Minimum one replica.
- Restart on failure.

#### api

- Dockerfile: deploy/docker/api.Dockerfile.
- Private health: /healthz.
- Readiness includes database connectivity.
- Graceful shutdown drains HTTP and SSE connections.

#### worker

- Dockerfile: deploy/docker/worker.Dockerfile.
- Private health reports process and queue-poll health.
- Graceful shutdown stops taking jobs and gives active jobs a bounded completion window.
- River uniqueness and idempotency prevent duplicate work after restart.

#### migrate

- Dockerfile: deploy/docker/migrate.Dockerfile.
- One-shot command.
- Must complete before new application services receive traffic.
- Production migration is separately attended.

### 23.3 Deployment behavior

Staging:

- Autodeploy from staging only after required GitHub checks.
- Run migration job.
- Deploy api and worker.
- Deploy web.
- Run smoke tests.
- Roll back automatically or manually on health failure.

Production:

- Merge approved staging release PR into master.
- Verify release evidence and backup.
- Start attended deployment.
- Run migration preflight.
- Apply safe forward migration.
- Deploy api/worker/web from the same release SHA.
- Run production smoke tests.
- Observe error, queue, source, and live-stream metrics for 30 minutes.
- Record release result.

Do not rebuild from a mutable branch head after approval. Deploy the approved commit SHA/tree.

### 23.4 Database and pgvector

The default Railway PostgreSQL service may not include every extension. Use a maintained PostgreSQL 18 image/template with pgvector, pin its image digest, and validate:

~~~sql
select current_setting('server_version');
select extversion from pg_extension where extname = 'vector';
~~~

If pgvector cannot be supported safely, use the EmbeddingIndex interface's PostgreSQL-array fallback for the small personal corpus until the database image is corrected. Do not introduce a separate vector database as an unreviewed workaround.

### 23.5 Backups

- Railway volume snapshots daily.
- PITR when supported by the selected database plan.
- Nightly encrypted logical pg_dump to a separate owner-controlled destination.
- Pre-production-deploy backup marker.
- Monthly automated restore test in an isolated temporary database.
- Quarterly full disaster-recovery drill.
- Record RPO and RTO evidence.

Targets:

- RPO: 24 hours initially, 1 hour after production is trusted.
- RTO: 4 hours.

### 23.6 Regions

Place web, api, worker, and PostgreSQL in the same Railway region. Select the closest stable supported region to the owner, subject to database availability and compliance. Do not hardcode a region before account availability is checked.

### 23.7 Scheduler deployment

- Keep worker continuously running in staging and production.
- Configure at least one worker replica; River leader election and the database occurrence ledger prevent duplicate schedule insertion.
- Do not configure the worker service itself as a Railway cron job.
- The worker starts the ScheduleReconciler with RunOnStart and a one-minute tick.
- Readiness exposes the most recent successful scheduler tick and oldest overdue occurrence.
- A deployment is unhealthy when the scheduler is enabled and no tick succeeds for three minutes.
- During graceful shutdown, finish the current reconciliation transaction but do not wait indefinitely for long AI work.

Railway cron may be used only for short-lived, non-critical tasks that exit, such as an additional off-platform backup command. Even those jobs must write an idempotent execution record because Railway can skip a scheduled execution when the prior run is still active.

---

## 24. Environment variables

Variables are validated at process start. Unknown production variables generate a warning; missing required variables fail fast. No secret may use a NEXT_PUBLIC prefix.

### 24.1 Shared non-secret variables

| Variable | Example | Scope |
|---|---|---|
| APP_ENV | local, staging, production | all |
| APP_VERSION | release tag | all |
| GIT_SHA | full commit SHA | all |
| LOG_LEVEL | info | all |
| TZ | UTC | all |
| CLOCK_MODE | system | api/worker; fixed allowed only in isolated local/test |
| TEST_NOW | empty | local/test only; forbidden in production |
| PUBLIC_BASE_URL | https://app.example.com | web/api |
| INTERNAL_API_URL | http://api.railway.internal:8080 | web |
| DATABASE_MAX_CONNS | 20 | api/worker |
| DATABASE_MIN_CONNS | 2 | api/worker |
| HTTP_PORT | 8080 | api |
| METRICS_ENABLED | true | api/worker |
| OTEL_SERVICE_VERSION | release tag | all |
| OTEL_EXPORTER_OTLP_ENDPOINT | provider endpoint | all |
| OTEL_EXPORTER_OTLP_PROTOCOL | grpc | all |
| OTEL_RESOURCE_ATTRIBUTES | deployment.environment=... | all |

Containers, logs, traces, and database timestamps remain UTC. Owner-facing schedules and dates use the IANA timezone stored in the schedule definition.

### 24.2 Shared secrets

| Variable | Scope |
|---|---|
| DATABASE_URL | web, api, worker, migrate |
| INTERNAL_API_SHARED_SECRET | web and api |
| OTEL_EXPORTER_OTLP_HEADERS | services when external OTLP is enabled |
| SENTRY_DSN | web/api only if Sentry is selected |

### 24.3 Web and authentication

| Variable | Secret | Requirement |
|---|---:|---|
| BETTER_AUTH_SECRET | yes | unique per environment, at least 32 random bytes |
| BETTER_AUTH_URL | no | exact public base URL |
| GITHUB_OAUTH_CLIENT_ID | no | separate OAuth app per environment |
| GITHUB_OAUTH_CLIENT_SECRET | yes | sealed |
| AUTH_ALLOWED_GITHUB_USER_ID | no | numeric owner ID |
| SESSION_MAX_AGE_SECONDS | no | 604800 default |
| NEXT_PUBLIC_APP_ENV | no | environment badge only |
| NEXT_PUBLIC_BUILD_SHA | no | short display SHA |
| CSP_REPORT_ONLY | no | true during staging rollout, false after validation |
| DEMO_ENABLED | no | true for reviewed staging/production web releases; server-only route fuse |
| DEMO_DATASET_VERSION | no | immutable fixture snapshot identifier shown in the UI |
| DEMO_CONTACT_URL | no | owner-approved portfolio or contact URL |
| DEMO_CASE_STUDY_URL | no | optional public case study; empty when unavailable |

DEMO_ENABLED is not a security control for owner data. The demo remains isolated even when enabled, and protected routes remain protected even when disabled. Do not create NEXT_PUBLIC variants for secrets or use a client flag to decide authorization.

### 24.4 Object storage

| Variable | Secret |
|---|---:|
| OBJECT_STORAGE_ENDPOINT | no |
| OBJECT_STORAGE_REGION | no |
| OBJECT_STORAGE_BUCKET | no |
| OBJECT_STORAGE_ACCESS_KEY_ID | yes |
| OBJECT_STORAGE_SECRET_ACCESS_KEY | yes |
| OBJECT_STORAGE_FORCE_PATH_STYLE | no |
| OBJECT_STORAGE_PREFIX | no |

Use different credentials and buckets in staging and production.

### 24.5 Fetching

| Variable | Default |
|---|---:|
| CRAWLER_USER_AGENT | DeveloperIntelligence/1.0 |
| CRAWLER_CONTACT_EMAIL | owner-controlled address |
| FETCH_CONNECT_TIMEOUT | 5s |
| FETCH_HEADER_TIMEOUT | 10s |
| FETCH_TOTAL_TIMEOUT | 30s |
| FETCH_MAX_REDIRECTS | 5 |
| FETCH_MAX_FEED_BYTES | 5242880 |
| FETCH_MAX_ARTICLE_BYTES | 15728640 |
| FETCH_GLOBAL_CONCURRENCY | 20 |
| FETCH_PER_HOST_CONCURRENCY | 2 |
| FETCH_RETRY_MAX | 5 |
| ROBOTS_CACHE_TTL | 24h |
| SOURCE_REGISTRY_PATH | /app/sources/registry.yaml |

The actual User-Agent must include a working contact URL or email. Do not ship the placeholder.

### 24.6 GitHub ingestion

Preferred production authentication: GitHub App with read-only public metadata.

| Variable | Secret |
|---|---:|
| GITHUB_AUTH_MODE | no |
| GITHUB_APP_ID | no |
| GITHUB_APP_INSTALLATION_ID | no |
| GITHUB_APP_PRIVATE_KEY | yes |
| GITHUB_READ_TOKEN | yes |
| GITHUB_API_BASE_URL | no |

Only one credential mode is active. A fine-grained read token is acceptable for local development. Never grant repository write or administration permission to the ingestion worker.

### 24.7 OpenAI

| Variable | Secret | Default |
|---|---:|---|
| OPENAI_API_KEY | yes | environment-specific project key |
| OPENAI_PROJECT_ID | no | environment project |
| OPENAI_ORG_ID | no | optional |
| OPENAI_WEBHOOK_SECRET | yes | per environment |
| WEB_INTERNAL_SERVICE_TOKEN | yes | distinct per environment; shared only by web and private API |
| OPENAI_BASE_URL | no | https://api.openai.com in hosted environments |
| FAKE_OPENAI_URL | no | http://fake-openai:8091 in the disconnected local profile only |
| OPENAI_FAST_ENABLED | no | false in hosted environments; true against local fake provider |
| OPENAI_RESEARCH_ENABLED | no | false in hosted environments; true against local fake provider |
| OPENAI_MODEL_FAST | no | gpt-5.6-luna |
| OPENAI_MODEL_RESEARCH | no | gpt-5.6-terra |
| OPENAI_MODEL_DEEP | no | gpt-5.6-sol |
| OPENAI_EMBEDDING_MODEL | no | text-embedding-3-small |
| OPENAI_FAST_REASONING | no | low |
| OPENAI_FAST_MAX_OUTPUT_TOKENS | no | 4096 |
| OPENAI_RESEARCH_REASONING | no | medium |
| OPENAI_RESEARCH_MAX_OUTPUT_TOKENS | no | 8192 |
| OPENAI_RESEARCH_MAX_TOOL_CALLS | no | 4 |
| OPENAI_RESEARCH_ALLOWED_DOMAINS | no | reviewed primary-source domain allowlist |
| OPENAI_RESEARCH_BLOCKED_DOMAINS | no | reviewed redirect, paste, and untrusted-host blocklist |
| OPENAI_DEEP_REASONING | no | high |
| OPENAI_VERBOSITY | no | low |
| OPENAI_BACKGROUND_ENABLED | no | true in hosted envs |
| OPENAI_BATCH_ENABLED | no | true after validation |
| OPENAI_MONTHLY_SOFT_USD | no | 25 |
| OPENAI_MONTHLY_HARD_USD | no | 50 |
| OPENAI_MAX_DOCUMENT_AGE | no | 720h |
| OPENAI_DAILY_WEB_SEARCH_LIMIT | no | 100 |
| OPENAI_DAILY_DEEP_LIMIT | no | 3 |

Staging uses a distinct OpenAI project and low cap. Local defaults to fake-openai. Production keys never exist in GitHub Actions unless a specifically approved production workflow requires them; routine CI uses mocks or limited staging eval keys.

### 24.8 Queue

| Variable | Default |
|---|---:|
| RIVER_QUEUES | critical=4,fetch=12,parse=8,ai_fast=4,ai_research=2,delivery=2,maintenance=1 |
| RIVER_MAX_ATTEMPTS | 5 |
| RIVER_FETCH_TIMEOUT | 1m |
| RIVER_AI_TIMEOUT | 10m |
| RIVER_RESEARCH_TIMEOUT | 30m |
| RIVER_GRACEFUL_STOP_TIMEOUT | 30s |
| SCHEDULER_ENABLED | true |
| SCHEDULER_TICK_INTERVAL | 1m |
| SCHEDULER_LOCK_NAMESPACE | developer-intelligence:schedule-reconciler |
| SCHEDULER_OVERDUE_ALERT_AFTER | 5m |
| SCHEDULER_DEFAULT_CATCHUP_GRACE | 6h |

Store queue definitions as parsed configuration, validate names and positive worker counts, and expose the active configuration.

### 24.9 Search and clustering

| Variable | Default |
|---|---:|
| SEARCH_HYBRID_ENABLED | true |
| SEARCH_RRF_K | 60 |
| DEDUPE_SIMHASH_DISTANCE | 17; promoted from the labeled precision fixture |
| DEDUPE_EMBEDDING_THRESHOLD | 0.86; promoted from the labeled embedding fixture |
| CLUSTER_MAX_AGE | 30d |
| EMBEDDING_DIMENSIONS | verified from configured model |

Do not invent similarity thresholds. Establish them from the labeled dedupe and cluster evaluation set.

### 24.10 Delivery

| Variable | Secret | Default |
|---|---:|---|
| DISCORD_ENABLED | no | true in production after test |
| DISCORD_WEBHOOK_URL | yes | none |
| RESEND_ENABLED | no | false |
| RESEND_API_KEY | yes | none |
| EMAIL_FROM | no | verified sender |
| EMAIL_TO | yes | owner address |
| DEFAULT_DIGEST_TIME | no | 08:00 |
| DEFAULT_DIGEST_TIMEZONE | no | America/New_York |
| DEFAULT_DIGEST_DAYS | no | 1,2,3,4,5,6,7 |
| DEFAULT_DIGEST_MAX_ITEMS | no | 10 |
| DEFAULT_DIGEST_MIN_SCORE | no | 60 |
| DEFAULT_DIGEST_EMPTY_BEHAVIOR | no | dashboard-only |
| DEFAULT_DIGEST_INCLUDE_LATER | no | false |
| DEFAULT_DIGEST_PREPARE_LEAD | no | 25m |
| DEFAULT_DIGEST_CUTOFF_LEAD | no | 15m |
| DEFAULT_WEEKEND_MODE | no | normal |
| DEFAULT_WEEKLY_RADAR_DAY | no | Saturday |
| DEFAULT_WEEKLY_RADAR_TIME | no | 09:00 |
| QUIET_HOURS_START | no | 22:00 |
| QUIET_HOURS_END | no | 07:00 |
| MAX_NONCRITICAL_ALERTS_PER_DAY | no | 5 |

These are seed defaults for a new owner configuration. The active schedule is stored in schedule_definitions and changed through the authenticated UI; editing a Railway variable is not the normal scheduling workflow.

DEFAULT_DIGEST_DAYS uses ISO weekdays: 1 is Monday and 7 is Sunday.

### 24.11 Environment-specific fuses

Local:

- EXTERNAL_PROVIDER_MODE=fake.
- DELIVERY_MODE=log.
- CLOCK_MODE=system unless an explicit time-travel target is running.
- ALLOW_LIVE_EXTERNAL_APIS=false.
- ALLOW_LIVE_DELIVERY=false.
- DEMO_ENABLED=true with the committed fixture snapshot.

Staging:

- Dedicated credentials.
- Discord test channel.
- Resend test recipient.
- Low OpenAI hard cap.
- Production data import prohibited.
- DEMO_ENABLED=true for anonymous route and security testing.

Production:

- Sealed variables.
- Production OAuth app.
- Production private webhook.
- Manual deployment.
- Full backups and alerting.
- CLOCK_MODE=system with TEST_NOW unset; startup fails otherwise.
- DEMO_ENABLED=true only after the demo audit and owner fixture review pass.

---

## 25. API contract

Base: /api/v1

Key endpoints:

- GET /healthz
- GET /readyz
- GET /me
- GET /today
- GET /live
- GET /stories
- GET /stories/{id}
- POST /stories/{id}/feedback
- PATCH /stories/{id}/state
- POST /stories/{id}/undo
- POST /stories/bulk-state
- POST /inbox/import-url
- GET /inbox
- GET /later
- PUT /later/order
- GET /starred
- GET /archive
- GET /snoozed
- GET /tags
- POST /tags
- PATCH /tags/{id}
- DELETE /tags/{id}
- GET /stories/{id}/annotations
- POST /stories/{id}/annotations
- PATCH /annotations/{id}
- DELETE /annotations/{id}
- GET /releases
- GET /radar
- GET /radar/{id}
- POST /radar/{id}/decision
- GET /search
- GET /sources
- GET /sources/{id}
- POST /sources/{id}/test
- POST /sources/{id}/pause
- POST /sources/{id}/resume
- GET /settings/interests
- PUT /settings/interests
- GET /settings/delivery
- PUT /settings/delivery
- GET /settings/shortcuts
- PUT /settings/shortcuts
- GET /schedules
- PUT /schedules/{id}
- POST /schedules/{id}/preview
- POST /schedules/{id}/run-now
- POST /schedules/{id}/skip-next
- POST /schedules/{id}/pause
- POST /schedules/{id}/resume
- GET /digests
- GET /digests/{id}
- POST /digests/{id}/retry-delivery
- POST /exports/markdown
- GET /exports/opml
- POST /imports/opml/preview
- POST /imports/opml/commit
- GET /operations/overview
- GET /operations/jobs
- POST /operations/jobs/{id}/retry

Public web-only integration endpoint:

- POST /api/webhooks/openai

Anonymous presentation routes:

- GET /demo
- GET /demo/story/{known_fixture_id}

There is no anonymous demo data API. Demo data is part of the reviewed static build. Every /api/v1 endpoint except its already-defined minimal health behavior requires an authenticated owner session in Version 1.

The web route verifies the signature against the untouched raw body, permits only documented OpenAI event types, and submits the verified provider event to a private idempotent API operation that is not exposed through the browser-facing BFF.

Rules:

- Huma request and response types generate OpenAPI 3.1.
- Every mutation supports an idempotency key where repetition is plausible.
- State mutations accept the current version and return the new version for optimistic concurrency.
- Bulk state changes return mutation IDs and an undo deadline.
- Pagination is cursor-based.
- Timestamps are RFC 3339 UTC.
- IDs are UUID strings except live cursors and attempt IDs.
- Errors follow one problem-details schema.
- Every response includes request ID.
- Conditional GET uses ETag for stable reads.

---

## 26. Background jobs

River queues:

- critical: security and source-outage work.
- fetch: scheduled connector calls.
- parse: normalization and revision processing.
- ai_fast: classification and extraction.
- ai_research: high-value synthesis.
- delivery: Discord/email.
- maintenance: retention, embeddings, reconciliation, and metrics.

Jobs:

- ReconcileSchedules.
- ScheduleDueSources.
- FetchEndpoint.
- NormalizeRevision.
- DeduplicateItem.
- ClusterStory.
- ClassifyItem.
- ExtractFacts.
- VerifyClaims.
- ResearchStory.
- PublishBrief.
- PreflightDigestSources.
- PrepareDailyDigest.
- FinalizeDailyDigest.
- DeliverDigest.
- ReturnSnoozedItems.
- PollOpenAIBackground.
- RefreshPackageMetrics.
- RunWeeklyRadarDiscovery.
- ReembedEntity.
- ReconcileSourceHealth.
- EnforceRetention.
- ReconcileOutbox.

Requirements:

- Typed argument structs.
- Unique keys for fetch window, revision processing, digest window, and provider delivery.
- Transactional enqueue with source/business updates.
- Bounded exponential retry.
- Permanent error classification.
- Dead-letter visibility.
- Manual retry with audit.
- No job depends on in-memory state.

### 26.1 Schedule and digest job contracts

ReconcileSchedules:

- Runs every minute and on worker leader start.
- Reads database schedule definitions.
- Creates durable occurrences and advances next_due_at transactionally.
- Has no OpenAI or delivery authority.

PreflightDigestSources:

- Checks Priority 0 source freshness.
- Enqueues only due or overdue source polls.
- Does not indiscriminately refetch healthy sources.

PrepareDailyDigest:

- Freezes window_start and window_end.
- Selects candidates deterministically.
- Enqueues only missing high-value research allowed by budget and deadline.
- Records the exact candidate set.

FinalizeDailyDigest:

- Revalidates candidate availability and citations.
- Applies category allocation, score, read/archive/snooze exclusions, optional Later reminders, mutes, and duplicate suppression.
- Renders immutable channel payloads.
- May request one bounded executive overview.
- Falls back to a deterministic heading when the model is unavailable.

DeliverDigest:

- Reads the immutable rendered payload.
- Uses channel-specific idempotency.
- Never regenerates content on retry.
- Records provider response and completion.

ReturnSnoozedItems:

- Clears snoozed_until while preserving the item's existing Inbox or Later location.
- Marks them unread.
- Writes a state mutation and outbox event.
- Uses the item and snooze timestamp as the unique key.

### 26.2 Scheduler invariants

- One occurrence per schedule and scheduled UTC instant.
- One daily digest per owner, local date, and channel.
- At-most-one visible delivery under retries; provider idempotency is used where supported.
- A worker restart cannot erase a due or completed occurrence.
- Schedule changes never rewrite completed occurrence history.
- Manual Run now does not advance the next scheduled occurrence.
- Skip next identifies one future occurrence and cannot accidentally disable the schedule.
- A paused schedule keeps computing an inspectable next nominal time but does not enqueue work.

---

## 27. Testing

### 27.1 Go

- Unit tests.
- Table-driven parser tests.
- Database integration tests against PostgreSQL 18 plus vector.
- Race detector.
- Fuzz tests for feed, HTML, URL, SSE, webhook, and schema parsing.
- Golden normalized-content fixtures.
- HTTP fault injection.
- Clock-controlled scheduler tests.
- Timezone, DST, missed-run, catch-up, Run-now, Skip-next, pause, resume, and duplicate-reconciler tests.
- Idempotency and duplicate-delivery tests.

### 27.2 Frontend

- Vitest unit tests.
- Testing Library component tests.
- Storybook interaction and accessibility tests for reusable UI.
- Playwright authenticated workflows.
- Visual regression for Night Index and Day Index.
- axe accessibility checks.
- Reduced-motion tests.
- SSE disconnect/resume tests.
- Inbox/Later/Starred/Archive/Snoozed state-transition tests.
- Optimistic concurrency, failed-mutation rollback, ten-second Undo, and bulk-action tests.
- Command palette and every default keyboard shortcut.
- Reader typography, progress restoration, highlights, orphaned-highlight warning, and Markdown export.
- Mobile, tablet, and three-pane desktop layouts.
- Anonymous /demo guided and explore journeys with the API, auth, SSE, provider, and delivery services blocked at the test proxy.
- Demo bundle forbidden-import, secret-pattern, owner-data, static metadata, snapshot-date, and no-cookie tests.
- Demo keyboard, screen-reader, reduced-motion, 360/768/1280/1600 visual-regression, Lighthouse, and cache-header tests.

### 27.3 Source fixtures

Each connector requires:

- Normal document.
- Empty document.
- Malformed document.
- Redirect.
- 304.
- 429 with Retry-After.
- 500.
- Oversized body.
- Compression bomb simulation.
- Changed revision.
- Duplicate story.
- Prompt-injection content.
- Character-encoding edge case.

Fixtures are sanitized and committed. Live fixture recording is explicit and reviewed.

### 27.4 OpenAI evaluations

Suites:

- Topic classification.
- Lifecycle state.
- Version extraction.
- Breaking-change detection.
- Security-severity handling.
- Claim-to-evidence grounding.
- Unsupported-claim rate.
- Summary factuality.
- Why-care personalization.
- Action recommendation consistency.
- Concision.
- Library incumbent comparison.
- Prompt-injection resistance.
- Executive-overview fallback when AI is unavailable.

Promotion thresholds:

- JSON schema validity: at least 99.9 percent after one retry.
- Material-claim evidence coverage: 100 percent.
- Unsupported material claim rate: below 0.5 percent.
- Priority topic recall: at least 95 percent.
- Irrelevant top-10 rate: below 10 percent on owner-labeled test set.
- Duplicate-cluster precision: at least 98 percent.
- Critical security recall on fixtures: 100 percent.

A model or prompt change cannot promote on aggregate score alone. It must not regress critical safety and evidence metrics.

### 27.5 Search evaluation

Maintain at least 100 labeled queries by production launch:

- Exact technology/version.
- Error-tolerant package name.
- Concept search.
- Breaking changes in time range.
- Security advisories affecting stack.
- New React libraries.
- Go profiling improvements.

Measure Recall@10, NDCG@10, and no-result rate.

### 27.6 Load and resilience

- 10,000-source synthetic schedule.
- 1,000 concurrent due jobs.
- Provider 429 and timeout storm.
- Database failover/restart.
- Worker restart mid-job.
- Duplicate OpenAI webhook.
- Missed webhook reconciliation.
- Browser SSE reconnect storm.
- Digest retry.
- Worker restart at the exact digest due minute.
- Two reconcilers racing for the same schedule.
- Fall-back duplicated local time and spring-forward nonexistent local time.
- Object-storage outage.

Initial personal scale is smaller, but these tests expose unsafe assumptions.

### 27.7 Reading-state model tests

Property tests must generate valid and invalid action sequences and prove:

- Exactly one location exists.
- Star is independent from location and read state.
- Snooze restores the recorded location once.
- Archive does not erase star, tags, notes, highlights, or reading progress.
- Dismiss creates explicit feedback and archive state.
- Mark read never changes relevance weights.
- Already known changes novelty feedback but not source trust.
- Replaying an idempotency key cannot apply a mutation twice.
- Undo restores the exact prior state unless a newer conflicting mutation exists.
- Bulk operations affect exactly the server-side filter snapshot confirmed by the owner.

---

## 28. Release and upgrade policy

### 28.1 Latest stable policy

At PR 0 and every release:

1. Identify the newest stable patch in the chosen supported line.
2. Check official security advisories.
3. Check runtime and peer constraints.
4. Run compatibility tests as one set.
5. Review provenance and release age.
6. Pin exact versions and hashes.
7. Record the decision in the build manifest.

Do not use an unbounded latest specifier in committed production manifests.

### 28.2 Pre-release policy

A pre-release can enter an isolated research branch when:

- The owner explicitly requests evaluation.
- It is not on the production path.
- Data is disposable.
- The comparison documents benefits and regressions.
- Removal is easy.

### 28.3 Security exception

The minimum release-age delay can be waived for a verified security fix after:

- Official advisory verification.
- Maintainer identity/provenance check.
- Targeted tests.
- Full regression suite.
- Attended release approval.

### 28.4 Technology review cadence

- Weekly dependency update report.
- Monthly stack and model review.
- Quarterly architecture review.
- Immediate review after a critical upstream compromise.

---

## 29. Implementation phases

Each phase ends with PASS, EXTEND, or FAIL. Evidence is stored under docs/evidence/{phase}/{date}.

### Phase P0: owner decisions and PR 0

Deliver:

- Final product/repository name.
- Owner GitHub numeric ID.
- Timezone and digest schedule.
- Production domain.
- Contact-bearing crawler identity.
- Discord and optional email decision.
- OpenAI budget.
- Source policy approval.
- Public demo contact/case-study links and approval of its synthetic fixture content.
- Exact stable version audit.
- ADRs 001–010.

PASS:

- No unresolved safety, auth, source-policy, database-image, or deployment decision.
- make doctor specification approved.
- Version manifest cites official releases and security floors.

### Phase P1: repository and local platform

Deliver:

- Repository layout.
- Makefile.
- Compose base and developer overlay.
- Production Dockerfiles.
- PostgreSQL plus pgvector.
- MinIO.
- Fake source, fake OpenAI, and fake delivery services.
- Injected clock and scheduler occurrence ledger.
- Optional local OpenTelemetry LGTM profile.
- Migration, API, worker, and web skeletons.
- Shared platform-neutral api-client, domain, and design-token packages; no shared rendering package.
- CI baseline.

PASS:

- make bootstrap works from a clean machine.
- make dev reaches healthy state.
- make prodlike-smoke passes.
- make config-check and make test-dst pass.
- A local 08:00 time-travel run produces exactly one captured digest.
- Production Docker stages run non-root.
- No live credential is required.

### Phase P2: deterministic source ingestion

Deliver:

- Source registry schema.
- Feed, page, GitHub release, and structured API connectors.
- Checkpoints, conditional requests, backoff, rate limiting, SSRF, robots, object storage.
- Source Health screen.

PASS:

- All source fixtures pass.
- Priority seed sources achieve seven continuous days of target polling in staging.
- No silent item loss.
- 304, 429, revision, and outage behavior verified.

### Phase P3: normalization, dedupe, cluster, and search

Deliver:

- Readability extraction.
- Content revisions.
- Exact, SimHash, and embedding candidate dedupe.
- Story clusters.
- PostgreSQL hybrid search.

PASS:

- Duplicate precision at least 98 percent.
- Search quality thresholds pass.
- Reprocessing is idempotent.
- Revision provenance is intact.

### Phase P4: bounded OpenAI intelligence

Deliver:

- Official SDK integration.
- Fast extraction.
- Evidence validation.
- Research stage.
- Background jobs and webhooks.
- Prompt/version registry.
- Cost ledger and hard cap.
- Evaluation suite.

PASS:

- All critical eval thresholds pass.
- Duplicate webhooks are harmless.
- Hard cost stop works.
- Prompt injection cannot gain tools or authority.
- Every material published claim has evidence.

### Phase P5: complete product UI

Deliver:

- Auth.
- Today, Inbox, Live, Later, Starred, Archive, Snoozed, Story, Releases, Search, Sources, Settings, and Operations.
- Complete reading-state controls, tags, notes, highlights, bulk actions, Undo, manual URL capture, and exports.
- Three-pane desktop, two-pane tablet, single-pane mobile, command palette, shortcut customization, and reader typography.
- Themes, responsive behavior, and keyboard navigation.
- SSE replay.
- Public /demo route, guided walkthrough, static fixture adapter, responsive showcase screens, metadata, snapshot methodology, and security isolation tests.

PASS:

- Playwright critical journeys pass.
- WCAG 2.2 AA checks pass.
- Performance budgets pass.
- Hidden-tab and reconnect tests pass.
- Every reading-state invariant and keyboard triage journey passes.
- /demo renders with all private services unavailable and its anonymous request graph contains no protected request.
- No authenticated route or data-access function relies on Proxy as its only authorization check.

### Phase P6: technology radar

Deliver:

- Weekly discovery.
- Candidate evidence.
- Package metrics.
- Comparison view.
- Owner decisions and review dates.

PASS:

- Ten known good and ten misleading candidate fixtures classify correctly.
- No package can move to Adopt without owner action.
- License, security, stability, and incumbent comparison are visible.

### Phase P7: digests and alerts

Deliver:

- Discord.
- Optional Resend.
- Daily and weekly scheduling.
- Database schedule definitions and occurrence ledger.
- Timezone/DST handling, preflight, cutoff, catch-up, Run now, Preview, Skip next, pause, resume, and missed-run operations.
- Critical security alert path.
- Quiet hours and coalescing.

PASS:

- Idempotent delivery.
- Staging cannot reach production recipients.
- Critical alert fixture arrives within target.
- Daily digest contains no duplicate stories.
- Restarting at the due minute still creates one occurrence and one visible delivery.
- OpenAI failure still delivers the deterministic digest.
- Spring-forward and fall-back schedule tests pass.

### Phase P8: security and operational hardening

Deliver:

- Full security workflows.
- Demo/auth route-normalization and forbidden-import tests.
- Restore automation.
- Runbooks.
- Metrics and alerts.
- Retention.
- SBOM and provenance.

PASS:

- Threat-model review complete.
- No high/critical unaccepted vulnerability.
- Restore drill meets RPO/RTO.
- Secret rotation drill succeeds.
- Incident exercises completed.

### Phase P9: Railway staging soak

Duration: minimum 14 consecutive days.

Required:

- All Priority 0 sources active.
- Real read-only APIs.
- Low-cap OpenAI project.
- Staging-only delivery.
- At least two simulated provider outages.
- At least one redeploy during backlog.
- At least one redeploy across a scheduled digest window.
- One simulated missed morning run followed by catch-up.
- At least one database restore rehearsal.
- Anonymous demo uptime, performance, accessibility, and isolation monitoring.

PASS:

- Freshness SLO met.
- No duplicate alerts.
- No unsupported material claims in reviewed sample.
- Owner marks at least 80 percent of top-10 items useful or already known.
- Costs remain within budget model.
- Queue recovers from outages.

EXTEND:

- Insufficient item count or one non-critical SLO miss with a clear fix.

FAIL:

- Security boundary failure.
- Lost evidence.
- Unbounded provider cost.
- Repeated critical miss.

### Phase P10: production launch

Deliver:

- Production OAuth app.
- Production Railway environment.
- Sealed variables.
- Domain and TLS.
- Manual release.
- Delivery endpoints.
- Backups and alerts.

Launch:

1. Freeze staging release SHA.
2. Complete release evidence.
3. Merge release PR staging to master.
4. Verify backup.
5. Deploy migrate.
6. Deploy api and worker.
7. Deploy web.
8. Run smoke tests.
9. Enable ingestion gradually: 10 percent, 50 percent, 100 percent source groups.
10. Enable OpenAI processing.
11. Enable digest after one full successful window.
12. Observe for 24 hours.
13. Verify the public demo from a signed-out browser with the API deliberately unavailable.

Rollback:

- Disable worker intake.
- Roll back application images to prior SHA.
- Keep forward-compatible migration.
- Restore only when data corruption is proven.
- Reprocess durable jobs after recovery.

### Phase P11: repository-aware intelligence

This is deferred and independently approved.

Potential read-only capabilities:

- Import dependency manifests and SBOMs from owner repositories.
- Map news directly to affected repositories.
- Generate repository-specific migration plans.
- Draft upgrade PRs with human review.

New security review is mandatory before private repository access.

---

## 30. Pull request sequence

1. PR 0: decisions, ADRs, version manifest, repository policy, Make/Compose skeleton.
2. PR 1: database image, migrations, pgvector validation, sqlc.
3. PR 2: config, telemetry, Huma/chi API skeleton, generated client.
4. PR 3: source registry and fixtures.
5. PR 4: fetcher, SSRF, checkpoints, object storage.
6. PR 5: parser, revisions, normalization.
7. PR 6: River queues, database schedule reconciler, occurrence ledger, injected clock, and retry operations.
8. PR 7: deterministic dedupe and clusters.
9. PR 8: embeddings and hybrid search.
10. PR 9: OpenAI structured extraction and eval harness.
11. PR 10: research synthesis, web search, background mode, webhooks.
12. PR 11: auth and application shell.
13. PR 12: Today, Live, Story, responsive application layout, shared design tokens, and the isolated public demo adapter/route.
14. PR 13: Inbox, Later, Starred, Archive, Snoozed, state mutations, bulk actions, Undo, tags, notes, and highlights.
15. PR 14: Releases, Search, manual URL capture, OPML, and exports.
16. PR 15: Sources, Settings, schedule controls, and Operations.
17. PR 16: Radar discovery and assessments.
18. PR 17: digest preparation, Discord, Resend, delivery capture, and catch-up behavior.
19. PR 18: security workflows, backups, runbooks.
20. PR 19: Railway staging and parity verification.
21. PR 20: staging soak fixes and production release evidence.

Every PR:

- Updates tests.
- Updates relevant ADR/runbook.
- Includes generated files.
- Passes make prepush.
- Has screenshots for UI changes.
- Has migration and rollback notes.
- Lists new variables and permissions.

---

## 31. Architecture decision records

Create:

- ADR-001: single-owner private product.
- ADR-002: explicit source registry instead of impossible whole-web promise.
- ADR-003: Go service and worker.
- ADR-004: PostgreSQL, pgvector, and River.
- ADR-005: Huma/chi OpenAPI 3.1 contract.
- ADR-006: Next.js BFF authentication boundary.
- ADR-007: bounded OpenAI pipeline.
- ADR-008: evidence and claim provenance.
- ADR-009: PostgreSQL hybrid search.
- ADR-010: fetch-stream SSE and durable outbox.
- ADR-011: GitHub staging/master release flow.
- ADR-012: Railway staging/production isolation.
- ADR-013: Make and Compose local parity.
- ADR-014: private Discord delivery.
- ADR-015: latest stable and exact pinning policy.
- ADR-016: no autonomous upgrades or code execution.
- ADR-017: continuous ingestion plus database-backed local-time digest scheduling.
- ADR-018: independent reading location, read, star, snooze, and relevance states.
- ADR-019: command-driven triage, Undo, and server-backed bulk mutations.
- ADR-020: static fixture-only public employer demo with no anonymous data API.
- ADR-021: post-Version 1 Expo/React Native Android client with shared non-visual TypeScript packages.
- ADR-022: Better Auth OAuth 2.1 public native client with S256 PKCE and verified App Links.
- ADR-023: signed APK and AAB distribution through immutable GitHub Releases.

---

## 32. Runbooks

Create:

- source-failing.md
- source-parser-drift.md
- source-policy-takedown.md
- queue-backlog.md
- openai-outage.md
- openai-budget-exhausted.md
- webhook-reconciliation.md
- duplicate-alert.md
- scheduler-stalled.md
- digest-missed-or-late.md
- digest-duplicate-prevention.md
- snoozed-item-not-returned.md
- bulk-state-mutation-recovery.md
- bad-summary-or-unsupported-claim.md
- security-advisory-missed.md
- database-unavailable.md
- restore-postgres.md
- object-storage-unavailable.md
- rotate-secrets.md
- rollback-release.md
- compromised-dependency.md
- disable-all-external-fetching.md
- disable-delivery.md
- demo-private-data-exposure.md
- mobile-device-or-token-revocation.md
- android-signing-key-loss-or-compromise.md
- mobile-push-delivery-failure.md

Every runbook contains:

- Trigger.
- Impact.
- Immediate containment.
- Exact verification commands.
- Recovery.
- Data-integrity checks.
- Communication.
- Post-incident evidence.

---

## 33. Acceptance tests

The complete platform is accepted only when all are true:

1. A clean machine can run make bootstrap and make dev.
2. make prodlike uses the same runtime stages Railway uses.
3. A Go release feed item is ingested with first-seen evidence.
4. A changed release note creates a revision without losing the prior evidence.
5. Three articles about one release form one story.
6. A stable release is not inferred from a beta announcement.
7. A community claim cannot publish without primary evidence.
8. A prompt injection inside an article cannot gain tools.
9. A malformed model response cannot publish.
10. A claim with an invalid evidence span cannot publish.
11. A duplicate OpenAI webhook produces one state transition.
12. The monthly hard cap stops new AI work.
13. Deterministic ingestion continues during the AI stop.
14. Search returns exact package and semantic concept results.
15. SSE resumes after disconnect without missing or duplicating durable events.
16. An expired cursor produces reset_required and a safe refetch.
17. Discord sends one idempotent digest.
18. Staging cannot send to the production webhook.
19. A confirmed critical watched-dependency advisory bypasses quiet hours.
20. An unconfirmed community rumor does not.
21. An owner can inspect claim-to-source evidence.
22. An owner can correct relevance and reset learned weights.
23. No model can move a package to Adopt.
24. No model can install or execute code.
25. GitHub release PR validation rejects a non-staging source.
26. Railway production cannot auto-deploy from a branch push.
27. Production services use private database/API networking.
28. A database restore meets the current RPO/RTO.
29. WCAG and Core Web Vitals budgets pass.
30. The 14-day staging soak passes.
31. Continuous ingestion continues before, during, and after the morning digest.
32. A worker restart at 07:59 creates one 08:00 occurrence and one visible delivery.
33. A missed 08:00 run catches up once within six hours and becomes missed after the grace window.
34. Daylight-saving tests produce exactly one intended local-date digest.
35. An OpenAI outage still produces the deterministic ranked digest.
36. Preview and Run now cannot accidentally deliver externally in the safe local profile.
37. Read, Later, star, archive, snooze, dismiss, and already-known states follow their independent contracts.
38. A snoozed item returns to its prior location and becomes unread once.
39. Bulk mutation, optimistic conflict, rollback, and Undo tests pass.
40. Keyboard-only triage and the command palette pass accessibility tests.
41. Starred archived items remain in Starred.
42. Notes and highlights retain revision provenance and visibly report orphaned spans.
43. Manual URL capture passes the same SSRF and content-policy pipeline.
44. OPML import previews changes and never silently enables an invalid source.
45. make config-check proves typed configuration, Compose, Docker, env examples, and Railway parity.
46. /demo renders from a signed-out browser while PostgreSQL, the private API, worker, Better Auth provider, OpenAI, and delivery providers are unavailable.
47. The complete /demo request graph contains no /api/v1, /api/auth, SSE, OpenAI, Discord, Resend, or object-storage request.
48. A logged-in owner and an anonymous visitor receive semantically equivalent, session-independent demo content for the same build; per-request CSP nonce bytes may differ.
49. Unknown fixture IDs and route-normalization attacks cannot cross from /demo into a protected route.
50. The emitted demo HTML, React payload, JavaScript, and public assets contain no forbidden secret or owner-data fixture.
51. Demo read/Later/star/archive interactions remain local, reset cleanly, and cannot create an audit or outbox record.
52. The guided demo passes keyboard-only, screen-reader, reduced-motion, contrast, and responsive-layout tests.
53. The public demo meets its 180 KiB JavaScript and 2.0-second p75 LCP budgets.

---

## 34. Owner setup checklist

Required before implementation:

- Choose product and repository name.
- Confirm America/New_York or replace timezone.
- Provide a crawler contact email or URL.
- Choose domain.
- Confirm daily and weekly digest times.
- Confirm digest weekdays, weekend mode, maximum item count, catch-up grace, empty-digest behavior, quiet hours, and whether critical alerts may bypass them.
- Confirm default mark-read behavior: after two seconds, manual-only, or scroll-complete.
- Create private GitHub repository.
- Record numeric GitHub owner ID.
- Create GitHub OAuth apps for local, staging, and production.
- Create GitHub App or read-only token for metadata ingestion.
- Create separate OpenAI staging and production projects.
- Set staging and production spend limits.
- Create private Discord channels and webhooks.
- Decide whether to enable Resend.
- Create separate staging and production delivery recipients; never test against the production Discord channel.
- Create Railway project.
- Choose staging and production regions.
- Select PostgreSQL plus pgvector image/template.
- Create separate staging and production buckets.
- Select OTLP destination or accept Railway-only baseline.
- Approve source content and retention policy.
- Approve the exact /demo fixture snapshot, snapshot date, contact URL, case-study URL if any, and noindex policy.
- Decide whether a separate sanitized public case-study repository will exist; never expose the private implementation repository by accident.

Deferred until Android Phase A0:

- Choose the final reverse-DNS Android application ID after the product name and domain are cleared.
- Create separate staging and production Firebase Android apps if push is enabled.
- Create and offline-back up the Android app-signing keystore before the first installable production release.
- Confirm whether GitHub-only sideloading remains the distribution method or a future Play internal-test track is also desired.

Do not place any secret values in this specification or the repository.

---

## 35. PR 0 instructions for the implementation agent

The implementation agent must:

1. Recheck every baseline version against official release and security sources on that day.
2. Produce docs/version-manifest.md with selected exact versions, dates, URLs, hashes, and compatibility notes.
3. Resolve the current PostgreSQL pgvector Railway image and pin its digest.
4. Resolve current official GitHub Action releases and pin full commit SHAs.
5. Create the repository layout exactly once.
6. Add Makefile, compose.yaml, and compose.dev.yaml before application features.
7. Implement make doctor, secrets, bootstrap, dev, prepush, prodlike, and reset safety first.
8. Build minimal web, api, worker, migrate, postgres, minio, fake-source, fake-openai, and fake-delivery services.
9. Prove local and Railway Docker-stage parity with deploy/railway/parity.yaml.
10. Implement the injectable clock, schedule definition/occurrence migrations, safe local time travel, and production fixed-clock fuse before any real digest job.
11. Add CI that fails on generated drift, MUI, ESLint, Prettier, floating Actions tags, missing lockfile, unsafe NEXT_PUBLIC names, or Compose/Railway configuration drift.
12. Add packages/api-client, packages/domain, and packages/design-tokens with platform-neutral import rules; do not scaffold apps/mobile yet.
13. Add /demo to the exact public-route matrix and specify the forbidden import graph before building the UI.
14. Stop after PR 0 evidence. Do not begin ingestion until the owner approves the version and infrastructure manifest.

---

## 36. Research findings behind the baseline

The build agent must reopen these sources because time-sensitive version facts can change.

### OpenAI

- Model selection: https://developers.openai.com/api/docs/guides/model-selection
- Models: https://developers.openai.com/api/docs/models
- Responses API: https://developers.openai.com/api/docs/guides/migrate-to-responses
- Structured Outputs: https://developers.openai.com/api/docs/guides/structured-outputs
- Web search: https://developers.openai.com/api/docs/guides/tools-web-search
- Background mode: https://developers.openai.com/api/docs/guides/background
- Webhooks: https://developers.openai.com/api/docs/guides/webhooks
- Batch: https://developers.openai.com/api/docs/guides/batch
- Prompt caching: https://developers.openai.com/api/docs/guides/prompt-caching
- Pricing: https://openai.com/api/pricing/

Important findings:

- Responses API supports the tool and structured-output design needed here.
- Web search can use domain filters and return a complete source list.
- Background responses require webhook reconciliation and have documented retention behavior.
- Webhooks can be duplicated and must be verified and deduplicated.
- Batch provides a lower-cost asynchronous lane for non-urgent work.
- Prompt caching benefits from a stable common prefix.

### Runtime and frontend

- Go releases: https://go.dev/doc/devel/release
- Go 1.27 notes: https://go.dev/doc/go1.27
- Node releases: https://nodejs.org/en/about/previous-releases
- React releases: https://react.dev/versions
- Next.js blog and security: https://nextjs.org/blog
- TypeScript blog: https://devblogs.microsoft.com/typescript/
- pnpm releases: https://github.com/pnpm/pnpm/releases
- Biome releases: https://biomejs.dev/blog/
- shadcn changelog: https://ui.shadcn.com/docs/changelog
- TanStack releases: https://tanstack.com/blog

Important findings:

- Go 1.27 is stable.
- Node 26.8.1 is the owner-selected production line under an explicit Current-line and release-age exception; keep every production input exactly pinned.
- TypeScript 7 is stable.
- Next.js 16.3.3 is the security floor identified during this audit; recheck for later patches.
- pnpm 12 and Biome 2.5 are current stable baselines.
- TanStack Form 2 is still pre-release, so Form 1 remains the production choice.

### Ingestion and ecosystem

- GitHub REST rate limits: https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api
- GitHub releases API: https://docs.github.com/rest/releases/releases
- npm registry API: https://github.com/npm/registry/blob/main/docs/REGISTRY-API.md
- Go module index: https://index.golang.org/
- deps.dev API: https://docs.deps.dev/api/v3/
- OSV API: https://google.github.io/osv.dev/api/
- OpenSSF Scorecard: https://securityscorecards.dev/
- Hacker News API: https://github.com/HackerNews/API
- robots standard: https://www.rfc-editor.org/rfc/rfc9309
- gofeed: https://github.com/mmcdole/gofeed
- readeck readability: https://codeberg.org/readeck/go-readability

Important findings:

- Authenticated GitHub API use provides a materially larger rate limit than anonymous use.
- Conditional requests reduce bandwidth and can protect rate budgets.
- The Go module index is an ordered discovery feed, not a curated relevance source.
- deps.dev v3 is the stable metadata API; experimental APIs are excluded.
- Scorecard checks are evidence inputs, not a single decision score.
- The former go-shiori readability repository is archived; use the maintained readeck successor.

### Data and jobs

- PostgreSQL: https://www.postgresql.org/docs/release/
- pgvector: https://github.com/pgvector/pgvector
- River: https://riverqueue.com/docs
- River periodic jobs: https://riverqueue.com/docs/periodic-jobs
- sqlc: https://sqlc.dev/
- Huma OpenAPI: https://huma.rocks/features/openapi-generation/

Important findings:

- PostgreSQL 18 is stable and 19 is still pre-release at the baseline date.
- PostgreSQL 18 supplies uuidv7().
- pgvector supports exact and approximate vector search.
- River provides PostgreSQL-native jobs, retries, uniqueness, transactions, periodic scheduling, and OpenTelemetry integration.
- River OSS periodic schedule state is memory-backed and can skip an instant across leader changes; durable periodic jobs are a Pro feature. Version 1 therefore keeps schedule definitions and occurrences in application tables and uses River only to wake the reconciler and execute typed jobs.
- Huma v2 generates OpenAPI 3.1 and can use chi.
- sqlc generates type-safe Go from reviewed SQL.

### Railway, GitHub, and Docker

- Railway environments: https://docs.railway.com/guides/isolate-staging-production
- Railway variables: https://docs.railway.com/variables
- Railway GitHub autodeploy: https://docs.railway.com/deployments/github-autodeploys
- Railway Dockerfiles: https://docs.railway.com/builds/dockerfiles
- Railway private networking: https://docs.railway.com/reference/private-networking
- Railway backups: https://docs.railway.com/reference/backups
- Railway cron: https://docs.railway.com/cron-jobs
- Docker Compose Watch: https://docs.docker.com/compose/how-tos/file-watch/
- Compose merge: https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/
- Compose startup order: https://docs.docker.com/compose/how-tos/startup-order/
- Compose secrets: https://docs.docker.com/compose/how-tos/use-secrets/
- Docker Compose profiles: https://docs.docker.com/compose/how-tos/profiles/
- Grafana Docker OpenTelemetry LGTM: https://grafana.com/docs/opentelemetry/docker-lgtm/
- GitHub secure Actions use: https://docs.github.com/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions

Important findings:

- Railway environments isolate resources and variables; sealed secrets require deliberate handling.
- Railway can build service-specific Dockerfiles from a shared monorepo.
- Private networking should carry application-to-application and database traffic.
- Railway cron uses UTC, has a five-minute minimum interval, may vary by a few minutes, and skips a new execution when the prior one is still active. It is not the primary digest scheduler.
- Compose Watch supports the fast local overlay while production Docker stages provide deployment rehearsal.
- The pinned Grafana OpenTelemetry LGTM image is appropriate for an optional development/test observability profile, not the production backend.
- GitHub recommends pinning third-party Actions to full commit SHAs.

### Reading workflow and interaction research

- Readwise Reader triage workflow: https://docs.readwise.io/reader/guides/workflows/library-configuration
- Readwise command palette and keyboard behavior: https://docs.readwise.io/reader/docs/faqs/navigation
- Readwise notes and highlights: https://docs.readwise.io/reader/docs/faqs/highlights-tags-notes
- Readwise export: https://docs.readwise.io/reader/docs/faqs/exporting
- Feedly keyboard shortcuts: https://docs.feedly.com/article/81-what-are-the-keyboard-shortcuts
- Inoreader read-later workflow: https://www.inoreader.com/blog/2025/06/use-inoreader-as-your-ultimate-read-later-app.html
- Inoreader 2026 compact navigation: https://www.inoreader.com/blog/2026/05/a-cleaner-top-bar-and-quicker-access-to-key-features.html

Important findings:

- Productive reading tools separate inbox, Later, archive, read state, favorites, tags, notes, and highlights rather than overloading one saved flag.
- Keyboard navigation, a command palette, bulk actions, Undo, stable controls, saved filters, and Markdown/OPML export materially reduce high-volume triage friction.
- The platform adopts those interaction principles while keeping its own source evidence, technology-radar, and relevance-feedback model.

### Public demo and Android application research

- Next.js public pages: https://nextjs.org/docs/app/guides/public-static-pages
- Next.js authentication: https://nextjs.org/docs/app/guides/authentication
- Next.js metadata and Open Graph images: https://nextjs.org/docs/app/getting-started/metadata-and-og-images
- Expo SDK versions: https://docs.expo.dev/versions/latest/
- Expo SDK 57 release: https://expo.dev/changelog/sdk-57
- Expo Router: https://docs.expo.dev/router/introduction/
- Expo monorepos: https://docs.expo.dev/guides/monorepos/
- Expo Continuous Native Generation: https://docs.expo.dev/workflow/continuous-native-generation/
- Expo local Android development: https://docs.expo.dev/guides/local-app-development/
- Expo APK builds: https://docs.expo.dev/build-reference/apk/
- Expo SecureStore: https://docs.expo.dev/versions/latest/sdk/securestore/
- Expo SQLite: https://docs.expo.dev/versions/latest/sdk/sqlite/
- Expo background tasks: https://docs.expo.dev/versions/latest/sdk/background-task/
- Expo direct FCM notifications: https://docs.expo.dev/push-notifications/sending-notifications-custom/
- Better Auth OAuth 2.1 provider: https://better-auth.com/docs/plugins/oauth-provider
- Android App Links: https://developer.android.com/training/app-links/about
- Android window-size classes: https://developer.android.com/develop/adaptive-apps/guides/use-window-size-classes
- Android canonical layouts: https://developer.android.com/develop/adaptive-apps/guides/canonical-layouts
- Android 16 behavior changes: https://developer.android.com/about/versions/16/behavior-changes-16
- Android 16 KB page-size compatibility: https://developer.android.com/guide/practices/page-sizes
- Android app signing: https://developer.android.com/studio/publish/app-signing
- GitHub release assets: https://docs.github.com/en/repositories/releasing-projects-on-github/managing-releases-in-a-repository
- GitHub artifact attestations: https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations

Important findings:

- Next.js Proxy is appropriate for optimistic route checks but is not a complete authorization layer; protected data access still needs server-side checks close to the data source.
- The demo should be a prerendered public page with static fixtures, not a privileged demo account or anonymous API.
- As of this audit, Expo SDK 57.0.17 uses React Native 0.86.3 and React 19.2.3 and fixes SDK 56/early-57 Hermes memory and development-startup regressions. Recheck the stable line before Android Phase A0.
- Expo supports pnpm workspaces and automatically configures Metro for modern monorepos. SDK 55 and later also align Metro and native autolinking resolution automatically.
- Expo SDK 57 targets and compiles against Android API 36 and supports Android 7 and later. Android 17/API 37 is still a preview at this audit and is test-only.
- API 36 enforces edge-to-edge behavior and predictive-back changes on Android 16. On large screens it also ignores orientation, aspect-ratio, and resizability restrictions, so a phone-only portrait layout is not a tablet strategy.
- React Native applications carry native libraries through their runtime and modules. Release artifacts must pass Android's 16 KB zip and ELF alignment checks rather than assuming the framework makes every dependency compatible.
- A universal APK can be installed directly on the owner's phone and tablet; an AAB is retained for a later Play Store track and cannot be directly installed as-is.
- APK updates require a matching signing certificate. Losing a self-managed signing key prevents seamless updates, so the first production key and its recovery process are launch-critical.
- Verified HTTPS App Links bind the web domain to the signing certificate and avoid an interceptable custom-scheme OAuth callback.
- Better Auth's OAuth 2.1 provider supports native public clients, S256 PKCE, exact redirect URIs, resource-bound tokens, refresh tokens, revocation, and no embedded client secret.
- Expo SecureStore encrypts Android values with the Android Keystore. Access and refresh tokens never belong in AsyncStorage, SQLite, logs, crash reports, or application state snapshots.
- Android and Expo background work is deferrable and cannot guarantee an exact morning time. The Railway worker remains the schedule authority; FCM notifies the device and background work performs opportunistic synchronization only.
- React Native/Expo reuses the existing TypeScript expertise and non-visual packages while delivering native navigation, storage, notifications, and gestures. A WebView, Trusted Web Activity, or Capacitor wrapper would maximize superficial UI reuse at the cost of native behavior, offline reliability, and security clarity.

---

## 37. Final authority and conflict rules

This section supersedes conflicting earlier wording inside this document.

1. Safety, evidence, legal, owner, and secret-isolation constraints are permanent until explicitly reviewed.
2. MUI remains prohibited by owner decision.
3. Latest means latest stable, security-patched, compatible, evaluated, and then exactly pinned.
4. Pre-release software is research-only unless the owner approves a documented exception.
5. Deterministic ingestion owns coverage and operations.
6. OpenAI owns bounded extraction and synthesis, not deployment or package authority.
7. Primary sources are required for material release and security claims.
8. Local Compose uses the same Dockerfile runtime stages as Railway.
9. staging is the integration/default branch; master is production-only.
10. Railway staging autodeploys after CI; production deployment is attended.
11. Only web is public.
12. Production and staging credentials, databases, buckets, OAuth apps, OpenAI projects, and delivery endpoints are isolated.
13. PostgreSQL plus pgvector and River are the Version 1 data/queue baseline.
14. Additional infrastructure is introduced only after its measurable trigger is met.
15. No AI-generated recommendation can automatically install, execute, merge, deploy, or publish externally.
16. Source ingestion runs continuously; the morning job assembles already-prepared intelligence rather than starting a whole-web crawl.
17. The database schedule definition and occurrence ledger are the durable timing authority; Railway cron and River's in-memory periodic schedule are not.
18. Reading location, read state, star, snooze, and relevance feedback remain independent.
19. Every external digest is immutable once delivery begins and retries reuse the same payload and idempotency key.
20. /demo is static fixture presentation, never an anonymous identity or data API.
21. The post-Version 1 Android client shares the backend, account, contracts, domain semantics, and tokens—not DOM components or server-only modules.
22. A native app is a public OAuth client: no GitHub, Better Auth, internal API, signing, FCM-server, or provider secret is embedded in the APK.
23. The server remains the authority for schedules, digests, ranking, and mutations; mobile background execution is only opportunistic sync.

---

## 38. Definition of done

The specification is implemented when:

- Every Version 1 capability exists.
- All acceptance tests pass.
- The 14-day Railway staging soak passes.
- Production deployment and rollback evidence exists.
- Restore evidence meets current RPO/RTO.
- Source coverage is measurable.
- Every material factual claim is traceable to evidence.
- Owner feedback demonstrates useful prioritization.
- Costs remain bounded.
- Security and accessibility gates pass.
- The timezone-aware morning schedule, missed-run recovery, and DST tests pass.
- Inbox, Later, Starred, Archive, Snoozed, tags, notes, highlights, bulk actions, Undo, keyboard controls, and exports pass their state contracts.
- Local time travel and fake delivery can verify a complete morning cycle without a live provider or deployment.
- The owner can operate the system using the documented Make targets, GitHub workflows, Railway screens, and runbooks without undocumented knowledge.
- The anonymous employer demo passes its fixture, isolation, auth-boundary, accessibility, performance, and signed-out availability gates.

The Android application is deliberately excluded from Version 1 definition of done. It has an independent phase and acceptance gate in Section 39 and cannot delay the initial web launch.

---

## 39. Post-Version 1 Android application

This is a committed follow-up track, not part of Version 1 launch scope. Begin it only after Phase P10 is complete, the production API contract is stable, and no unresolved severity-one or severity-two incident remains.

### 39.1 Product and architecture decision

Build a native Android client with React Native and Expo in apps/mobile. It uses:

- The same GitHub-authenticated owner identity.
- The same PostgreSQL records and private Go business logic.
- The same /api/v1 contract through the public Next.js authorization/resource BFF.
- The same story IDs, read state, Later order, stars, archives, snoozes, tags, notes, highlights, schedules, digests, and mutation idempotency keys.
- The same semantic design tokens and pure TypeScript domain commands.

It does not reuse:

- React Server Components.
- DOM components.
- shadcn/ui or Radix components.
- Tailwind CSS classes.
- Next.js route code.
- Browser cookies as a native session mechanism.
- Server-only modules or environment access.

The result is the same product and data, not a brittle attempt to run the same rendering code everywhere.

Do not ship a WebView shell, Trusted Web Activity, or Capacitor wrapper as the production Android client. Those approaches maximize superficial reuse but weaken offline behavior, token boundaries, notifications, adaptive navigation, and native testing. Keep the responsive web application available in a mobile browser as a fallback.

Expo is the default over a Kotlin-only Jetpack Compose app because this project already has TypeScript, React, pnpm, TanStack Query, Zustand, shared generated contracts, and a compatible monorepo. Re-evaluate Kotlin Compose only if a release-build benchmark proves that Expo cannot meet an explicit adaptive-layout, accessibility, background, startup, memory, or rendering requirement after a bounded optimization attempt.

### 39.2 Android technology baseline

At Android Phase A0, re-run the latest-stable compatibility policy and pin the selected set exactly. The audited 2026-08-29 starting point is:

| Capability | Audited baseline | Rule |
|---|---|---|
| Expo | 57.0.17 or newer stable patch in SDK 57 | Earlier SDK 57 patches had resolved Hermes regressions; recheck before bootstrap |
| React Native | 0.86.3 through the Expo SDK | Do not override Expo's supported version |
| React | 19.2.3 | Keep one compatible React line across the workspace |
| TypeScript | 7 stable | Share the root strict baseline |
| Node | 26.8.1 compatible patch | Owner-selected production line; satisfies Expo's Node 22.13 minimum and matches the web toolchain |
| pnpm | 12 stable | One root workspace and lockfile |
| Expo Router | SDK 57 compatible ~57.0.17 line | Typed file routes; exact resolved package pin |
| React Native architecture | New Architecture with Hermes | Legacy Architecture is not supported by current Expo |
| TanStack Query | 5 | Remote server state and foreground synchronization |
| Zustand | 5 | Ephemeral navigation, selection, filters, and UI state only |
| FlashList | 2.0.2 or newer compatible stable patch | Long story and release lists; New Architecture required |
| Reanimated | 4.5.1 SDK-compatible line | Short native-thread transitions only |
| Gesture Handler | 2.32 SDK-compatible line | Swipe/gesture behavior with visible button equivalents |
| SecureStore | SDK 57 compatible line | Refresh token and local database key only |
| Expo SQLite with SQLCipher | SDK 57 compatible line | Encrypted cache and mutation outbox |
| Expo Notifications | SDK 57 compatible line | Native FCM token and notification handling |
| Expo Background Task | SDK 57 compatible line | Opportunistic sync, never exact scheduling |
| expo/fetch plus eventsource-parser | SDK 57 compatible line plus the web parser version | Foreground streaming SSE |
| jest-expo and React Native Testing Library | SDK 57 compatible line | Unit and component tests |
| Maestro | current stable compatible release | Black-box Android device journeys |
| Android compile/target SDK | API 36 | API 37 remains preview at this audit |
| Minimum Android | Android 7 / API 24 | Raise only with an owner-approved device-coverage reason |
| Java | Temurin 17 | Match the selected Android/Expo build image |

Use expo install for SDK-coupled packages, then commit the exact root lockfile. expo-doctor, React Native Directory compatibility, changelogs, security advisories, native build tests, and the device suite must all pass as one set. Do not manually force a newer React Native, Gradle, Android Gradle Plugin, Kotlin, or native module beyond Expo's supported matrix merely because a standalone release exists.

Do not use an Expo, React Native, Router, native-module, Android SDK, or Gradle preview in production. A preview may run in a separate research workflow with no signing secrets.

### 39.3 Shared-code boundary

packages/api-client contains:

- Generated request/response types.
- Endpoint builders.
- Problem-details parsing.
- Cursor and ETag helpers.
- Runtime schema validation where needed at an untrusted boundary.

packages/domain contains only platform-neutral code:

- Story and release view models derived from API records.
- Reading-state commands and invariants.
- Sorting, grouping, relative-time, version, and source-tier formatters.
- Idempotency-key and optimistic-mutation helpers.
- Schedule-display calculations that do not become the schedule authority.

packages/design-tokens contains:

- Semantic color roles in light and dark themes.
- Typography scale names and numeric values.
- Spacing, radius, density, and motion-duration tokens.
- Chart and status palettes.

Platform adapters map the tokens to CSS/OKLCH on web and React Native colors/styles on Android. Do not share component implementations, persistence implementations, route modules, auth storage, or transport setup.

Enforce boundaries with package exports and CI:

- Shared packages cannot import next, react-dom, Node built-ins, server-only, Radix, shadcn/ui, Tailwind, Expo, react-native, or native modules.
- apps/web cannot import apps/mobile.
- apps/mobile cannot import apps/web, internal Go code, server configuration, or raw generated secrets.
- The generated API client must compile in both web and React Native test projects.

### 39.4 Runtime topology

~~~text
Android phone or tablet
  |-- SecureStore: refresh token and SQLCipher key
  |-- SQLCipher SQLite: cached briefs, state, cursor, mutation outbox
  |-- foreground HTTPS and SSE
  |-- FCM notifications
  v
Public Next.js OAuth/resource BFF
  |-- validates native access token, audience, owner, client, and version
  |-- rate limits and records request/device context
  v
Private Go API -> PostgreSQL, object metadata, durable outbox
~~~

The Android app never calls the Railway private API hostname. The BFF verifies the native token, converts it to the same internal owner identity used by browser requests, and calls the Go API with the rotated internal service credential.

The Go service remains the only writer of business records. Mobile SQLite is a disposable encrypted replica and offline outbox, not a second authority.

### 39.5 Screens and capability scope

The first Android release includes:

- Sign in and device registration.
- Today and morning digest.
- Inbox.
- Live while foregrounded.
- Read Later with manual ordering.
- Starred.
- Archive.
- Snoozed.
- Story detail with evidence, related sources, examples, and reading progress.
- Releases and Coming Soon.
- Technology Radar.
- Search.
- Read/unread, Later, star, archive, snooze, tags, notes, highlights, feedback, Undo, and safe offline mutations.
- Digest schedule summary and notification preferences.
- Read-only source-health and operations summary.
- Settings, app lock, device/session management, sync status, app version, build SHA, and release link.
- Verified App Links from web, Discord, email, and notifications into a story or digest.
- Android share target for sending a URL through the existing server-side import/SSRF pipeline after the core app passes.

The first Android release excludes:

- Source-registry editing.
- OAuth/provider administration.
- OpenAI budget or prompt administration.
- Restore, queue retry, secret rotation, or other dangerous operations.
- A full operational console.
- Arbitrary file import.
- Exact-time device scheduling.
- Widgets.
- Android Auto, TV, Wear OS, or XR variants.
- Google Play public distribution.
- Over-the-air JavaScript updates.

Dangerous or infrequent administration remains on the authenticated web app and can be opened through an explicit Open web administration action.

### 39.6 Adaptive phone, foldable, and tablet design

Classify the current app window, not the physical device or orientation:

| Width | Navigation | Content |
|---|---|---|
| Compact, below 600 dp | Bottom tabs plus stack navigation | One pane; list or detail |
| Medium, 600–839 dp | Navigation rail | One wide pane or narrow list plus detail when the content remains readable |
| Expanded, 840–1199 dp | Persistent navigation rail | Canonical list-detail with resizable reading pane |
| Large, 1200–1599 dp | Rail plus optional labels | List-detail plus supporting evidence pane when useful |
| Extra-large, 1600 dp and above | Persistent navigation | Maximum three panes; content width remains bounded |

The width class can change during split screen, rotation, desktop windowing, folding, and unfolding. Hoist current layout class and selected item so a transition does not lose the list position, selected story, draft note, or reading progress.

Phone behavior:

- Today, Inbox, Later, Starred, and Search occupy the five bottom destinations.
- More contains Releases, Radar, Archive, Snoozed, Sources, Settings, and Operations.
- Story detail is full-screen with a stable bottom action bar.
- Swipe actions are optional accelerators and always have visible accessible equivalents.
- Back follows Android predictive-back behavior and never discards an unsaved note without warning.

Tablet and unfolded behavior:

- The left pane is a virtualized list with filters and unread count.
- The center pane is the story reader.
- The optional right pane holds claim evidence, related sources, notes, or radar comparison.
- Pane widths are based on available space; do not stretch prose beyond the configured readable line length.
- Hardware keyboard arrows, J/K, Enter, M, L, S, E, Z, and search shortcuts match the web commands where Android conventions permit.

Visual rules:

- Preserve Night Index and Day Index semantic roles but use native Android surfaces, elevation, ripple, system bars, keyboard insets, and touch behavior.
- Use tabular numerals for versions, dates, scores, and costs.
- Minimum touch target is 48 dp.
- Transitions are 80–220 ms and honor reduced motion.
- Haptics are restrained to completed triage, pull-to-refresh completion, and destructive confirmation; provide a disable setting.
- Story headlines, status, source, action, and reading-state controls remain stable during synchronization.
- Never show skeletons after cached content is available; show a subtle sync state instead.
- Render edge to edge with correct status-bar, navigation-bar, display-cutout, gesture-navigation, keyboard, and fold/hinge insets. Do not use an edge-to-edge opt-out.
- Do not lock orientation, aspect ratio, or resizability. The UI must remain valid when API 36 ignores those restrictions on large screens.

Accessibility gates:

- TalkBack labels and traversal order.
- Font scaling through 200 percent without clipped controls.
- Light/dark contrast and no color-only state.
- Switch Access and keyboard navigation.
- Reduced motion.
- Logical focus after navigation, Undo, errors, and pane changes.
- Visible alternatives for every gesture.
- Accessibility Scanner review on compact and expanded layouts.

### 39.7 Authentication and session lifecycle

Enable Better Auth's OAuth 2.1 provider only when Android Phase A1 starts. Register one administrator-created first-party native public client per environment:

- application_type: native.
- token_endpoint_auth_method: none.
- response_types: code.
- grant_types: authorization_code and refresh_token.
- require_pkce: true.
- exact verified HTTPS redirect URI.
- scopes: openid, profile, offline_access, and the minimum product scopes.
- resource/audience: the product API only.
- skip consent only for the reviewed first-party client.

Sign-in sequence:

1. The app generates a high-entropy state, nonce, and PKCE verifier.
2. It derives an S256 code challenge and opens the authorization URL in the system browser through Expo WebBrowser/AuthSession.
3. Better Auth requires the existing GitHub sign-in and numeric owner allowlist.
4. The authorization server redirects only to the exact HTTPS App Link.
5. Android verifies the domain association through /.well-known/assetlinks.json and routes the callback to the correct application variant.
6. The app validates state and issuer, then exchanges the authorization code plus verifier. No client secret exists.
7. The server returns a short-lived resource-bound access token and rotating refresh token.
8. The access token remains in memory. The refresh token is stored in SecureStore and excluded from Android backup.
9. On cold start, the app refreshes the access token after device/app-lock checks.

Starting lifetime defaults:

- Access token: 10 minutes.
- Refresh token: 30 days maximum, rotated on every successful use.
- Authorization code: 60 seconds and one use.
- Clock-skew allowance: 60 seconds.

Recheck Better Auth's supported knobs before implementation and record the exact configured values. Reuse detection revokes the refresh-token family. Logout revokes the current family, deletes SecureStore values, closes the encrypted database, and offers to erase cached data. The authenticated web Devices screen can revoke any mobile installation.

Use separate development, staging, and production application IDs, OAuth client IDs, redirect URIs, signing certificates, App Link fingerprints, Firebase apps, icons, and visible environment badges. Wildcard OAuth redirects and broad trusted-origin wildcards are forbidden outside isolated local development.

Optional app lock:

- Disabled by default until owner selection.
- Uses LocalAuthentication to gate retrieval of the SecureStore-wrapped database key and refresh token.
- Falls back to the device credential where supported.
- A biometric enrollment change may invalidate protected material; the app must offer safe sign-out/re-authentication rather than lose server data.

### 39.8 API, cache, and offline synchronization

The Android client uses the same business endpoints under /api/v1. Add only the mobile lifecycle endpoints that are genuinely platform-specific:

- GET /api/v1/client-policy?platform=android&version={version}
- POST /api/v1/mobile/installations
- DELETE /api/v1/mobile/installations/{id}
- POST /api/v1/mobile/push-tokens
- DELETE /api/v1/mobile/push-tokens/{id}
- GET /api/v1/sync?cursor={cursor}&limit={limit}
- POST /api/v1/sync/mutations

Every native request includes:

- Authorization: Bearer access token.
- X-Client-Platform: android.
- X-Client-Version: semantic version.
- X-Client-Build: integer versionCode.
- X-Installation-ID: random server-issued installation ID.
- X-Request-ID: UUID.

The server validates token audience/resource, subject, owner allowlist, authorized client ID, installation status, and minimum compatible version. CORS is not a native-app security control.

SQLCipher SQLite stores only the data required for useful offline reading and safe synchronization:

- cached_stories.
- cached_story_sources with excerpts, not unrestricted full article archives.
- cached_user_item_states.
- cached_tags and cached_annotations.
- cached_digests.
- pending_mutations.
- sync_state.
- local_search_fts.

Generate a random 256-bit database key at first authenticated setup, wrap it with SecureStore/Android Keystore, and exclude both SecureStore and database files from backup and logs. If the key becomes unavailable, delete the disposable cache after confirmation, re-authenticate, and resynchronize. Never hardcode or derive the key from the user ID, package name, device identifier, or signing certificate.

Synchronization rules:

- Server records are the source of truth.
- Initial bootstrap is paginated and resumable.
- Incremental sync uses an opaque durable server cursor.
- Each offline mutation has a UUID idempotency key, base entity version, local sequence, created time, and typed payload.
- Mutations upload in local sequence and remain until the server confirms the idempotency key.
- Version conflicts return current server state and a typed conflict reason.
- Commutative actions such as mark-read may auto-rebase when safe.
- Notes, highlight edits, Later ordering, and destructive state conflicts require an explicit merge or owner choice.
- Undo may cancel an unsent mutation locally or submit the server-backed inverse using the original mutation record.
- A failed or expired auth refresh pauses upload without discarding queued mutations.
- Logout warns about pending mutations and offers Sync now, retain encrypted cache until next login, or discard local changes.
- Local search searches only cached fields and labels incomplete offline coverage.

TanStack Query owns foreground request orchestration and cache invalidation. The explicit SQLite repository owns persistent offline data and outbox state. Zustand remains ephemeral UI state. Do not persist TanStack's entire internal cache or Zustand store as a second undocumented database.

### 39.9 Live updates, background work, and push

Foreground live updates use the same SSE protocol and cursor semantics as web:

- expo/fetch provides a streaming ReadableStream.
- eventsource-parser parses the SSE wire format.
- The platform LiveTransport adapter supplies bearer authorization, AbortController, exponential retry with jitter, Last-Event-ID/cursor resume, and reset_required handling.
- Only one foreground live connection exists per app process.
- The connection stops when the app backgrounds and resumes from the durable cursor on foreground.

Do not maintain a permanent socket or foreground service merely to keep the feed live. Background tasks are deferrable and battery-managed; they cannot guarantee the morning delivery time.

The Railway worker remains the only morning schedule authority. For native notifications:

- Create separate Firebase Android apps for staging and production.
- expo-notifications obtains the native device token with getDevicePushTokenAsync.
- The app submits the token only after authenticated owner consent.
- The Go worker sends through FCM HTTP v1 using a sealed service-account credential.
- Store token, installation, app variant, notification permission state, last success, last failure, and invalidation time.
- Remove tokens on explicit logout, uninstall feedback where available, and permanent FCM invalid-token responses.

Notification channels:

- Critical security: high importance; owner chooses lock-screen detail.
- Morning digest: default importance.
- Sync/operations: low importance and disabled by default.

Payloads contain a story/digest ID and minimal display text. They never contain notes, highlights, tokens, provider IDs, private source credentials, or complete article content. Tapping a notification opens a verified route and then fetches authorized data.

Use Expo Background Task only for opportunistic cursor sync and pending-mutation retry under network/battery constraints. Default minimum interval is six hours. It is not used to create the daily digest, fire an exact alarm, or duplicate server polling.

### 39.10 Server-side mobile records

Add reviewed migrations for:

#### mobile_installations

- id uuid primary key.
- user_id uuid foreign key.
- oauth_client_id text.
- platform text check android.
- app_variant text check development, staging, production.
- app_version text.
- version_code bigint.
- display_name text nullable and owner-editable.
- created_at, last_seen_at, revoked_at.
- last_ip_hash text nullable with short retention; never store a hardware identifier.

#### mobile_push_tokens

- id uuid primary key.
- installation_id uuid foreign key.
- provider text check fcm.
- token_ciphertext bytea.
- token_hash text unique for dedupe.
- permission_state text.
- created_at, rotated_at, last_success_at, last_failure_at, invalidated_at.

Encrypt FCM tokens with an application key distinct from database and auth secrets. Redact them from logs, traces, audit payloads, administration screens, and error reports.

Better Auth OAuth provider tables and migrations remain owned by the authentication subsystem but are reviewed and applied through the same migrate service. Dynamic and unauthenticated client registration stay disabled. The native clients are created idempotently by an attended administration command and recorded in release evidence.

### 39.11 Local Android development

The backend still runs through make and Docker Compose. Metro, the Android emulator, and physical-device tooling run on the host because nesting an emulator inside Compose harms performance and device access.

Host requirements:

- Existing Version 1 tools.
- Android Studio current stable.
- Android SDK platform/build tools matching the pinned compile SDK.
- Android emulator and one pinned virtual-device definition.
- Temurin JDK 17.
- adb.
- KVM acceleration on Linux where available.
- A USB-debuggable physical device for biometric, notification, foldable, and release-install tests.

Make targets added in Phase A0:

| Target | Behavior |
|---|---|
| make android-doctor | Validate JDK, SDK, adb, emulator acceleration, Node/pnpm, ports, app variants, and safe variables |
| make android-bootstrap | Install workspace packages, run expo-doctor, generate the development native project, create the emulator if missing, and install the development build |
| make android-dev | Start safe Compose backend, configure adb reverse, and start the Expo development server |
| make android-dev-device | Configure adb reverse and launch on the attached physical device |
| make android-prebuild | Run clean deterministic Android CNG generation |
| make android-prebuild-check | Generate twice in fresh temporary directories and fail on drift or unreviewed native changes |
| make android-test | Typecheck, Jest, Router integration, domain, storage, sync, and auth tests |
| make android-e2e | Build a release-like test APK and run Maestro against the local fake backend |
| make android-apk | Build an unsigned local production-like APK with fake providers; never reads production signing secrets |
| make android-release-verify artifact=... | Verify APK signature, certificate fingerprint, package ID, version, SBOM, checksums, cleartext policy, and forbidden strings |
| make android-clean | Remove generated native/build/Metro outputs but preserve the encrypted development database unless reset is requested |
| make android-reset | Confirm before removing this app's emulator data, generated native tree, and local mobile fixtures |

Local connectivity:

- Android emulator and USB device use adb reverse tcp:3000 tcp:3000 so the app reaches the local Next.js BFF without exposing it to the LAN.
- The mobile app never calls api:8080 directly.
- Development permits cleartext HTTP only in the development variant and only to the reversed localhost origin through a debug-only network-security configuration.
- Staging and production builds reject cleartext traffic.
- Local auth uses the existing fixture-owner authentication path compiled only into development/test server and app variants. Staging validates the real GitHub/OAuth/App Link flow.

Continuous Native Generation:

- Keep apps/mobile/android out of source control.
- Treat app.config.ts and reviewed config plugins as native source.
- Run expo prebuild --clean for local native regeneration and CI.
- Pin every package and plugin so generation is reproducible.
- make android-prebuild-check compares normalized generated trees and the expected manifest, permissions, network policy, package name, App Links, backup rules, and native dependency list.
- A dependency requiring an unreviewed manual edit to the generated Android directory is rejected or wrapped in a tested first-party config plugin.

Local test services add:

- Fake OAuth authorization/token responses with PKCE validation.
- Fake FCM capture and invalid-token scenarios.
- Deterministic sync cursor pages.
- Offline mutation conflict fixtures.
- App Link test URLs.

### 39.12 Android and Railway configuration

Values with EXPO_PUBLIC are compiled into or readable from the client and are never secrets.

Client build variables:

| Variable | Example | Secret |
|---|---|---:|
| EXPO_PUBLIC_APP_VARIANT | development, staging, production | no |
| EXPO_PUBLIC_API_BASE_URL | https://app.example.com | no |
| EXPO_PUBLIC_WEB_BASE_URL | https://app.example.com | no |
| EXPO_PUBLIC_OAUTH_CLIENT_ID | registered native public client ID | no |
| EXPO_PUBLIC_OAUTH_RESOURCE | urn:developer-intelligence:api | no |
| EXPO_PUBLIC_APP_LINK_HOST | app.example.com | no |
| EXPO_PUBLIC_SENTRY_DSN | optional mobile project DSN | no |
| ANDROID_APPLICATION_ID | com.example.developerintelligence | no |
| ANDROID_VERSION_NAME | semantic version | no |
| ANDROID_VERSION_CODE | monotonically increasing integer | no |

Railway web/BFF variables added only when mobile auth is enabled:

| Variable | Default | Secret |
|---|---|---:|
| MOBILE_OAUTH_ENABLED | false until A1 | no |
| MOBILE_OAUTH_CLIENT_ID | environment-specific public client | no |
| MOBILE_OAUTH_REDIRECT_URI | exact HTTPS App Link | no |
| MOBILE_OAUTH_RESOURCE | product API audience | no |
| MOBILE_ACCESS_TOKEN_TTL | 10m | no |
| MOBILE_REFRESH_TOKEN_TTL | 720h | no |
| MOBILE_MIN_SUPPORTED_VERSION | owner-controlled policy | no |
| MOBILE_LATEST_VERSION | current release | no |
| MOBILE_APP_LINK_CERT_SHA256 | environment certificate fingerprint | no |

Railway worker variables added only when push is enabled:

| Variable | Default | Secret |
|---|---|---:|
| MOBILE_PUSH_ENABLED | false until A5 | no |
| FCM_PROJECT_ID | environment-specific project | no |
| FCM_SERVICE_ACCOUNT_JSON | sealed service-account JSON | yes |
| FCM_DRY_RUN | true in staging verification | no |
| MOBILE_PUSH_MAX_ATTEMPTS | 5 | no |

GitHub Android environment secrets, never Railway variables:

- ANDROID_RELEASE_KEYSTORE_B64.
- ANDROID_KEYSTORE_PASSWORD.
- ANDROID_KEY_ALIAS.
- ANDROID_KEY_PASSWORD.
- ANDROID_FIREBASE_CONFIG_B64 for the matching variant when generated in CI.
- SENTRY_AUTH_TOKEN only if source-map upload is enabled.

Development, staging, and production use different application IDs and certificates:

- com.example.developerintelligence.dev.
- com.example.developerintelligence.staging.
- com.example.developerintelligence.

Replace com.example.developerintelligence only after naming and reverse-domain clearance. The production identifier becomes expensive to change after distribution.

### 39.13 GitHub Actions and release flow

Create GitHub environments android-staging and android-production. Environment secrets may be used on a supported private-repository plan. Required human reviewers are enabled only when the plan supports them for private repositories; otherwise the production workflow requires an attended workflow_dispatch with the signed tag, full SHA, and typed RELEASE_ANDROID confirmation before secrets are read.

#### android-ci.yml

Trigger on pull requests that affect apps/mobile, shared packages, contracts, auth, mobile endpoints, root package metadata, or Android workflow/config files.

Jobs:

- policy: package boundaries, exact versions, Expo-compatible dependencies, no native secret, no forbidden server import.
- typecheck: strict TypeScript for mobile and shared packages.
- unit: jest-expo and React Native Testing Library.
- contract: regenerate packages/api-client and require a clean diff.
- prebuild: expo-doctor, clean CNG generation, deterministic-tree check, manifest/permission/backup/network/App-Link assertions.
- gradle: actions/setup-java v5-era and gradle/actions/setup-gradle v6-era releases pinned to full SHAs, Temurin 17, the committed Gradle wrapper, lint, dependency graph, and release-like test APK.
- security: OSV/npm audit policy, secret scan, SBOM, Android lint, APK forbidden-string scan, exported-component and cleartext-policy checks.
- native-compatibility: APK Analyzer plus Android's zipalign/ELF checks prove every bundled native library supports 16 KB page-size devices.
- e2e: boot pinned API 36 phone and tablet/foldable emulator profiles and run Maestro against fake services.
- size-performance: compare APK, JS bundle, startup, and critical-screen metrics to the accepted baseline.

Do not run signing secrets on pull-request events. Forked or untrusted PRs never receive environment credentials.

#### android-staging.yml

- Trigger after staging CI succeeds or by manual dispatch.
- Build from the exact staging commit.
- Use the staging application ID, icon badge, OAuth client, App Links, Firebase app, and signing key.
- Run real staging OAuth, API, push dry-run, sync, upgrade, and device smoke tests.
- Publish an immutable GitHub prerelease tagged android-vX.Y.Z-rc.N.
- Attach the signed universal APK, AAB, SHA256SUMS, certificate fingerprint, SPDX SBOM, build manifest, test evidence, and release notes.

#### android-release.yml

- Runs only from master for a signed android-vX.Y.Z tag whose tree matches the approved release tree.
- Requires attended approval by the strongest mechanism the repository plan actually supports.
- Re-runs the complete Android suite.
- Generates the native tree once from an immutable git archive.
- Builds the production APK and AAB from that tree.
- Loads the production signing key only inside the signing job.
- Signs, zip-aligns where required, and verifies with apksigner/bundletool.
- Records package ID, versionName, versionCode, minimum/target SDK, signing-certificate SHA-256, commit SHA, dependency lock hash, and artifact SHA-256.
- Creates a draft GitHub Release, uploads every artifact, verifies downloads, then publishes it as immutable.
- Never deploys Railway or changes the mobile minimum-version policy automatically.

GitHub native artifact attestation is required only when the repository plan supports attestations for private repositories. Otherwise retain the build manifest, signed Android artifact, checksums, SBOM, signed tag, protected workflow, and release evidence without falsely claiming GitHub attestation coverage.

### 39.14 Signing-key lifecycle

Create production signing material once, before the first production APK:

- Generate the upload/application-signing key with the current stable Android Studio or JDK tooling and an Android-supported algorithm and size. Record the exact algorithm, size, signature scheme, creation command, and public certificate in release evidence. Set validity beyond the intended app lifetime; Android guidance requires a validity period ending after 22 October 2033 and recommends at least 25 years.
- Keep development, staging, and production keys distinct.
- Store one encrypted offline keystore backup in a separately controlled location.
- Store passwords in the owner's password manager, separate from the keystore backup.
- Store the CI copy only as an environment-scoped encrypted secret.
- Never commit the keystore, passwords, decoded CI file, or secret-bearing Gradle properties.
- Record the public certificate and SHA-256 fingerprint in release evidence and assetlinks.json.
- Test restoration and signing from the backup before the first production release.

Every update uses the same production signing identity. If the self-managed key is lost before Play App Signing is adopted, seamless updates are impossible; the runbook must state that plainly. If Google Play is adopted later, decide whether to provide the existing signing key so GitHub and Play distributions retain a compatible identity, then document upload-key separation and recovery.

### 39.15 Installation and update behavior

GitHub is the initial distribution channel:

1. The owner opens the private repository's Releases page while authenticated to GitHub.
2. The owner downloads the production universal APK, not the AAB.
3. Android may require permission for that browser/file manager to install unknown apps.
4. The owner compares the published SHA-256 and signing-certificate fingerprint on the first install.
5. Android verifies matching signatures for later APK updates and preserves application data.

Because the repository is private, release downloads require GitHub authentication. Never embed a GitHub personal access token in the app to create silent updates.

Settings shows:

- versionName and versionCode.
- Git commit SHA.
- signing-certificate fingerprint abbreviation.
- API environment.
- latest known version and minimum supported version.
- Open GitHub release page.
- Copy checksum and build information.

The backend client-policy endpoint may:

- Show an optional update notice.
- Require an update only for a documented API incompatibility or security issue.
- Provide the release tag and notes URL.

A required-update screen still allows sign-out, local-data deletion, diagnostic export, and opening the authenticated GitHub release page. It does not silently download or install an APK.

Produce both artifacts:

- Universal signed APK for direct installation on phone and tablet.
- Signed AAB retained for a future Google Play internal/public track; an AAB is not presented as directly installable.

Disable EAS Update/over-the-air JavaScript delivery for the first Android release. GitHub-tagged APKs remain the sole production code path. Reconsider OTA only in a new ADR after signed update verification, runtime-version policy, channel isolation, rollback, audit evidence, and the applicable Expo plan are approved.

### 39.16 Testing and device matrix

Unit and integration tests:

- Shared domain invariants run unchanged for web and mobile.
- API client serialization, problem details, ETag, cursor, and version headers.
- OAuth state, nonce, PKCE, issuer, audience, expiry, refresh rotation, reuse, logout, and revocation.
- SecureStore unavailable/invalidated behavior.
- SQLCipher creation, migration, wrong-key, corruption, reset, and upgrade.
- Initial and incremental sync.
- Offline mutation queue, restart, retry, conflict, ordering, and Undo.
- SSE resume, reset_required, token refresh, background/foreground, and duplicate event handling.
- FCM token registration, rotation, invalidation, permission denial, and deep links.
- Window-class changes without state loss.
- Minimum-version and optional-update policy.

Required emulator coverage:

- API 24 minimum phone.
- API 36 compact phone.
- API 36 foldable profile across folded, unfolded, rotation, and split-screen transitions.
- API 36 tablet in portrait, landscape, split screen, and freeform window sizes.

Required physical-device coverage before production:

- The owner's primary supported Android phone.
- The owner's primary supported Android tablet.
- One lower-memory reference Android device or cloud-device profile.
- A physical foldable only if foldable hardware is declared a supported owner device; otherwise use the pinned foldable emulator and an independent cloud-device run.

Device journeys:

- Fresh install and GitHub sign-in.
- Existing web state appears after mobile sync.
- Offline read/Later/star/note mutations survive process death and synchronize once.
- Notification opens the correct story from terminated, backgrounded, and foregrounded states.
- App Link opens the installed app and falls back to web when not installed.
- Staging and production variants coexist and cannot exchange tokens, links, push records, or encrypted storage.
- Production APK upgrades over the previous production APK without data loss.
- A differently signed APK is rejected as an update.
- The release APK installs and starts on a 16 KB page-size emulator/device, and every bundled shared library passes alignment inspection.
- Airplane mode, metered network, captive portal, clock skew, token expiry, database corruption, and server 429/500 behavior.
- TalkBack, 200-percent font, dark/light, reduced motion, hardware keyboard, and predictive back.

Performance gates on a release build:

- Cold start p75 at or below 2.5 seconds on the reference mid-range device.
- Warm resume p75 at or below 750 milliseconds.
- Cached Today and Inbox render without a network-blocking blank screen.
- Scrolling a 1,000-row mixed story fixture sustains the platform frame budget with fewer than 5 percent slow frames.
- Read/star/Later feedback appears within 100 milliseconds before network reconciliation.
- No release increases universal APK size, JS bundle size, cold start, peak memory, or slow-frame rate by more than 10 percent without reviewed evidence.
- Crash-free sessions at or above 99.5 percent during the owner soak.

### 39.17 Security, privacy, and observability

- Use Sentry React Native as the default crash/performance owner only if the phase audit confirms current Expo compatibility and acceptable data handling; otherwise choose an equivalent reviewed provider before A0 completes.
- Upload source maps from the release workflow and never serve them publicly.
- Tag reports with app variant, version, versionCode, commit SHA, route template, device class, and API request ID.
- Do not capture access/refresh tokens, SecureStore values, SQLCipher keys, FCM tokens, full story content, notes, highlights, search text, email, GitHub username, or raw URLs in telemetry.
- Scrub notification payloads, headers, and API bodies before logging.
- Use random installation IDs rather than advertising ID, Android ID, serial number, IMEI, MAC address, or device fingerprinting.
- Disable screenshots on auth/token/recovery screens only if the owner accepts the usability tradeoff; do not blanket-disable screenshots throughout a reading app.
- Reject rooted-device attestation as an authentication requirement in the first release; it creates lockout and maintenance cost without replacing server authorization.
- Network Security Config trusts system roots, forbids cleartext outside debug, and does not implement brittle certificate pinning without an operational rotation design.
- Exported Android components are denied by default; App Link and share-target activities validate every input and expose only the required intent filters.
- The release manifest permission allowlist starts with INTERNET and, only when Phase A5 enables push, POST_NOTIFICATIONS. Any storage, media, camera, microphone, contacts, calendar, location, Bluetooth, nearby-device, accessibility-service, package-query, overlay, exact-alarm, or foreground-service permission requires a new ADR and device test.
- Every native dependency and bundled .so file must pass Android 16 KB page-size compatibility checks; no incompatible prebuilt native library may be waived into release.
- Android backup excludes SecureStore, SQLCipher data, auth state, and notification tokens.
- App uninstall/reinstall is treated as a new installation requiring authentication and resync.

### 39.18 Android implementation phases

Each Android phase ends with PASS, EXTEND, or FAIL and stores evidence under docs/evidence/android/{phase}/{date}.

#### Phase A0: decision, versions, and shared seams

Deliver:

- Re-audited Expo/React Native/Android/Better Auth versions.
- ADRs 021–023.
- apps/mobile scaffold.
- Shared package boundary tests.
- Android application IDs and environment matrix.
- CNG and Make targets.
- Emulator profiles.

PASS:

- expo-doctor, Android doctor, prebuild determinism, generated client compilation, and safe local development pass.
- No production signing or FCM secret is required.

#### Phase A1: OAuth and application shell

Deliver:

- Better Auth OAuth 2.1 provider.
- Native public clients.
- PKCE/App Link sign-in.
- SecureStore token lifecycle.
- Device/session management.
- Compact, medium, and expanded navigation shell.

PASS:

- Real staging GitHub login works on phone and tablet.
- Redirect interception, state/nonce/PKCE, refresh reuse, revocation, and variant-isolation tests pass.

#### Phase A2: online read-only product

Deliver:

- Today, Inbox, Live, Later, Starred, Archive, Snoozed, Story, Releases, Radar, Search, and read-only status/settings.
- Shared API client and domain formatters.
- Foreground SSE.

PASS:

- Server and Android render the same fixture records and state semantics.
- Deep links, foreground resume, accessibility, and performance baselines pass.

#### Phase A3: encrypted offline state and mutations

Deliver:

- SQLCipher cache.
- Incremental cursor sync.
- Mutation outbox.
- State actions, tags, notes, highlights, ordering, conflicts, and Undo.

PASS:

- All offline/process-death/conflict/upgrade/corruption tests pass with no lost confirmed server mutation.

#### Phase A4: adaptive design completion

Deliver:

- Phone, foldable, tablet, split-screen, keyboard, and accessibility polish.
- Night/Day themes.
- Motion, haptics, reader controls, and three-pane expanded layout.

PASS:

- Required emulator and physical-device matrix passes accessibility, visual, state-restoration, and performance gates.

#### Phase A5: push, background sync, and share target

Deliver:

- FCM registration and server sender.
- Notification channels and preferences.
- Story/digest deep links.
- Opportunistic background sync.
- Share-to-Inbox target using the server validation pipeline.

PASS:

- Staging cannot reach production Firebase or app links.
- Notification, token invalidation, permission denial, killed-process, and duplicate-delivery tests pass.
- No device job is treated as an exact scheduler.

#### Phase A6: signed distribution pipeline

Deliver:

- Restored/tested signing-key backup.
- android-ci, android-staging, and android-release workflows.
- Signed staging APK, production rehearsal APK/AAB, SBOM, checksums, manifests, and verification.

PASS:

- A clean owner device installs the staging APK from a GitHub prerelease.
- A subsequent same-key APK upgrades it without losing state.
- Wrong-key, wrong-package, wrong-environment, and modified-artifact checks fail safely.

#### Phase A7: owner soak and production Android release

Duration: minimum 14 consecutive days across phone and tablet.

Required:

- Daily use on both form factors.
- At least one offline mutation session.
- At least one token refresh and forced session revocation.
- At least one app process death during queued work.
- At least one staging APK upgrade.
- At least one FCM invalid-token and provider-outage simulation.

PASS:

- No data loss, cross-environment access, duplicate mutation, auth bypass, critical accessibility failure, or unresolved crash loop.
- Performance and crash-free-session gates pass.
- Owner approves release evidence.

Release:

1. Freeze the approved staging SHA.
2. Merge the matching release PR from staging to master.
3. Create the signed Android tag.
4. Manually run android-release with exact tag, SHA, and confirmation.
5. Verify the downloaded GitHub draft artifacts on a clean device.
6. Publish the immutable release.
7. Install the production APK on phone and tablet.
8. Register production push tokens.
9. Leave minimum-version policy non-blocking for at least 24 hours.
10. Observe auth, sync, push, crash, and performance telemetry.

### 39.19 Android acceptance tests

The Android release is accepted only when all are true:

1. The app builds from the same repository and root pnpm lockfile as web.
2. Shared packages contain no platform UI or server-only imports.
3. Development, staging, and production variants coexist and remain isolated.
4. No client secret or server credential exists in source, bundle, resources, manifest, APK strings, logs, or crash reports.
5. GitHub OAuth completes through Better Auth, OAuth 2.1 authorization code, S256 PKCE, and a verified HTTPS App Link.
6. Only the configured numeric owner identity receives product access.
7. Access-token audience/resource, client, installation, expiry, and minimum-version checks fail closed.
8. Refresh rotation, reuse detection, logout, and web device revocation pass.
9. SecureStore and SQLCipher backup exclusions are present in the generated release manifest.
10. A lost local database key causes a recoverable cache reset, not server data loss.
11. Web and Android show the same reading, star, Later, archive, snooze, tag, note, highlight, and schedule state after sync.
12. Offline mutations survive process death and apply once after reconnection.
13. Conflicting non-commutative mutations never overwrite silently.
14. SSE resumes without missed or duplicated durable events.
15. Backgrounding closes the stream and foregrounding resumes from the cursor.
16. The app does not attempt exact morning scheduling on the device.
17. FCM notifications respect channel, permission, privacy, environment, and token-invalidation rules.
18. Notification and web links open the correct story on compact and expanded layouts.
19. App Link fallback works when the application is not installed.
20. The share target sends only a URL through the authenticated server-side SSRF/content pipeline.
21. Phone, foldable, tablet, rotation, split-screen, and keyboard layout tests pass without losing state.
22. TalkBack, 200-percent font, reduced motion, contrast, and touch-target gates pass.
23. Cached content is usable offline and is visibly labeled when incomplete or stale.
24. The release meets startup, scrolling, response, size-regression, memory, and crash-free-session budgets.
25. The production APK is release-signed, zip-aligned where required, and verified with the recorded certificate.
26. The AAB validates and is not presented as directly installable.
27. A wrong-key APK cannot upgrade the installed app.
28. Every GitHub Release artifact matches its published checksum and build manifest.
29. A private-repository GitHub token is never embedded for update checks.
30. The production release and rollback/revocation runbooks are tested on both phone and tablet.
31. Edge-to-edge, predictive back, freeform resizing, rotation, split screen, cutouts, and keyboard insets pass on API 36 without an orientation/resizability compatibility opt-out.
32. The release manifest contains only the approved permissions and exported components, and every bundled native library passes 16 KB page-size installation and alignment tests.

### 39.20 Future iOS reuse

Expo preserves a credible iOS path, but Android approval does not automatically authorize it. An iOS phase must separately cover Apple developer membership, bundle IDs, Universal Links, Keychain behavior, APNs, app signing, TestFlight/App Store distribution, privacy manifests, device testing, and review requirements. The shared API client, domain package, design tokens, Expo routes, and most React Native feature code may be reused only after platform behavior and design are reviewed.

---

## 40. Naming shortlist

These are preliminary product names from a general exact-name collision screen. They are not trademark or domain clearance.

| Name | Why it fits | Note |
|---|---|---|
| UpstreamIQ | Captures intelligence directly from upstream maintainers and official sources | Recommended |
| StackCurrent | Keeps the owner's actual technology stack current | Most immediately understandable |
| ChangeSift | Sifts high-value software changes from noise | Strong description of the core action |
| ReleaseSift | Emphasizes release intelligence and concise filtering | Slightly narrow for essays and architecture articles |
| ByteCurrent | Short, technical, and implies a continuously refreshed developer current | More brandable than descriptive |
| SourceSift | Highlights audited source coverage and filtering | Accurate but less premium sounding |
| ReleaseBeacon | Important releases become visible signals | Clear, but less focused on personalized analysis |

Names removed after finding active adjacent products or services:

- DevBrief.
- StackBrief.
- StackLoom.
- StackSift.
- ReleaseLens.
- Versionary.
- CodeCurrent.
- SignalForge.

Before choosing:

1. Search USPTO and relevant international classes.
2. Search exact and confusingly similar software marks.
3. Check the preferred domain and common alternatives.
4. Check GitHub organization, npm scope, social handles, and mobile stores.
5. Keep the repository and Railway identifiers generic until the name is cleared.
