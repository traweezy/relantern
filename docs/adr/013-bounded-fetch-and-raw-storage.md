# ADR-013: Bounded fetch and immutable raw storage

Status: Accepted
Date: 2026-08-29

## Context

Reviewed endpoints must be fetched without turning Relantern into a general
network proxy or allowing DNS rebinding, redirects, compression, retries, or
large response bodies to bypass resource boundaries. Successful bytes need an
immutable evidence identity, while HTTP validators and every attempt remain
durable and inspectable. The owner has not authorized live source polling.

## Decision

- Require HTTPS, an exact reviewed endpoint-host allowlist, no URL userinfo or
  fragments, and ports 80 or 443. HTTP, private addresses, and alternate ports
  are available only to explicitly named local/test fixture targets.
- Resolve DNS before a request and again inside `DialContext`. Reject loopback,
  private, shared, link-local, multicast, documentation, benchmarking,
  translation, tunneling, and reserved address ranges. Dial a validated IP
  directly while retaining the reviewed hostname for TLS verification.
- Disable environment proxy inheritance. Cap redirects at five and apply the
  same URL, host, DNS, address, and port policy to every redirect.
- Use five-second connection/TLS, ten-second response-header, and thirty-second
  per-attempt deadlines. Bound response headers, global concurrency at 20,
  per-host concurrency at two, and host request rate with a token bucket.
- Send a GitHub-URL contact-bearing User-Agent, conditional ETag and
  Last-Modified headers, and an explicit Accept list. Treat HTTP 304 as a
  successful poll without a new object. Preserve `Retry-After`; short delays
  may retry with exponential jitter, while long delays return a durable future
  retry time instead of sleeping a worker indefinitely.
- Disable transparent transport decompression. Accept identity and gzip,
  count compressed bytes, cap decompressed bytes, and reject a ratio above
  100:1. Endpoint limits may be lower than the reviewed connector defaults but
  cannot be raised through runtime input.
- Stream allowed raw bodies through SHA-256 into a random private staging key
  with the Apache-2.0 MinIO Go client v7.3.0. After all size and ratio checks
  pass, copy the staged object to
  `raw/{source_id}/{yyyy}/{mm}/{dd}/{sha256}.{extension}` and remove staging.
  Content-addressed promotion is idempotent. `metadata-only` sources are
  hashed and discarded rather than persisted.
- Persist checkpoints, runtime source overrides, every attempt, retry
  metadata, and raw-document identity in additive PostgreSQL tables. Update
  checkpoint and success state in one transaction; never erase a prior
  checkpoint after a failed attempt. Paused endpoints remain paused.
- Validate the S3 adapter against real loopback-only MinIO in a dedicated CI
  job that runs in parallel with other Go gates. Database integration uses a
  serializable transaction and rolls its fixture back.

## Reliability and observability

Every HTTP attempt records timestamps, status, final URL, allowlisted content
type, compressed and decompressed byte counts, duration, validators,
`Retry-After`, and a stable error code. These records provide the initial
source-health audit trail before the metrics adapter and due-source scheduler
arrive. The fetch path is bounded by its request deadline and backpressure;
priority freshness remains the specification target of 95 percent within 15
minutes once staged polling is authorized.

An object can exist without database metadata if promotion succeeds and the
database transaction later fails. Its content-addressed key makes retries
safe; a later maintenance job will remove unreferenced objects after a
retention window. Database metadata must never reference an object unless the
promotion completed.

## Consequences

The network boundary is independently testable with fault fixtures and fuzz
seeds, core fetcher coverage exceeds 80 percent, and raw storage does not hold
an unbounded response in memory. The storage SDK expands the Go dependency
graph and therefore remains pinned, scanned, and listed in the supply-chain
manifest.

The registry's top-level `enabled: false` fuse remains mandatory. This change
does not schedule a source, enable live fixture recording, contact a publisher,
or deploy Railway. Parsing, revisions, normalization, and staged ingestion
rollout remain separate reviewed changes.

## Primary references

- [Go HTTP transport](https://pkg.go.dev/net/http#Transport)
- [Go network address types](https://pkg.go.dev/net/netip)
- [MinIO Go SDK releases](https://github.com/minio/minio-go/releases)
- [MinIO Go object API](https://github.com/minio/minio-go/blob/master/docs/API.md)
- [RFC 9110 Retry-After](https://www.rfc-editor.org/rfc/rfc9110.html#name-retry-after)
