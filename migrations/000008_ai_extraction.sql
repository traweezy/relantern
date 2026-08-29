-- +goose Up
create table app.model_configs (
    id uuid primary key default uuidv7(),
    role text not null,
    provider text not null,
    model_id text not null,
    reasoning text not null,
    verbosity text not null,
    enabled_tools text[] not null,
    max_output_tokens integer not null,
    input_usd_per_million numeric(12, 6) not null,
    cached_input_usd_per_million numeric(12, 6) not null,
    output_usd_per_million numeric(12, 6) not null,
    enabled boolean not null,
    valid_from timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint model_configs_role_check check (role in ('fast', 'research', 'deep')),
    constraint model_configs_provider_check check (provider = 'openai'),
    constraint model_configs_model_id_check check (length(model_id) between 1 and 255),
    constraint model_configs_reasoning_check
        check (reasoning in ('none', 'low', 'medium', 'high', 'xhigh', 'max')),
    constraint model_configs_verbosity_check check (verbosity in ('low', 'medium', 'high')),
    constraint model_configs_enabled_tools_check
        check (enabled_tools <@ array['web_search']::text[]),
    constraint model_configs_max_output_tokens_check
        check (max_output_tokens between 256 and 128000),
    constraint model_configs_input_price_check check (input_usd_per_million >= 0),
    constraint model_configs_cached_input_price_check
        check (cached_input_usd_per_million >= 0),
    constraint model_configs_output_price_check check (output_usd_per_million >= 0),
    constraint model_configs_identity_key unique (role, provider, model_id, valid_from)
);

create unique index model_configs_single_enabled_role_key
    on app.model_configs (role)
    where enabled;

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
    enabled,
    valid_from
) values (
    'fast',
    'openai',
    'gpt-5.6-luna',
    'low',
    'low',
    array[]::text[],
    4096,
    0.20,
    0.02,
    1.20,
    true,
    '2026-08-29T00:00:00Z'
);

