# PR 0 foundation evidence

Date: 2026-08-29
Result: PASS — owner review required before ingestion begins.

## Scope proven

- Owner decisions, source policy, versions, image manifests, action commits,
  and ADRs 001–010 are recorded.
- One Go module and one pnpm workspace/lockfile provide the runtime and shared
  package boundaries from the specification.
- The safe local stack contains PostgreSQL/pgvector, MinIO, migration, API,
  worker, web, and fake source/OpenAI/delivery services.
- Live source ingestion, OpenAI API access, Discord, Resend, and email are
  absent or explicitly fused off.
- Fixed clocks are rejected in staging and production configuration.

## Commands and results

| Command or check | Result |
|---|---|
| `make doctor` | PASS; Docker 29.7.2, Compose 5.4.0 with Watch, Git, Make, architecture, disk, permissions, and fuses validated |
| `make bootstrap` | PASS; frozen install, generation, exact image builds, private bucket setup, migration, and idempotent seed completed |
| `make config-check` | PASS; Compose base/overlay, Docker/Railway paths, clock mode, and reviewed image digests agree |
| `make prepush` | PASS; Biome, gofmt, vet, strict TypeScript, tests, integration smoke, generated drift, policy, race detector, and production build |
| `make prodlike-smoke` | PASS; exact production stages healthy on web, API, worker, PostgreSQL, MinIO, and fake providers |
| `make time-travel at=2026-08-29T08:00:00-04:00` | PASS; one schedule occurrence and one captured delivery |
| `gitleaks git .` using v8.30.1 | PASS; four commits scanned and no tracked secret leak found |

The host has Node 26 Current, so host pnpm reports the expected engine warning.
The production image independently built and ran with the pinned Node 24.20.0
LTS image. TypeScript is held at 5.9.3 because the stable OpenAPI generator's
declared peer range is 5.x; `pnpm peers check` reports no peer issues.

## Runtime evidence

- Database: PostgreSQL 18.6.
- Extensions: `vector=0.8.6`, `pg_trgm=1.6`.
- Runtime users: web, API, worker, and migration images report
  `nonroot:nonroot`.
- Long-running services: healthy.
- One-shot services: migration, seed, and MinIO initialization exited 0.
- Published development ports bind to `127.0.0.1` only.
- Web CSP uses per-request nonces for scripts and styles; it contains no
  `unsafe-inline` directive.
- Empty capture state is deterministic JSON: `{"captures":[],"count":0}`.
- A full-directory secret scan found only generated Next.js nonces under the
  ignored `.next` directory; the Git-history scan found no leak.

## Schedule evidence

The isolated injected-clock run persisted:

```text
delivered|scheduled|2026-08-29|2026-08-29 12:00:00+00
```

The fake provider returned one capture with the same local date and scheduled
instant. The time-travel containers were stopped without deleting their
database volume, `relantern-time-travel_postgres-data`.

## Visual evidence

The production page was opened in Firefox 154.0.1 on the active Wayland desktop
and captured with Flameshot. The screenshot was inspected for layout, spacing,
wrapping, clipping, focus-independent hierarchy, and fallback styling.

- Path: `/tmp/relantern-pr0-home.png`
- SHA-256: `c8ec5c622f59acdecd3093b7a0776b64a8e0ba4246f99c468b5da8acab48f890`
- Resolution: 8660×3200 across the active desktop
- Result: PASS

## Gate

Do not implement source ingestion or enable any live provider after this
report. The owner must review and approve
`docs/version-manifest.md`, `docs/owner-decisions.md`, the infrastructure
manifest, and this evidence first.
