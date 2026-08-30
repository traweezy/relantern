# Object storage unavailable runbook

## Trigger

- Worker startup check, fetch, parse, retention, or object read/delete fails.
- Bucket policy, credential, capacity, or provider health changes unexpectedly.

## Impact

New raw/normalized evidence cannot be committed or read. Database metadata and
existing private briefs remain, but ingestion and evidence inspection degrade.

## Immediate containment

1. Pause source intake and retention. Keep delivery disabled if evidence cannot
   be verified.
2. Record bucket/environment, release SHA, failing operation, bounded object
   key prefix, and first failure. Never record storage credentials.
3. Do not null metadata, recreate the bucket publicly, or bypass TLS/policy.

## Exact verification commands

```sh
make ps
docker compose logs --since=30m minio worker
curl --fail --silent --show-error http://127.0.0.1:9000/minio/health/live
make test-integration
```

## Recovery

1. Restore private bucket health, least-privilege credentials, capacity, and
   network access.
2. Confirm staging/incoming cleanup and a bounded put-copy-read-delete test.
3. Resume durable fetch/parse work; idempotency prevents duplicate revisions.
4. Resume retention only after evidence reads pass.

## Data-integrity checks

- Bucket remains non-public and contains only approved content-addressed keys.
- Database object keys exist or carry an explicit retention-pruned timestamp.
- Staging objects are not treated as committed evidence.

## Communication

Record affected evidence windows, failed operations, recovery time, and any
reprocessing. Redact object content, URLs, and credentials.

## Post-incident evidence

Retain provider status, bounded storage logs, consistency query, test result,
capacity graph, and the prevention change.
