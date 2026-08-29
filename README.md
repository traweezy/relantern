# Relantern

Relantern is a private, single-owner developer-intelligence platform. It
continuously gathers evidence from an explicit source registry and produces a
concise, personalized morning brief. Deterministic code owns coverage,
freshness, scheduling, deduplication, delivery, and cost limits; bounded AI
stages extract and synthesize claims only after evidence has been stored.

The implementation authority is
[the build specification](docs/PERSONAL_DEVELOPER_INTELLIGENCE_PLATFORM_BUILD_SPEC.md).

## Current phase

The reproducible foundation, database/API skeleton, parallel CI graph, and
reviewed source-registry contract are implemented. Live ingestion and provider
access remain intentionally disabled until the SSRF-safe fetcher in PR 4 is
implemented and approved.

## Safe local workflow

Prerequisites are Docker Engine/Desktop with Compose Watch, GNU Make, and Git.
Host Go, Node, and pnpm are optional for editor tooling.

```sh
make doctor
make bootstrap
make dev
```

The default stack binds only to loopback and uses fake source, OpenAI, and
delivery providers. It cannot contact live delivery endpoints.

Useful targets:

```sh
make ps
make logs
make test
make prepush
make prodlike-smoke
make stop
```

`make workflow-lint` validates all GitHub workflow and local composite-action
files with the pinned actionlint release. CI warms the pnpm and Go caches in
setup gates, then runs the independent frontend and Go checks in parallel. See
[ADR-011](docs/adr/011-parallel-github-actions.md) for the graph and plan-aware
security gates.

`make sources-verify` performs strict, zero-network validation of 91 reviewed
source endpoints and the deterministic fixture catalog. The forward-migration
release command mirrors built-ins into PostgreSQL as paused entries; no poll is
scheduled while the registry network fuse is off. See
[ADR-012](docs/adr/012-reviewed-source-registry.md).

`make reset` is destructive and requires confirmation. It targets only the
validated Relantern Compose project and its volumes.

## Service boundaries

- `web`: the only public application service; Next.js UI and BFF boundary.
- `api`: private Huma/chi API.
- `worker`: private polling, scheduling, intelligence, and delivery worker.
- `migrate`: one-shot forward migration service.
- `postgres`: PostgreSQL 18.6 with pgvector 0.8.6.
- `minio`: local-only S3-compatible development storage.
- fake providers: development/test-only source, OpenAI, and delivery surfaces.

No secret belongs in Git, a `NEXT_PUBLIC_*` variable, an APK, or a browser
bundle. See `.env.local.example` and `docs/runbooks/rotate-secrets.md` as those
artifacts are introduced.
