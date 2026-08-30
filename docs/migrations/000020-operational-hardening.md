# Operational hardening migration 000020

## Scope

Migration 20 adds retention and restore-drill ledgers, evidence-aware object
pruning markers, mutation-snapshot compaction, and retention indexes. It moves
new-owner defaults to 180 days for raw snapshots and 30 days for full mutation
snapshots; rows still on the former untouched 90/365 pair are migrated to the
approved defaults.

## Forward rollout

1. Keep all live provider and delivery fuses disabled.
2. Apply migration 20 before starting the updated worker or API.
3. Start the worker and confirm one `run_retention` job completes.
4. Inspect private `/metrics` and `/alerts`; confirm no URL, title, provider ID,
   recipient, or credential labels appear.
5. Run `make backup` and `make restore-drill`; retain the checksum and redacted
   report outside the application database.
6. Configure encrypted off-platform backup storage and monthly restore
   automation in staging before the soak clock starts.

## Rollback

Hosted rollback is application-first and forward-only. Keep migration 20 and
its ledgers, deploy the prior application, and leave the retention job disabled
until corrected code is forward-deployed. Never restore merely to roll back
code.

The Goose down block is local/test only and refuses reversal after retention
has pruned an object or compacted mutation state. Deleted content cannot be
reconstructed by a schema rollback.

## Variables and permissions

No runtime variable or external permission is added. The worker reuses its
private PostgreSQL and object-storage roles; its object role must delete only
the configured private bucket. Hosted backup automation separately requires a
write-only encrypted destination credential and must not reuse application,
database-owner, or production-provider credentials.
