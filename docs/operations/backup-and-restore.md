# Backup and restore policy

Initial objectives are RPO 24 hours and RTO four hours. After production is
trusted, hourly/PITR evidence must pass before changing the stated RPO to one
hour.

- Railway volume snapshots: daily when supported by the selected database
  plan.
- Logical backup: nightly custom-format `pg_dump`, encrypted before upload to
  an owner-controlled destination separate from Railway.
- Release marker: record a verified backup immediately before a production
  migration/deploy.
- Restore: automated monthly into an isolated temporary database; quarterly
  full disaster-recovery exercise.
- Evidence: backup checksum, release SHA, migration version, started/completed
  times, bounded table counts, RPO/RTO, and result. Never retain a password,
  connection URL, owner identity, recipient, or private content in evidence.

Local `make backup` and `make restore-drill` create ignored mode-0600 artifacts
under `dist/`. Hosted logical dumps must be encrypted in transit and at rest;
an unencrypted local dump is never uploaded or attached to a GitHub artifact.
