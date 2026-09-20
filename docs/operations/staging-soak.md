# Railway staging soak procedure

P9 requires at least 336 continuous hours on one frozen release SHA. This
procedure never authorizes a production deploy. The specification remains the
authority when a result is ambiguous.

The living [staging provisioning status](staging-provisioning-status.md)
records the current attended setup state. It is not soak evidence and does not
replace any gate below.

## Preconditions

1. Freeze a full 40- or 64-character lowercase Git SHA from `staging`. Run
   `make prepush`, `make security-scan`, the production-like smoke test, and the
   restore drill against that tree.
2. Review `make railway-plan environment=staging`. It must contain no
   destructive change. Infrastructure apply remains an attended Railway
   operation; this repository intentionally exposes no apply target.
3. Enter distinct staging GitHub OAuth, OpenAI, Discord test-channel, and
   optional Resend test-recipient credentials out of band. Never copy a
   production credential or destination into staging.
4. Enter identical attended copies of the staging `DATABASE_URL` for
   `postgres`, `migrate`, `api`, `worker`, and `web`, and of the staging
   `WEB_INTERNAL_SERVICE_TOKEN` for `api` and `web`, and `OPENAI_API_KEY` for
   `api` and `worker`; then seal every copy. Railway sealed variables cannot be
   cross-service references.
5. Confirm the OpenAI project hard cap, off-platform encrypted backup, and
   staging-only delivery recipient in their provider control planes.
6. Generate a Railway domain for `web` only. Keep all TCP proxies disabled.
   Run:

   ```sh
   make railway-readiness environment=staging release_sha=<full-sha>
   ```

   The audit reads topology, deployment metadata, domains, and TCP exposure.
   It does not request or print Railway variable values.
7. Copy `docs/evidence/staging/soak-template.json` to a dated ledger, then set
   the Railway project ID, frozen SHA, RFC3339 start time, and confirmed
   profile. Do not set `endedAt` until the review is concluded.

The clock starts only after the exact SHA is healthy, readiness passes, all
Priority 0 sources are active, real APIs are read-only, low-cap OpenAI is
active, staging delivery isolation is proven, and backup automation exists.

## Daily observation

Record at least one chronological observation every 26 hours. Each observation
must use the frozen SHA and include a committed repository-relative evidence
path or immutable HTTPS URL plus the lowercase SHA-256 digest of that artifact.
Redact credentials, URLs containing tokens, owner content, recipient identity,
and private source text.

`soakctl` recomputes every repository-relative artifact hash and rejects
missing files, oversized files, and symlinks escaping the repository. Remote
HTTPS evidence must be immutable and may not use query strings, fragments, or
embedded credentials.

Each observation records:

- source freshness SLO result;
- duplicate alert count;
- reviewed unsupported material claim count;
- queue health and recovery;
- anonymous demo uptime, performance, accessibility, and isolation checks.

Capture Railway deployment/queue metrics, private worker alerts, delivery
ledger results, cost-ledger totals, demo audit output, and provider status as
small redacted evidence artifacts. Evidence must remain available after the
soak; an ignored workstation file alone is insufficient.

## Required exercises

Record all exercises in chronological order with `passed: true`, the frozen
SHA, and hashed evidence:

- two simulated provider outages;
- one redeploy while the queue has a backlog;
- one redeploy spanning a scheduled digest window;
- one missed morning run followed by exactly one in-grace catch-up;
- one isolated database restore rehearsal meeting the current RPO/RTO.

The outage and redeploy evidence must show bounded retries, no duplicate
delivery, and queue recovery. The digest-window evidence must show one durable
occurrence and one visible delivery. The restore evidence must include the
backup checksum, migration version, RPO/RTO, and redacted verification counts.

## Concluding the ledger

Record at least ten reviewed top items and the number marked useful or already
known. Record provider cost in integer cents against the reviewed budget and
hard cap. Add non-critical misses verbatim; add any security-boundary failure,
lost evidence, unbounded provider cost, or repeated critical miss to
`fatalFindings`. Then set `endedAt` and run:

```sh
make soak-status file=docs/evidence/staging/<dated-ledger>.json
make soak-validate file=docs/evidence/staging/<dated-ledger>.json
```

`soak-validate` exits successfully only for `pass`. `extend` requires more
evidence or correction of a non-critical miss. Any code or configuration fix
that changes the release SHA starts a new ledger and a new 336-hour window.
`fail` blocks promotion and requires incident review. Never delete or rewrite a
failed ledger; supersede it with a new dated ledger.

## Migration and rollback

Starting or observing the soak performs no schema migration. Hosted migration
jobs remain forward-only. If an application release is unhealthy, disable
worker intake and live delivery, roll the application back to a compatible
prior SHA, preserve the database/bucket/volume, and close the ledger as
non-passing. Restore only when corruption is proven.
