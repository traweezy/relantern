# Discovery and portability migration 000016

## Scope

Migration `000016_discovery_portability.sql` additively creates four
owner-scoped tables:

- `app.manual_captures` records idempotent URL capture progress and the source,
  endpoint, item, and story-cluster lineage produced by the normal pipeline.
- `app.source_import_previews` stores a bounded OPML digest and candidate list
  for 30 minutes so approval cannot be substituted or replayed.
- `app.saved_searches` stores private named queries and exact filters without
  caching results.
- `app.watched_technologies` stores the owner version baseline consumed by the
  release matrix and the later Settings slice.

The migration does not enable a source, add public traffic, backfill data, or
grant an external provider permission.

## Forward rollout

1. Apply migration 16 before the API, worker, or web change receives traffic.
2. Verify all four tables and their owner/time indexes exist.
3. Run `make test-integration` against the migrated database.
4. Deploy the private API and worker before web so capture jobs and download
   routes exist when the controls become visible.
5. Submit one local fixture capture and one OPML preview. Confirm the captured
   and imported sources remain `origin = 'owner'`, `validation_state =
   'pending'`, and `enabled = false`.
6. Observe manual-capture failures, River retry/discard counts, API request
   latency, PostgreSQL lock waits, and object-storage errors.

## Rollback

Hosted rollback is application-first and forward-only. Remove the PR 14 web
controls, API routes, and worker registration while retaining migration 16 and
its owner data. Correct schema defects with another forward migration.

The Goose `down` block is only for a disposable development database. It drops
capture history, import previews, saved searches, and watched-version settings;
it does not remove source, endpoint, item, or story records already created by
a successful capture/import. Never run it in production.

## Variables and permissions

No variable or external permission is added. The feature reuses the private
web-to-API service credential, owner session, application database, bounded
fetch policy, MinIO object storage, and existing fake provider in the default
local profile. Live OpenAI, GitHub write, Discord, Resend, and Railway delivery
access remain disconnected.
