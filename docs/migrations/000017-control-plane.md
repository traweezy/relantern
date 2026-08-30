# Owner control-plane migration 000017

## Scope

Migration `000017_control_plane.sql` additively introduces the private owner
control plane:

- `app.interest_profiles` and `app.interest_topics` hold versioned ranking
  intent independently from source polling configuration.
- `app.user_source_preferences` holds owner mute, digest exclusion, and bounded
  relevance adjustments without changing the reviewed source registry.
- `app.owner_settings` holds quiet hours, monthly AI budgets, and retention
  targets.
- `app.source_validation_runs` records deterministic owner-source checks before
  approval.
- `app.watched_technologies` gains explicit constraint, lifecycle, provenance,
  and verification fields.
- `app.schedule_definitions` gains digest policy, channel, timed-pause, and
  optimistic-version fields.

All new tables are owner-scoped through foreign keys with cascade cleanup.
Migration constraints enforce bounded weights, budgets, retention, schedule
policy, channel allowlists, and valid pause state. No source, provider, or
external delivery is enabled by the migration.

## Forward rollout

1. Apply migration 17 before the API, worker, or web change receives traffic.
2. Run the idempotent seed to create local owner profile, settings, current
   stack, and schedule defaults without replacing owner-modified rows.
3. Run `make test-integration`; this exercises optimistic updates, audited
   source actions, idempotent Run now previews, DST schedule calculation, and
   automatic expiry of timed pauses.
4. Deploy the private API and worker before web so every visible control has a
   durable command target.
5. Confirm imported owner sources remain `enabled = false` after deterministic
   validation and approval. Network activation remains a separate rollout.
6. Observe API error rate and latency, source validation failures, scheduler
   last-success age, oldest overdue occurrence, and River queue depth.

## Rollback

Hosted rollback is application-first and forward-only. Roll back the web, API,
and worker while retaining migration 17 and all owner preferences. Correct a
schema defect with a new additive migration; do not remove control-plane state
from a hosted database.

The Goose `down` block exists only for a disposable development database. It
removes the PR15 preference, validation, and settings data and contracts the
new schedule/technology columns. Never run it in staging or production.

## Variables and permissions

No variable or external permission is added by migration 17. Deployment
metadata uses the existing environment/version/SHA configuration. The feature
reuses the private web-to-API service credential and PostgreSQL role. Source
tests are configuration-only and perform no network I/O. PR17 later amends Run
now so external delivery requires both `deliver=true` and the environment fuse.
