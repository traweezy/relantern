-- +goose Up
alter table app.model_configs
    add column web_search_usd_per_call numeric(12, 6) not null default 0;

alter table app.model_configs
    add constraint model_configs_web_search_price_check
    check (web_search_usd_per_call >= 0);

insert into app.model_configs (
    role,
    provider,
    model_id,
    reasoning,
    verbosity,
    enabled_tools,
    max_output_tokens,
    input_usd_per_million,
    cached_input_usd_per_million,
    output_usd_per_million,
    web_search_usd_per_call,
    enabled,
    valid_from
) values (
    'research',
    'openai',
    'gpt-5.6-terra',
    'medium',
    'low',
    array['web_search']::text[],
    8192,
    2.00,
    0.20,
    12.00,
    0.01,
    true,
    '2026-08-29T00:00:00Z'
);

alter table app.prompt_versions
    drop constraint prompt_versions_purpose_check;

alter table app.prompt_versions
    add constraint prompt_versions_purpose_check
    check (purpose in ('structured_extraction', 'research_synthesis'));

insert into app.prompt_versions (
    purpose,
    semantic_version,
    prompt_sha256,
    schema_version,
    schema_sha256,
    active,
    created_at
) values (
    'research_synthesis',
    '1.0.0',
    decode('ae1e1821c82796c2f08ecf28060175a677a82a1694b7e167c0826ba6744c5f47', 'hex'),
    '1.0.0',
    decode('230819ebb2cc281b5918c448d98e88cdd59ed000f79685ec77bef4d8d23b24a3', 'hex'),
    true,
    '2026-08-29T00:00:00Z'
);

alter table app.ai_runs
    add column cluster_id uuid;

alter table app.ai_runs
    add constraint ai_runs_cluster_id_fkey
    foreign key (cluster_id) references app.story_clusters (id) on delete restrict;

alter table app.ai_runs
    drop constraint ai_runs_purpose_check;

alter table app.ai_runs
    add constraint ai_runs_purpose_check
    check (purpose in ('structured_extraction', 'research_synthesis'));

alter table app.ai_runs
    drop constraint ai_runs_tool_calls_check;

alter table app.ai_runs
    add constraint ai_runs_tool_calls_check check (tool_calls >= 0);

alter table app.ai_runs
    add constraint ai_runs_purpose_shape_check check (
        (purpose = 'structured_extraction' and cluster_id is null and not background)
        or (purpose = 'research_synthesis' and cluster_id is not null)
    );

create index idx_ai_runs_provider_response_id_partial
    on app.ai_runs (provider_response_id)
    where provider_response_id is not null;

create index idx_ai_runs_pending_background
    on app.ai_runs (updated_at, id)
    where background and state in ('pending', 'running', 'failed_retryable');

alter table app.ai_run_attempts
    add column reserved_tool_calls integer not null default 0,
    add column tool_calls integer not null default 0;

alter table app.ai_run_attempts
    add constraint ai_run_attempts_tool_calls_check check (
        reserved_tool_calls >= 0 and tool_calls >= 0 and tool_calls <= reserved_tool_calls
    );

create table app.research_briefs (
    id uuid primary key default uuidv7(),
    cluster_id uuid not null,
    ai_run_id uuid not null,
    headline text not null,
    summary text not null,
    why_it_matters text not null,
    recommended_action text not null,
    confidence text not null,
    uncertainties jsonb not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint research_briefs_cluster_id_fkey
        foreign key (cluster_id) references app.story_clusters (id) on delete restrict,
    constraint research_briefs_ai_run_id_fkey
        foreign key (ai_run_id) references app.ai_runs (id) on delete restrict,
    constraint research_briefs_ai_run_id_key unique (ai_run_id),
    constraint research_briefs_headline_check check (length(headline) between 1 and 180),
    constraint research_briefs_summary_check check (length(summary) between 1 and 2000),
    constraint research_briefs_why_it_matters_check
        check (length(why_it_matters) between 1 and 2000),
    constraint research_briefs_recommended_action_check
        check (length(recommended_action) between 1 and 1500),
    constraint research_briefs_confidence_check
        check (confidence in ('high', 'medium', 'low', 'unknown')),
    constraint research_briefs_uncertainties_check
        check (jsonb_typeof(uncertainties) = 'array')
);

create index idx_research_briefs_cluster_created_at
    on app.research_briefs (cluster_id, created_at desc, id desc);

create table app.research_assertions (
    id uuid primary key default uuidv7(),
    research_brief_id uuid not null,
    assertion_index integer not null,
    assertion_text text not null,
    material boolean not null,
    created_at timestamptz not null default now(),
    constraint research_assertions_research_brief_id_fkey
        foreign key (research_brief_id) references app.research_briefs (id) on delete restrict,
    constraint research_assertions_index_check check (assertion_index between 0 and 24),
    constraint research_assertions_text_check check (length(assertion_text) between 1 and 1000),
    constraint research_assertions_brief_index_key
        unique (research_brief_id, assertion_index)
);

