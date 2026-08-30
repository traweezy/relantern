-- +goose Up
alter table app.owner_settings
    alter column raw_retention_days set default 180,
    alter column audit_retention_days set default 30;

update app.owner_settings
set raw_retention_days = 180,
    audit_retention_days = 30,
    version = version + 1,
    updated_at = now()
where raw_retention_days = 90 and audit_retention_days = 365;

alter table app.raw_documents
    add column raw_pruned_at timestamptz;

alter table app.raw_documents
    drop constraint raw_documents_object_policy_check,
    add constraint raw_documents_object_policy_check check (
        (content_policy = 'metadata-only' and object_key is null and raw_pruned_at is null)
        or (
            content_policy = 'link-and-excerpt'
            and (
                (object_key is not null and raw_pruned_at is null)
                or (object_key is null and raw_pruned_at is not null)
            )
        )
    );

create index idx_raw_documents_retention_partial
    on app.raw_documents (first_seen_at, id)
    where object_key is not null;

alter table app.content_revisions
    alter column normalized_text_object_key drop not null,
    add column normalized_text_pruned_at timestamptz,
    add constraint content_revisions_normalized_object_policy_check check (
        (normalized_text_object_key is not null and normalized_text_pruned_at is null)
        or (normalized_text_object_key is null and normalized_text_pruned_at is not null)
    );

create index idx_content_revisions_retention_partial
    on app.content_revisions (observed_at, id)
    where normalized_text_object_key is not null;

alter table app.item_state_mutations
    alter column before_state drop not null,
    alter column after_state drop not null,
    drop constraint item_state_mutations_before_state_check,
    drop constraint item_state_mutations_after_state_check,
    add column compacted_at timestamptz,
    add constraint item_state_mutations_snapshot_policy_check check (
        (
            before_state is not null
            and after_state is not null
            and jsonb_typeof(before_state) = 'object'
            and jsonb_typeof(after_state) = 'object'
            and compacted_at is null
        )
        or (before_state is null and after_state is null and compacted_at is not null)
    );

create table app.retention_runs (
    id uuid primary key default uuidv7(),
    idempotency_key text not null,
    state text not null,
    policy jsonb not null,
    counts jsonb not null default '{}'::jsonb,
    started_at timestamptz not null,
    completed_at timestamptz,
    error_code text,
    created_at timestamptz not null default now(),
    constraint retention_runs_idempotency_key_key unique (idempotency_key),
    constraint retention_runs_idempotency_key_check
        check (idempotency_key ~ '^retention:[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    constraint retention_runs_state_check check (state in ('running', 'completed', 'failed')),
    constraint retention_runs_policy_check check (jsonb_typeof(policy) = 'object'),
    constraint retention_runs_counts_check check (jsonb_typeof(counts) = 'object'),
    constraint retention_runs_error_code_check
        check (error_code is null or length(error_code) between 1 and 100),
    constraint retention_runs_completion_check check (
        (state = 'running' and completed_at is null and error_code is null)
        or (state = 'completed' and completed_at is not null and error_code is null)
        or (state = 'failed' and completed_at is not null and error_code is not null)
    )
);

create index idx_retention_runs_started_at on app.retention_runs (started_at desc, id desc);

create table app.restore_drills (
    id uuid primary key default uuidv7(),
    backup_sha256 bytea not null,
    release_git_sha text not null,
    state text not null,
    rpo_seconds bigint not null,
    rto_seconds bigint not null,
    rpo_target_seconds bigint not null,
    rto_target_seconds bigint not null,
    restored_migration_version bigint not null,
    verification_counts jsonb not null,
    started_at timestamptz not null,
    completed_at timestamptz not null,
    created_at timestamptz not null default now(),
    constraint restore_drills_backup_sha256_check check (octet_length(backup_sha256) = 32),
    constraint restore_drills_release_git_sha_check check (release_git_sha ~ '^[0-9a-f]{7,64}$'),
    constraint restore_drills_state_check check (state in ('passed', 'failed')),
    constraint restore_drills_rpo_check check (rpo_seconds >= 0 and rpo_target_seconds > 0),
    constraint restore_drills_rto_check check (rto_seconds >= 0 and rto_target_seconds > 0),
    constraint restore_drills_migration_version_check check (restored_migration_version > 0),
    constraint restore_drills_verification_counts_check check (jsonb_typeof(verification_counts) = 'object'),
    constraint restore_drills_completed_at_check check (completed_at >= started_at),
    constraint restore_drills_backup_started_key unique (backup_sha256, started_at)
);

create index idx_restore_drills_completed_at on app.restore_drills (completed_at desc, id desc);

-- +goose Down
do $$
begin
    if exists (select 1 from app.raw_documents where raw_pruned_at is not null)
        or exists (select 1 from app.content_revisions where normalized_text_pruned_at is not null)
        or exists (select 1 from app.item_state_mutations where compacted_at is not null) then
        raise exception 'refusing to reverse operational hardening after retention changed data';
    end if;
end $$;

drop table if exists app.restore_drills;
drop table if exists app.retention_runs;
alter table app.item_state_mutations
    drop constraint if exists item_state_mutations_snapshot_policy_check,
    drop column if exists compacted_at,
    alter column before_state set not null,
    alter column after_state set not null,
    add constraint item_state_mutations_before_state_check check (jsonb_typeof(before_state) = 'object'),
    add constraint item_state_mutations_after_state_check check (jsonb_typeof(after_state) = 'object');
drop index if exists app.idx_content_revisions_retention_partial;
alter table app.content_revisions
    drop constraint if exists content_revisions_normalized_object_policy_check,
    drop column if exists normalized_text_pruned_at,
    alter column normalized_text_object_key set not null;
drop index if exists app.idx_raw_documents_retention_partial;
alter table app.raw_documents
    drop constraint if exists raw_documents_object_policy_check,
    drop column if exists raw_pruned_at,
    add constraint raw_documents_object_policy_check
        check ((content_policy = 'link-and-excerpt' and object_key is not null) or (content_policy = 'metadata-only' and object_key is null));
alter table app.owner_settings
    alter column raw_retention_days set default 90,
    alter column audit_retention_days set default 365;
