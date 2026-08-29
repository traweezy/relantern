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
| Node.js | 26.8.1 | [Node 26.8.1 release](https://nodejs.org/en/blog/release/v26.8.1) | Owner-directed Current-line and release-age exception; exact production pin |
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
| Better Auth | 1.7.1 | Pinned for owner OAuth; newest 1.7.x satisfying the seven-day release-age gate |
| node-postgres (`pg`) | 8.23.0 | Pinned PostgreSQL adapter peer for Better Auth |
| Vitest | 4.1.11 | Pinned |
| Playwright | 1.62.1 | Audited; add with browser journey scope |
| openapi-typescript | 7.13.0 | Pinned for generated platform-neutral schemas |
| openapi-fetch | 0.17.0 | Pinned in the shared API client |
| OpenAI JavaScript SDK | 7.5.0 | Pinned for raw-body webhook verification; released 2026-08-17 and satisfies the seven-day gate |

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
| OpenAI JavaScript SDK 7.5.0 | `sha512-ZbDBz8FSB8Mv8fFYIUvzTFMdV5vl93/octp1MdtK2lfYepSpfv/ewmeugpKz/cwGtFSx+YuUM4NwpZ2P55YiPA==` |
| Better Auth 1.7.1 | `sha512-g8WlTQijxXWJjPVZfFu1+EJg9cwwHrKDmIkcYMzx8CzYA+tDxl6NI7qQbKkbgw5UtHILsT5VH+RMzFzwnVJqAg==` |
| node-postgres 8.23.0 | `sha512-Ip2EQCngowJLGOfCwkFhPXU7/ljlhn6Rxlmy4XYfL2Y+vyRM59+8uR2xqRWKdYmbXmxCFOAmKxBuSUCdF34qLg==` |
| `@types/pg` 8.23.1 | `sha512-fKVHpikPdg4GKks3JuLEhvwSyvwzF23hnabPy6DD8ljVbC7+6J5dQzdv4arV6jqq57djnMgs1HKBxX4P8aBI3A==` |

## Go release set

| Module/tool | Version | Published |
|---|---:|---:|
| `github.com/danielgtaylor/huma/v2` | 2.39.1 | 2026-07-29 |
| `github.com/go-chi/chi/v5` | 5.3.2 | 2026-08-20 |
| `github.com/jackc/pgx/v5` | 5.10.0 | 2026-06-03 |
| `github.com/pressly/goose/v3` | 3.27.3 | 2026-07-22 |
| `gopkg.in/yaml.v3` | 3.0.1 | Stable strict registry/fixture decoder |
| `github.com/riverqueue/river` | 0.44.1 | 2026-08-21; newest stable release satisfying the seven-day age gate; MPL-2.0 |
| `github.com/robfig/cron/v3` | 3.0.1 | Audited |
| `github.com/mmcdole/gofeed` | 1.4.2 | 2026-08-20 |
| `codeberg.org/readeck/go-readability/v2` | 2.1.2 | 2026-06-18 |
| `golang.org/x/net` | 0.58.0 | 2026-08-12 |
| `golang.org/x/text` | 0.41.0 | 2026-08-11 |
| `github.com/minio/minio-go/v7` | 7.3.0 | Pinned Apache-2.0 S3-compatible streaming client; released 2026-08-15 |
| `github.com/pgvector/pgvector-go` | 0.4.1 | 2026-07-30; pinned sqlc pgx vector codec |
| `github.com/openai/openai-go/v3` | 3.52.0 | 2026-08-17; newest stable release satisfying the seven-day age gate |
| `go.opentelemetry.io/otel` | 1.46.0 | Audited; wait for release-age gate before use |
| `github.com/sqlc-dev/sqlc` | 1.31.1 | Audited for database PR |

Huma's [OpenAPI generation](https://huma.rocks/features/openapi-generation/)
and sqlc's [pgx/v5 generation](https://docs.sqlc.dev/en/latest/howto/generate.html)
are the reviewed integration paths.

## Container manifests

| Image | Immutable manifest digest |
|---|---|
| `golang:1.27.0-bookworm` | `sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452` |
| `node:26.8.1-bookworm-slim` | `sha256:367679cf9792759492a486e4aa4b421764d71a9546a6dae8aab81a99eb797b3e` |
| `gcr.io/distroless/static-debian13:nonroot` | `sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7` |
| `gcr.io/distroless/nodejs26-debian13:nonroot` | `sha256:10ec8cb93ef461563da50d4eb8dfac7d048783826825bf5b07510c2f34c14315` |
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
- Node 26.8.1 is the owner-selected production line under an explicit
  Current-line and release-age exception; every runtime and build input is
  pinned exactly.
- River 0.45.0 and 0.46.0 were released on 2026-08-25 and 2026-08-29,
  respectively, so 0.44.1 is the newest stable release eligible under the
  seven-day dependency-age policy.
- openai-go 3.53.0 and 3.54.0 were released on 2026-08-26 and 2026-08-27,
  respectively, so 3.52.0 is the newest stable release eligible under the
  seven-day dependency-age policy.
- OpenAI JavaScript SDK releases newer than 7.5.0 had not satisfied the
  seven-day dependency-age policy at audit time.
- Better Auth 1.7.2 was released on 2026-08-26 and had not satisfied the
  seven-day dependency-age policy. Version 1.7.1 is the newest eligible stable
  release for PR 11.
- PostgreSQL 19 remains beta and is excluded from production.
