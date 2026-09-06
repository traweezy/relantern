# Public demo deployment

Owner: repository owner
Last verified: 2026-09-06
Review trigger: demo routes, dependencies, server, Railway configuration, or fixture changes

## Scope

The owner authorized a public Railway demonstration on 2026-09-06. This is an
isolated static export of the existing synthetic interface, built from
`apps/web/demo`. Source publication was separately authorized on the same date
in [ADR-026](adr/026-public-source-private-runtime.md); neither action changes
the private application's staging or production release controls.

Only the `web` service in the separate `demo` environment receives this
artifact. No database, broker, authentication, AI, delivery, or storage service
is needed. The private application and its existing environments stay separate.

## Build and verify

```sh
pnpm demo:build
pnpm demo:test
```

The build reuses the existing root pnpm lockfile, installed toolchain, styles,
and demo components. It exports only admitted public routes. The packager
rejects additional HTML routes, symlinks, unexpected asset types, and missing
required pages; computes inline-script CSP hashes; and records SHA-256 checksums
for every served asset.

The generated `.local/public-demo` directory contains the deployable artifact.
Only its static assets, checksum manifest, CSP, and dependency-free server are
copied into a pinned non-root Node 26 distroless image. Source, local secrets,
private routes, and backend adapters are not included. The runtime verifies
asset checksums before accepting traffic and serves only manifest-listed files.
Request URLs never access the filesystem.

`make prepush` includes the demo build and HTTP boundary tests. The HTTP tests
cover private paths, metadata/credential paths, rejected write methods, CSP,
cache revalidation, health, and asset-integrity failures.

## Railway configuration

The artifact includes `railway.json`: one replica, `/healthz` readiness, bounded
restart attempts, and serverless sleep for periods without traffic. Railway
provides HTTPS and `PORT`. No application secrets are required. Static serving
uses compression, immutable asset caching, and short HTML caching. Cold-start
latency is possible after inactivity.

Deploy the generated directory with an explicit project, environment, and
service, using `railway up --path-as-root`. This uploads an approved demo
artifact directly and does not require a Git push. Public source access does
not authorize private application deployment or provider activation.

## Public boundary and verification

The expected contract is HTTP 200 for the demo and health check, HTTP 404 for
private or unknown routes, and HTTP 405 for write methods. Every response has
security headers and `noindex`; scripts use hashes rather than an unsafe-inline
script policy. Same-origin connection policy permits only static navigation.

Access logs contain method, admitted route, status, and duration. They do not
record query strings, request headers, client addresses, or fixture contents.
The service handles SIGTERM gracefully with a bounded shutdown deadline.

Verified on 2026-09-06:

- Public URL: [relantern demonstration](https://web-demo-ce4e.up.railway.app/demo).
- Railway deployment: `e33db89b-7600-40ad-86d8-548d0dbedd47`, successful health check.
- The deployed static assets match the local manifest SHA-256 checksums.
- Desktop (1440 px) and mobile (390 px) browser interactions pass; automated
  accessibility reports no violations, no page overflow or browser errors.
- All browser requests stay on the demo origin and use only GET/HEAD.
- Public HTTP checks verify health, HEAD, rejected writes and private-path 404s.
- Gitleaks reports zero findings in the upload. The demo image has zero
  HIGH/CRITICAL Trivy findings; a CycloneDX SBOM was generated.
- Existing staging/production service sources, domains and deployment IDs match
  their pre-deployment state. Demo variables contain only Railway metadata.
- The initial deployment was built from the reviewed working tree. Its artifact
  manifest records the base commit and explicitly marks local changes; later
  source publication does not change those deployed bytes.

Local review evidence is in `/tmp/portfolio-demo-deploy-20260906`. These local
reports are temporary; retain them with release evidence when approving a commit.

## Rollback

Redeploy the last verified demo artifact to the same explicit demo service and
environment. To take the demo offline, remove its public domain or stop only
its demo deployment. Do not change private staging or production services.
