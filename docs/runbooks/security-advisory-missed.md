# Security advisory missed runbook

## Trigger

- A confirmed watched-dependency critical advisory was not visible within ten
  minutes of first observation or did not bypass quiet hours.

## Impact

The owner may act late on a material security issue. Ordinary ranking and
digest delivery must not hide the failure.

## Immediate containment

1. Verify the advisory from an official vendor or maintainer source; do not
   amplify an unconfirmed community claim.
2. Disable automated claims that lack primary evidence and record source,
   first-observed time, watched dependency match, and expected delivery path.
3. If delivery is unsafe, use the dashboard record and owner communication
   path without bypassing the immutable ledger.

## Exact verification commands

```sh
make sources-verify
make test-extraction
make test-scheduler
make digest-preview
docker compose logs --since=30m worker api fake-delivery
```

## Recovery

1. Correct source polling, deterministic package matching, classification,
   critical queue priority, quiet-hour bypass, or delivery configuration.
2. Reprocess the durable source revision; do not create a synthetic claim.
3. Verify one critical fixture reaches the safe capture path within target and
   one unconfirmed rumor does not.

## Data-integrity checks

- Advisory identity, affected package/range, severity, and primary source are
  preserved.
- One confirmed event maps to one urgent alert/delivery identity.
- Quiet-hour bypass is owner-enabled and applies only to confirmed criticals.

## Communication

Record verified facts, delay, owner exposure, affected dependency, remediation
status, and confidence. Link the primary advisory; do not overstate impact.

## Post-incident evidence

Retain fixture, timing trace, source revision/claim IDs, queue/delivery ledger,
root cause, and regression test.
