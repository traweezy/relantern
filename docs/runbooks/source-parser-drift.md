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
3. Replay the immutable raw object. For a feed or API collection, compare the
   affected external entry IDs and their child raw hashes before replaying
   parse jobs. Create a material revision only when that entry's normalized
   hash changes.
4. Resume the endpoint after all reviewed fixtures pass.

A failed collection split or permanent child parse records a failed attempt and
stops the endpoint. Resume re-arms it; source reconciliation requeues the
specific retained raw document, then clears its failure after the parent
entries are durably recorded or the child is deduplicated. The live external
API fuse must still be enabled for source reconciliation. Cross-origin entry
links stay in the child raw payload as untrusted metadata, with canonical
evidence URLs on the reviewed feed origin. The reviewed GitHub API alias is
limited to exact release and repository advisory item URLs on `github.com`
under the same repository as the `api.github.com` collection.

A stored collection also starts with a durable `pending_entries` marker in the
same transaction as its checkpoint and parse job. A storage or database error
while recording children leaves that marker in place. If the River job runs out
of attempts, reconciliation can requeue the retained raw document; a
successful child-recording pass clears the marker.

## Data-integrity checks

- Prior revision and raw hashes remain unchanged.
- Offsets, outline, title, dates, and canonical URL are deterministic.
- Parser replay does not create duplicate items/stories.
- The source entry ID remains stable when an entry URL changes, and other
  entries from the same fetch retain their own revision chains.

## Communication

Record source, failure window, affected item count, last good parser, current
content-policy status, and recovery. Do not attach full copyrighted bodies.

## Post-incident evidence

Retain sanitized fixture, before/after parser output, attempt IDs, tests, and
source-policy review.
