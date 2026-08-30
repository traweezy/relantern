# Source policy takedown runbook

## Trigger

- A publisher, owner, legal requirement, robots/terms review, or license change
  requires polling, stored-body, or presentation removal.

## Impact

New ingestion stops and some retained bodies may require deletion. Claims and
audit metadata may need suppression while preserving minimal compliance proof.

## Immediate containment

1. Disable every endpoint for the source and exclude it from digests.
2. Record requester, authority, source ID, policy/license record, requested
   scope, timestamps, and affected object/revision counts.
3. Do not destroy evidence until scope, retention exceptions, and legal needs
   are clear.

## Exact verification commands

```sh
make sources-verify
make digest-preview
docker compose logs --since=30m worker api
```

## Recovery

1. Apply the narrowest reviewed deletion/suppression workflow. Never delete by
   a broad bucket prefix or ad hoc SQL.
2. Remove eligible bodies through the retention object-key validator; preserve
   minimal hashes/audit when permitted.
3. Rebuild search and digest projections so removed content cannot surface.
4. Resume only after a new explicit policy review.

## Data-integrity checks

- No enabled endpoint or pending job remains for the source.
- Search, Today, digest, and demo do not expose removed content.
- Deletion scope matches the recorded IDs and no unrelated evidence changed.

## Communication

Record request, authority, scope, completion, retained metadata rationale, and
owner impact. Avoid republishing disputed content in the incident record.

## Post-incident evidence

Retain request, policy decision, bounded deletion manifest, verification
queries, and completion acknowledgement.
