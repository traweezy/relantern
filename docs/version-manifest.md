# PR 0 version and supply-chain manifest

Audit date: 2026-08-29
Policy: newest stable, security-patched, mutually compatible release that has
aged at least seven days, followed by exact pinning. A verified security fix may
receive a reviewed age exception.

The root `pnpm-lock.yaml` and `go.sum` are the package integrity authorities.
Container manifest digests and GitHub Action commit SHAs are recorded below.

## Runtime and platform selections

| Component | Selected | Release evidence | Decision |
|---|---:|---|---|
| Go | 1.27.0 | [Go release history](https://go.dev/doc/devel/release) | Stable release from 2026-08-19 |
| Node.js | 24.20.0 | [Node 24 archive](https://nodejs.org/en/download/archive/v24) | Active LTS; Node 26 remains Current |
| pnpm | 11.24.0 | [npm registry](https://www.npmjs.com/package/pnpm/v/11.24.0) | Stable `latest`; 12.0.0 remains on `next-12` |
| PostgreSQL | 18.6 | [18.6 release notes](https://www.postgresql.org/docs/release/18.6/) | Current security-patched 18.x |
| pgvector | 0.8.6 | [pgvector changelog](https://github.com/pgvector/pgvector/blob/v0.8.6/CHANGELOG.md) | Current stable and PostgreSQL 18 compatible |
| Docker Compose | 5.4.0 host baseline | [Compose Watch](https://docs.docker.com/compose/how-tos/file-watch/) | Exceeds the 2.22 Watch floor |

## Frontend release set

| Package | Audited stable | PR 0 pin or status |
|---|---:|---|
| Next.js | 16.3.3 | Pinned; reviewed minimum-age exception for the critical security release and its exact-version `@next/env` and platform `@next/swc-*` artifacts |
| React / React DOM | 19.2.8 | Pinned |
| TypeScript | 5.9.3 | Compatibility hold: newest stable 5.x patch accepted by stable `openapi-typescript` |
| Biome | 2.5.11 | Hold at 2.5.10 until the seven-day release-age gate passes |
| Tailwind CSS / PostCSS adapter | 4.3.3 | Pinned |
| shadcn/ui CLI | 4.19.0 | Audited; add only when generated components enter scope |
| unified Radix package | 1.6.7 | Audited; add only with generated components |
| TanStack Query | 5.102.8 | Audited; add with browser server-state scope |
| TanStack Table | 9.2.4 | Audited stable; add with table scope |
| TanStack Virtual | 3.14.10 | Audited; add with long-list scope |
| TanStack Form | 1.33.5 | Audited; Form 2 remains pre-release |
| Zustand | 5.0.15 | Audited; add only for ephemeral client state |
| Motion | 13.1.1 | Audited; add only for measured interaction value |
| Better Auth | 1.7.2 | Audited; add in the authentication PR after release-age gate |
| Vitest | 4.1.11 | Pinned |
| Playwright | 1.62.1 | Audited; add with browser journey scope |
| openapi-typescript | 7.13.0 | Pinned for generated platform-neutral schemas |
| openapi-fetch | 0.17.0 | Pinned in the shared API client |

Primary release sources include the [Next.js August 2026 security
release](https://nextjs.org/blog), [React versions](https://react.dev/versions),
[TypeScript 5.9 announcement](https://devblogs.microsoft.com/typescript/announcing-typescript-5-9/),
[Biome 2.5 announcement](https://biomejs.dev/blog/biome-v2-5/),
[Tailwind CSS 4.3 announcement](https://tailwindcss.com/blog/tailwindcss-v4-3),
[shadcn/ui changelog](https://ui.shadcn.com/docs/changelog),
[TanStack Table 9 announcement](https://tanstack.com/blog/announcing-tanstack-table-v9),
and [Better Auth 1.7 changelog](https://better-auth.com/changelog).

### Registry integrity records

| Package | Integrity |
|---|---|
| pnpm 11.24.0 | `sha512-vSfjRel23LC+C3oSKCF7BJqBfiGx81XJDb59xGZxiVqLwebQbCRVRQXqk+oLRfSJon7Bv7yN5qlln8oPFvoAAA==` |
| TypeScript 5.9.3 | `sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw==` |
| Biome 2.5.10 | `sha512-WRKXARA3kTuiV5sxqTpobJ/I0MVd4vk3pOL6wnp5az4LntFIhWTj1RWZq3DI9PCEN3lXcqy7p5aqUHzvq8AXyQ==` |
| Next.js 16.3.3 | `sha512-tuRTx1nQ/yVw83cwJBo9F+njGUgMn3UHQycreWHB8XsStvvAh1AthbI8/4IpKnFaF58F+iSiHejYOlMQ/eq83g==` |
| React 19.2.8 | `sha512-PWaYA1L/q9u2u7xYQi+Y3L3Yfnie7XyLeaJICV1MGD6LprsBxcAqGjYyr0eY3p+QdsA+x/Irkt4Qif8D63+Sbw==` |

## Go release set

| Module/tool | Version | Published |
|---|---:|---:|
| `github.com/danielgtaylor/huma/v2` | 2.39.1 | 2026-07-29 |
| `github.com/go-chi/chi/v5` | 5.3.2 | 2026-08-20 |
| `github.com/jackc/pgx/v5` | 5.10.0 | 2026-06-03 |
| `github.com/pressly/goose/v3` | 3.27.3 | 2026-07-22 |
| `github.com/riverqueue/river` | 0.45.0 | Audited; wait for release-age gate before use |
| `github.com/robfig/cron/v3` | 3.0.1 | Audited |
| `github.com/mmcdole/gofeed` | 1.4.2 | Audited |
| `codeberg.org/readeck/go-readability/v2` | 2.1.2 | Audited |
| `github.com/openai/openai-go/v3` | 3.54.0 | Audited; wait for release-age gate before use |
| `go.opentelemetry.io/otel` | 1.46.0 | Audited; wait for release-age gate before use |
| `github.com/sqlc-dev/sqlc` | 1.31.1 | Audited for database PR |

Huma's [OpenAPI generation](https://huma.rocks/features/openapi-generation/)
and sqlc's [pgx/v5 generation](https://docs.sqlc.dev/en/latest/howto/generate.html)
are the reviewed integration paths.

## Container manifests

| Image | Immutable manifest digest |
|---|---|
| `golang:1.27.0-bookworm` | `sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452` |
| `node:24.20.0-bookworm-slim` | `sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e` |
| `gcr.io/distroless/static-debian13:nonroot` | `sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7` |
| `gcr.io/distroless/nodejs24-debian13:nonroot` | `sha256:774b7d020b24214835769e24c3544835526cd0288f0b094eae48e8b2c2429a79` |
| `pgvector/pgvector:0.8.6-pg18-trixie` | `sha256:78bf48b801e792f99e3ac62b5036fd3876e9be48afda16c1e331af1c75ceb2ff` |
| `minio/minio:RELEASE.2025-09-07T16-13-09Z` | `sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e` |
| `minio/mc:RELEASE.2025-08-13T08-35-41Z` | `sha256:a7fe349ef4bd8521fb8497f55c6042871b2ae640607cf99d9bede5e9bdf11727` |
| `grafana/otel-lgtm:0.32.0` | `sha256:d6b20e35890ef2f91d13944805939acdaf1e5d3ffbf9f9aed08586312826c815` |

The selected database image was executed during the audit and reported
PostgreSQL `18.6` and vector extension `0.8.6`.

## GitHub Action pins

| Action | Release | Commit SHA |
|---|---:|---|
| `actions/checkout` | 7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | 7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `actions/setup-node` | 7.0.0 | `820762786026740c76f36085b0efc47a31fe5020` |
| `actions/upload-artifact` | 7.0.1 | `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` |
| `actions/download-artifact` | 8.0.1 | `3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c` |
| `actions/dependency-review-action` | 5.0.0 | `a1d282b36b6f3519aa1f3fc636f609c47dddb294` |
| `github/codeql-action` | 4.37.9 / bundle 2.26.4 | `cdf488f595d80d6e07e03d4674febd5ab45fa938` |
| `docker/setup-buildx-action` | 4.3.0 | `37fe631027851001ddb9b187196cc803df7f5f0e` |
| `docker/build-push-action` | 7.3.0 | `53b7df96c91f9c12dcc8a07bcb9ccacbed38856a` |
| `pnpm/action-setup` | 6.0.10 | `0977fd99725f1db4007ccb2928dbb4e90d06cc86` |

## CI validation tools

| Tool | Version | Integrity policy |
|---|---:|---|
| ripgrep | 15.2.0 | Linux x86_64 archive pinned to SHA-256 `33e15bcf1624b25cdd2a55813a47a2f95dbe126268203e76aa6a585d1e7b149c` |
| actionlint | 1.7.12 | Go module version pinned in the workflow lint target |
| gitleaks | 8.30.1 | Go module version pinned in the security job |
| govulncheck | 1.7.0 | Go module version pinned in the Go vulnerability job |

## Compatibility holds and exceptions

- pnpm 12.0.0 is not selected while its registry channel remains `next-12`.
- Biome 2.5.11 is held until it satisfies the seven-day age policy.
- Next.js 16.3.3 receives a minimum-age exception because it fixes two
  critical vulnerabilities and is the Active LTS security floor.
- TypeScript 7.0.2 is held because stable `openapi-typescript` 7.13.0 declares
  TypeScript 5.x compatibility. TypeScript 5.9.3 keeps the peer graph clean;
  the hold is revisited when the generator publishes stable TypeScript 7 support.
- Node 26 is Current rather than LTS and is excluded from production.
- PostgreSQL 19 remains beta and is excluded from production.
