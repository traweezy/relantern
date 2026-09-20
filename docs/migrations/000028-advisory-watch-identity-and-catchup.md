# Advisory watch identity and catch-up migration 000028

Migration `000028_watched_technology_ecosystem_identity.sql` replaces the
package-only watched-technology uniqueness rule with
`(user_id, ecosystem, package_name) NULLS NOT DISTINCT`. This permits the
same package name in different advisory ecosystems while preserving one
untyped legacy row per owner and package. It builds the replacement unique
and active-watch lookup indexes concurrently before taking a short lock to
attach the constraint and release the old one. A five-second lock timeout
keeps deployment from waiting indefinitely behind long transactions. If an
interrupted concurrent index build leaves an invalid index, inspect and drop
that invalid index concurrently before retrying the migration.

Deploy migration 28 before the API and worker code that accepts same-name
cross-ecosystem watches. Saving a newly active typed watch, or changing its
version, enqueues a transactional River catch-up scan. The scan pages through
existing current official advisory evidence, including supporting revisions,
and enqueues the normal alert assessor on the separate `advisory_backfill`
queue. It stops scanning when no active typed watch remains. No legacy row is
automatically assigned an ecosystem, and an untyped row never triggers an
alert.

Monitor the `advisory_backfill` queue depth, catch-up page completion/failure
logs, and critical assessment outcomes after rollout. The scanner uses a
16-revision page and a UUID keyset cursor with no fixed total cap. If a page
exhausts River retries, investigate the error and change the typed watch's
version to create a new settings-version scan. Alert creation remains idempotent by
owner, advisory, ecosystem, and package identity.
