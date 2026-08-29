-- +goose Up
alter table app.users
    add column email text,
    add column email_verified boolean not null default false,
    add column image_url text;

update app.users
set display_name = coalesce(display_name, login),
    email = github_user_id::text || '@github.relantern.local',
    email_verified = true
where email is null;

alter table app.users
    alter column email set not null,
    alter column display_name set not null;

alter table app.users
    add constraint users_email_key unique (email),
    add constraint users_email_check
        check (length(email) between 3 and 320),
    add constraint users_image_url_check
        check (image_url is null or length(image_url) between 1 and 2048);

create table app.auth_sessions (
    id uuid primary key default uuidv7(),
    expires_at timestamptz not null,
    token text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    ip_address text,
    user_agent text,
    user_id uuid not null,
    constraint auth_sessions_token_key unique (token),
    constraint auth_sessions_token_check check (length(token) between 32 and 512),
    constraint auth_sessions_ip_address_check
        check (ip_address is null or length(ip_address) between 1 and 64),
    constraint auth_sessions_user_agent_check
        check (user_agent is null or length(user_agent) between 1 and 1024),
    constraint auth_sessions_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade
);

create index idx_auth_sessions_user_id on app.auth_sessions (user_id);
create index idx_auth_sessions_expires_at on app.auth_sessions (expires_at);

create table app.auth_accounts (
    id uuid primary key default uuidv7(),
    issuer text not null,
    account_id text not null,
    provider_id text not null,
    user_id uuid not null,
    access_token text,
    refresh_token text,
    id_token text,
    access_token_expires_at timestamptz,
    refresh_token_expires_at timestamptz,
    scope text,
    password text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint auth_accounts_issuer_account_id_key unique (issuer, account_id),
    constraint auth_accounts_issuer_check check (length(issuer) between 1 and 512),
    constraint auth_accounts_account_id_check check (length(account_id) between 1 and 512),
    constraint auth_accounts_provider_id_check check (length(provider_id) between 1 and 120),
    constraint auth_accounts_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade
);

create index idx_auth_accounts_user_id on app.auth_accounts (user_id);

create table app.auth_verifications (
    id uuid primary key default uuidv7(),
    identifier text not null,
    value text not null,
    expires_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint auth_verifications_identifier_check
        check (length(identifier) between 1 and 1024),
    constraint auth_verifications_value_check
        check (length(value) between 1 and 8192)
);

create index idx_auth_verifications_identifier
    on app.auth_verifications (identifier);
create index idx_auth_verifications_expires_at
    on app.auth_verifications (expires_at);

create table app.auth_rate_limits (
    id uuid primary key default uuidv7(),
    key text not null,
    count integer not null,
    last_request bigint not null,
    constraint auth_rate_limits_key_key unique (key),
    constraint auth_rate_limits_key_check check (length(key) between 1 and 1024),
    constraint auth_rate_limits_count_check check (count >= 0),
    constraint auth_rate_limits_last_request_check check (last_request >= 0)
);

-- +goose Down
drop table if exists app.auth_rate_limits;
drop table if exists app.auth_verifications;
drop table if exists app.auth_accounts;
drop table if exists app.auth_sessions;

alter table app.users
    drop constraint if exists users_image_url_check,
    drop constraint if exists users_email_check,
    drop constraint if exists users_email_key,
    drop column if exists image_url,
    drop column if exists email_verified,
    drop column if exists email,
    alter column display_name drop not null;
