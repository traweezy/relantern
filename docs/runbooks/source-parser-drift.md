# Source parser drift runbook

## Trigger

- Repeated parse failures, empty required fields, unexpected warning growth, or
  fixture/output drift for one reviewed endpoint.

## Impact

New evidence may be delayed, malformed, or incorrectly deduplicated. Existing
revisions and published evidence remain immutable.

## Immediate containment

1. Pause the affected endpoint without disabling unrelated sources.
2. Preserve fetch/parse attempt IDs, raw hash/object key, parser name/version,
   warnings, status/content type, and last good revision.
3. Do not publish or deliver content parsed under an unverified workaround.

## Exact verification commands

```sh
make sources-verify
go test ./internal/parsing/... -count=1
docker compose logs --since=30m worker fake-source
```

## Recovery

1. Confirm the provider changed shape or media type using a policy-compliant
   recorded fixture.
2. Update the smallest parser boundary and expected normalized fixture.
3. Replay the immutable raw object; create a material revision only when the
   normalized hash changes.
4. Resume the endpoint after all reviewed fixtures pass.

## Data-integrity checks

- Prior revision and raw hashes remain unchanged.
- Offsets, outline, title, dates, and canonical URL are deterministic.
- Parser replay does not create duplicate items/stories.

## Communication

Record source, failure window, affected item count, last good parser, current
content-policy status, and recovery. Do not attach full copyrighted bodies.

## Post-incident evidence

Retain sanitized fixture, before/after parser output, attempt IDs, tests, and
source-policy review.
