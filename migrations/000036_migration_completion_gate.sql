-- +goose Up
set local lock_timeout = '5s';

-- The one-shot migrator writes this only after Goose, River, and source
-- registry synchronization have all succeeded for the immutable release.
-- It is intentionally separate from Goose's per-file version history.
create table app.migration_completions (
    release_sha text not null,
    goose_version bigint not null,
    completed_at timestamptz not null default now(),
    constraint migration_completions_pkey primary key (release_sha, goose_version),
    constraint migration_completions_release_sha_check check (
        release_sha = 'unknown' or release_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'
    ),
    constraint migration_completions_goose_version_check check (goose_version > 0)
);