create table app.prompt_versions (
    id uuid primary key default uuidv7(),
    purpose text not null,
    semantic_version text not null,
    prompt_sha256 bytea not null,
    schema_version text not null,
    schema_sha256 bytea not null,
    active boolean not null,
    created_at timestamptz not null default now(),
    constraint prompt_versions_purpose_check check (purpose = 'structured_extraction'),
    constraint prompt_versions_semantic_version_check
        check (semantic_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
    constraint prompt_versions_schema_version_check
        check (schema_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
    constraint prompt_versions_prompt_sha256_check check (octet_length(prompt_sha256) = 32),
    constraint prompt_versions_schema_sha256_check check (octet_length(schema_sha256) = 32),
    constraint prompt_versions_identity_key unique (purpose, semantic_version)
);

create unique index prompt_versions_single_active_purpose_key
    on app.prompt_versions (purpose)
    where active;

insert into app.prompt_versions (
    purpose,
    semantic_version,
    prompt_sha256,
    schema_version,
    schema_sha256,
    active,
    created_at
) values (
    'structured_extraction',
    '1.0.0',
    decode('4de559117dffa18cca7cfb10137144157bdb31086ab10799f2bb3e73f0b298da', 'hex'),
    '1.0.0',
    decode('33e696bcb118ddccce0277524b14a2bf1480002d507d84e15d409b1deb0fe309', 'hex'),
    true,
    '2026-08-29T00:00:00Z'
);

create table app.ai_runs (
    id uuid primary key default uuidv7(),
    item_id uuid not null,
    revision_id uuid not null,
    purpose text not null,
    model_config_id uuid not null,
    prompt_version_id uuid not null,
    provider_response_id text,
    background boolean not null default false,
    state text not null,
    input_sha256 bytea not null,
    validated_output jsonb,
    input_tokens bigint not null default 0,
    cached_input_tokens bigint not null default 0,
    output_tokens bigint not null default 0,
    tool_calls integer not null default 0,
    attempt_count integer not null default 0,
    estimated_cost_usd numeric(14, 8) not null default 0,
    started_at timestamptz not null,
    completed_at timestamptz,
    error_code text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint ai_runs_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint ai_runs_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint ai_runs_model_config_id_fkey
        foreign key (model_config_id) references app.model_configs (id) on delete restrict,
    constraint ai_runs_prompt_version_id_fkey
        foreign key (prompt_version_id) references app.prompt_versions (id) on delete restrict,
    constraint ai_runs_purpose_check check (purpose = 'structured_extraction'),
    constraint ai_runs_state_check check (
        state in (
            'pending', 'running', 'completed', 'needs_review',
            'failed_retryable', 'budget_blocked', 'obsolete'
        )
    ),
    constraint ai_runs_input_sha256_check check (octet_length(input_sha256) = 32),
    constraint ai_runs_validated_output_check
        check (validated_output is null or jsonb_typeof(validated_output) = 'object'),
    constraint ai_runs_token_counts_check check (
        input_tokens >= 0 and cached_input_tokens >= 0
        and cached_input_tokens <= input_tokens and output_tokens >= 0
    ),
    constraint ai_runs_tool_calls_check check (tool_calls = 0),
    constraint ai_runs_attempt_count_check check (attempt_count >= 0),
    constraint ai_runs_estimated_cost_check check (estimated_cost_usd >= 0),
    constraint ai_runs_completed_at_check
        check (completed_at is null or completed_at >= started_at),
    constraint ai_runs_error_code_check
        check (error_code is null or length(error_code) between 1 and 100),
    constraint ai_runs_identity_key unique (
        item_id, revision_id, purpose, model_config_id, prompt_version_id
    )
);

create index idx_ai_runs_state_started_at on app.ai_runs (state, started_at, id);
create index idx_ai_runs_item_started_at on app.ai_runs (item_id, started_at desc, id desc);

create table app.ai_run_attempts (
    id bigint generated always as identity primary key,
    ai_run_id uuid not null,
    attempt_number integer not null,
    state text not null,
    provider_response_id text,
    input_tokens bigint not null default 0,
    cached_input_tokens bigint not null default 0,
    output_tokens bigint not null default 0,
    reserved_cost_usd numeric(14, 8) not null,
    estimated_cost_usd numeric(14, 8) not null,
    budget_soft_alert boolean not null default false,
    started_at timestamptz not null,
    completed_at timestamptz,
    error_code text,
    created_at timestamptz not null default now(),
    constraint ai_run_attempts_ai_run_id_fkey
        foreign key (ai_run_id) references app.ai_runs (id) on delete restrict,
    constraint ai_run_attempts_attempt_number_check check (attempt_number >= 1),
    constraint ai_run_attempts_state_check
        check (state in ('reserved', 'completed', 'failed')),
    constraint ai_run_attempts_token_counts_check check (
        input_tokens >= 0 and cached_input_tokens >= 0
        and cached_input_tokens <= input_tokens and output_tokens >= 0
    ),
    constraint ai_run_attempts_reserved_cost_check check (reserved_cost_usd >= 0),
    constraint ai_run_attempts_estimated_cost_check check (estimated_cost_usd >= 0),
    constraint ai_run_attempts_completed_at_check
        check (completed_at is null or completed_at >= started_at),
    constraint ai_run_attempts_error_code_check
        check (error_code is null or length(error_code) between 1 and 100),
    constraint ai_run_attempts_run_number_key unique (ai_run_id, attempt_number)
);

create index idx_ai_run_attempts_started_at
    on app.ai_run_attempts (started_at, ai_run_id);

create table app.claims (
    id uuid primary key default uuidv7(),
    item_id uuid not null,
    revision_id uuid not null,
    ai_run_id uuid not null,
    claim_index integer not null,
    claim_type text not null,
    claim_text text not null,
    normalized_value jsonb not null,
    confidence text not null,
    material boolean not null,
    verification_state text not null,
    created_at timestamptz not null default now(),
    constraint claims_item_id_fkey
        foreign key (item_id) references app.items (id) on delete restrict,
    constraint claims_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint claims_ai_run_id_fkey
        foreign key (ai_run_id) references app.ai_runs (id) on delete restrict,
    constraint claims_claim_index_check check (claim_index between 0 and 49),
    constraint claims_claim_type_check check (
        claim_type in (
            'release', 'version', 'date', 'breaking_change', 'deprecation',
            'security', 'migration_prerequisite', 'example', 'compatibility', 'general'
        )
    ),
    constraint claims_claim_text_check check (length(claim_text) between 1 and 1000),
    constraint claims_normalized_value_check
        check (jsonb_typeof(normalized_value) = 'string'),
    constraint claims_confidence_check check (confidence in ('high', 'medium', 'low', 'unknown')),
    constraint claims_verification_state_check
        check (verification_state in ('verified_span', 'review_required')),
    constraint claims_run_index_key unique (ai_run_id, claim_index)
);

create index idx_claims_item_id on app.claims (item_id, created_at desc, id desc);
create index idx_claims_revision_id on app.claims (revision_id, claim_index);

create table app.evidence_spans (
    id uuid primary key default uuidv7(),
    claim_id uuid not null,
    revision_id uuid not null,
    span_identifier text not null,
    section_path text not null,
    start_offset integer not null,
    end_offset integer not null,
    quote_hash bytea not null,
    created_at timestamptz not null default now(),
    constraint evidence_spans_claim_id_fkey
        foreign key (claim_id) references app.claims (id) on delete restrict,
    constraint evidence_spans_revision_id_fkey
        foreign key (revision_id) references app.content_revisions (id) on delete restrict,
    constraint evidence_spans_identifier_check check (span_identifier ~ '^span_[0-9]{4}$'),
    constraint evidence_spans_section_path_check check (length(section_path) between 1 and 1000),
    constraint evidence_spans_offsets_check
        check (start_offset >= 0 and end_offset > start_offset),
    constraint evidence_spans_quote_hash_check check (octet_length(quote_hash) = 32),
    constraint evidence_spans_claim_identifier_key unique (claim_id, span_identifier)
);

create index idx_evidence_spans_revision_offsets
    on app.evidence_spans (revision_id, start_offset, end_offset);

create table app.eval_cases (
    id uuid primary key default uuidv7(),
    suite text not null,
    input_fixture text not null,
    expected jsonb not null,
    source text not null,
    active boolean not null,
    created_at timestamptz not null default now(),
    constraint eval_cases_suite_check check (length(suite) between 1 and 100),
    constraint eval_cases_input_fixture_check check (length(input_fixture) between 1 and 1000),
    constraint eval_cases_expected_check check (jsonb_typeof(expected) = 'object'),
    constraint eval_cases_source_check check (length(source) between 1 and 1000),
    constraint eval_cases_identity_key unique (suite, input_fixture)
);

create table app.eval_runs (
    id uuid primary key default uuidv7(),
    git_sha text not null,
    model_config_id uuid not null,
    prompt_version_id uuid not null,
    metrics jsonb not null,
    result text not null,
    created_at timestamptz not null default now(),
    constraint eval_runs_model_config_id_fkey
        foreign key (model_config_id) references app.model_configs (id) on delete restrict,
    constraint eval_runs_prompt_version_id_fkey
        foreign key (prompt_version_id) references app.prompt_versions (id) on delete restrict,
    constraint eval_runs_git_sha_check check (git_sha ~ '^[0-9a-f]{7,64}$'),
    constraint eval_runs_metrics_check check (jsonb_typeof(metrics) = 'object'),
    constraint eval_runs_result_check check (result in ('pass', 'fail'))
);

create index idx_eval_runs_created_at on app.eval_runs (created_at desc, id desc);

-- +goose Down
drop table if exists app.eval_runs;
drop table if exists app.eval_cases;
drop table if exists app.evidence_spans;
drop table if exists app.claims;
drop table if exists app.ai_run_attempts;
drop table if exists app.ai_runs;
drop table if exists app.prompt_versions;
drop table if exists app.model_configs;
