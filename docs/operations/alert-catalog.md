# Operational alert catalog

Private worker `GET /metrics` exposes low-cardinality Prometheus text. Private
`GET /alerts` evaluates the same snapshot. Only `web` is public; these worker
routes remain on Railway private networking.

| Alert | Severity | Condition | Runbook |
|---|---|---|---|
| auth bypass or secret leak | critical | security event or credential exposure | owner authentication / rotate secrets |
| database unavailable | critical | readiness or metric query cannot reach PostgreSQL | database unavailable |
| migration failure | critical | migrate exits nonzero | rollback release |
| restore integrity/objective | critical | latest drill fails or misses target | restore PostgreSQL |
| watched critical advisory missed | critical | confirmed advisory exceeds 10 minutes | security advisory missed |
| budget hard cap exceeded | critical | recorded cost crosses hard cap | OpenAI budget exhausted |
| priority source freshness | warning | oldest P0 success exceeds 15 minutes | source failing |
| queue backlog | warning | oldest available/retryable job exceeds 15 minutes | queue backlog |
| digest delayed | warning | active daily occurrence is more than 15 minutes late | digest missed or late |
| object storage failure | warning | worker storage check or operation fails | object storage unavailable |
| repeated parser drift | warning | parse failures repeat for one reviewed source | source parser drift |
| retention failed | warning | latest retention run is failed | retention policy and database/object runbooks |
| restore drill stale | warning | no passing drill in 35 days | restore PostgreSQL |

Metrics never label raw URLs, titles, provider response IDs, recipients, owner
identifiers, or job IDs. Those bounded correlations belong in access-controlled
structured logs and audit records.
