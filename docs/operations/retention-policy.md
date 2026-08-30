# Data retention policy

| Data | Default | Action after window |
|---|---:|---|
| Raw source object | 180 days | Delete body unless it supports a published item or claim; retain hash/metadata |
| Normalized revision object | 365 days | Delete body unless it supports a published item or claim; retain revision metadata |
| Published claims, briefs, citations, digests, audit identity | Indefinite | Owner-directed deletion only |
| Fetch and parse attempts | 90 days | Delete operational rows |
| Item-mutation before/after state | 30 days | Null snapshots; retain mutation identity and timestamps |
| Processed OpenAI webhook event detail | 30 days | Delete processed event row; retain derived AI provenance/cost |
| Outbox replay | 7 days | Delete replay row; stale clients perform reset/refetch |
| Detailed traces | 14 days | Configure in telemetry backend |
| Application logs | 30 days | Configure in Railway/log backend |

`make retention-run` exercises the same idempotent runner used by the daily
maintenance job. A run processes at most ten batches of 100 objects per class,
then one transactional operational-row prune. It stops on the first storage or
database error and records a bounded failure code.

Retention never uses a raw URL as an object key or telemetry label. Object
deletion accepts only validated `raw/.../<sha256>.<ext>` and
`normalized/.../<sha256>.txt` keys and rejects the staging prefix.