create table app.research_assertion_claims (
    research_assertion_id uuid not null,
    claim_id uuid not null,
    created_at timestamptz not null default now(),
    constraint research_assertion_claims_pkey
        primary key (research_assertion_id, claim_id),
    constraint research_assertion_claims_assertion_id_fkey
        foreign key (research_assertion_id) references app.research_assertions (id) on delete restrict,
    constraint research_assertion_claims_claim_id_fkey
        foreign key (claim_id) references app.claims (id) on delete restrict
);

create table app.research_sources (
    id uuid primary key default uuidv7(),
    ai_run_id uuid not null,
    source_url text not null,
    source_domain text not null,
    created_at timestamptz not null default now(),
    constraint research_sources_ai_run_id_fkey
        foreign key (ai_run_id) references app.ai_runs (id) on delete restrict,
    constraint research_sources_url_check check (length(source_url) between 9 and 4096),
    constraint research_sources_domain_check check (length(source_domain) between 1 and 253),
    constraint research_sources_run_url_key unique (ai_run_id, source_url)
);

create table app.research_assertion_sources (
    research_assertion_id uuid not null,
    research_source_id uuid not null,
    created_at timestamptz not null default now(),
    constraint research_assertion_sources_pkey
        primary key (research_assertion_id, research_source_id),
    constraint research_assertion_sources_assertion_id_fkey
        foreign key (research_assertion_id) references app.research_assertions (id) on delete restrict,
    constraint research_assertion_sources_source_id_fkey
        foreign key (research_source_id) references app.research_sources (id) on delete restrict
);

create table app.openai_webhook_events (
    webhook_id text primary key,
    event_id text not null,
    event_type text not null,
    response_id text not null,
    event_created_at timestamptz not null,
    received_at timestamptz not null,
    processed_at timestamptz,
    processing_error text,
    created_at timestamptz not null default now(),
    constraint openai_webhook_events_event_id_key unique (event_id),
    constraint openai_webhook_events_webhook_id_check
        check (length(webhook_id) between 1 and 255),
    constraint openai_webhook_events_event_id_check
        check (length(event_id) between 1 and 255),
    constraint openai_webhook_events_event_type_check check (
        event_type in (
            'response.completed', 'response.failed',
            'response.incomplete', 'response.cancelled'
        )
    ),
    constraint openai_webhook_events_response_id_check
        check (length(response_id) between 1 and 255),
    constraint openai_webhook_events_processed_at_check
        check (processed_at is null or processed_at >= received_at),
    constraint openai_webhook_events_processing_error_check
        check (processing_error is null or length(processing_error) between 1 and 1000)
);

create index idx_openai_webhook_events_pending
    on app.openai_webhook_events (received_at, webhook_id)
    where processed_at is null;

-- +goose Down
drop table if exists app.openai_webhook_events;
drop table if exists app.research_assertion_sources;
drop table if exists app.research_sources;
drop table if exists app.research_assertion_claims;
drop table if exists app.research_assertions;
drop table if exists app.research_briefs;
drop index if exists app.idx_ai_runs_pending_background;
drop index if exists app.idx_ai_runs_provider_response_id_partial;
delete from app.ai_run_attempts
where ai_run_id in (select id from app.ai_runs where purpose = 'research_synthesis');
delete from app.ai_runs where purpose = 'research_synthesis';
alter table app.ai_run_attempts
    drop constraint if exists ai_run_attempts_tool_calls_check,
    drop column if exists tool_calls,
    drop column if exists reserved_tool_calls;
alter table app.ai_runs drop constraint if exists ai_runs_purpose_shape_check;
alter table app.ai_runs drop constraint if exists ai_runs_cluster_id_fkey;
alter table app.ai_runs drop column if exists cluster_id;
alter table app.ai_runs drop constraint if exists ai_runs_tool_calls_check;
alter table app.ai_runs add constraint ai_runs_tool_calls_check check (tool_calls = 0);
alter table app.ai_runs drop constraint if exists ai_runs_purpose_check;
alter table app.ai_runs add constraint ai_runs_purpose_check check (purpose = 'structured_extraction');
delete from app.prompt_versions where purpose = 'research_synthesis';
alter table app.prompt_versions drop constraint if exists prompt_versions_purpose_check;
alter table app.prompt_versions
    add constraint prompt_versions_purpose_check check (purpose = 'structured_extraction');
delete from app.model_configs where role = 'research' and model_id = 'gpt-5.6-terra';
alter table app.model_configs drop constraint if exists model_configs_web_search_price_check;
alter table app.model_configs drop column if exists web_search_usd_per_call;
