# Advisory watch routing migration 000026

Migration `000026_watched_technology_ecosystem.sql` adds a nullable advisory
ecosystem to each watched technology. The accepted values match GitHub's global
security advisory ecosystems. Existing rows remain `NULL`: their display names
and package names are insufficient to establish a registry identity. Critical
advisory matching must skip those rows until the owner selects an ecosystem in
Settings. A row with `other` is valid for storage, but matching still requires
an exact advisory ecosystem and package identity. Urgent matching initially
supports Go, npm, Rust, and pub with simple numeric version ranges; other
ecosystems can be recorded but do not trigger urgent alerts yet.

The migration also adds `owner_settings.critical_alert_channels`, independent
of digest schedule channels. Existing and new owners default to
`["dashboard"]`. Dashboard is mandatory; the database and API accept a
duplicate-free subset of `discord` and `email` alongside it. External channels
remain subject to the environment's existing delivery fuses and credentials.

Apply migration 26 before deploying the API, worker, and web changes that read
these columns. Verify the owner Settings page shows legacy ecosystems blank and
critical channels set to Dashboard; save a typed watch and confirm it retains
its ecosystem. Monitor settings validation failures and critical advisory
match/delivery counts after rollout. Roll back application code first while
retaining the additive schema; repair any schema issue with a later forward
migration. Do not infer ecosystems or backfill legacy rows automatically.
