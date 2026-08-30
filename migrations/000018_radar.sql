-- +goose Up
create table app.radar_discovery_runs (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    trigger_type text not null,
    state text not null default 'queued',
    idempotency_key text not null,
    evidence_count integer not null default 0,
    candidate_count integer not null default 0,
    misleading_count integer not null default 0,
    error_code text,
    requested_at timestamptz not null,
    started_at timestamptz,
    completed_at timestamptz,
    river_job_id bigint,
    constraint radar_discovery_runs_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint radar_discovery_runs_trigger_type_check
        check (trigger_type in ('scheduled', 'owner')),
    constraint radar_discovery_runs_state_check
        check (state in ('queued', 'running', 'completed', 'failed')),
    constraint radar_discovery_runs_idempotency_key_check
        check (length(idempotency_key) between 16 and 200),
    constraint radar_discovery_runs_count_check check (
        evidence_count >= 0 and candidate_count >= 0 and misleading_count >= 0
        and misleading_count <= evidence_count
    ),
    constraint radar_discovery_runs_error_code_check
        check (error_code is null or length(error_code) between 1 and 120),
    constraint radar_discovery_runs_completed_at_check
        check (completed_at is null or (started_at is not null and completed_at >= started_at)),
    constraint radar_discovery_runs_user_idempotency_key_key
        unique (user_id, idempotency_key),
    constraint radar_discovery_runs_river_job_id_key unique (river_job_id)
);

create index idx_radar_discovery_runs_user_requested_at
    on app.radar_discovery_runs (user_id, requested_at desc, id desc);

create table app.package_candidate_evidence (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    ecosystem text not null,
    package_name text not null,
    repository_url text not null,
    discovery_source text not null,
    incumbent_package text not null,
    stable_release text not null,
    license text not null,
    contributor_count integer not null,
    release_cadence_days integer,
    issue_response_days integer,
    security_response_days integer,
    security_advisory_count integer not null default 0,
    critical_advisory_count integer not null default 0,
    scorecard_score numeric(4, 2),
    signed_releases boolean not null default false,
    provenance_verified boolean not null default false,
    types_supported boolean not null default false,
    bundle_size_bytes bigint,
    runtime_compatibility text[] not null default array[]::text[],
    project_types text[] not null default array[]::text[],
    compatibility_requirements text[] not null default array[]::text[],
    exit_conditions text[] not null default array[]::text[],
    maintenance_signals jsonb not null default '{}'::jsonb,
    security_signals jsonb not null default '{}'::jsonb,
    popularity_signals jsonb not null default '{}'::jsonb,
    evidence_links jsonb not null default '[]'::jsonb,
    observed_at timestamptz not null,
    processed_at timestamptz,
    created_at timestamptz not null default now(),
    constraint package_candidate_evidence_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint package_candidate_evidence_ecosystem_check
        check (ecosystem in ('go', 'npm', 'python', 'rust', 'jvm', 'other')),
    constraint package_candidate_evidence_package_name_check
        check (length(package_name) between 1 and 255),
    constraint package_candidate_evidence_repository_url_check
        check (repository_url ~ '^https://'),
    constraint package_candidate_evidence_discovery_source_check
        check (length(discovery_source) between 1 and 120),
    constraint package_candidate_evidence_incumbent_package_check
        check (length(incumbent_package) between 1 and 255),
    constraint package_candidate_evidence_stable_release_check
        check (length(stable_release) between 1 and 100),
    constraint package_candidate_evidence_license_check
        check (length(license) between 1 and 100),
    constraint package_candidate_evidence_metric_ranges_check check (
        contributor_count >= 0
        and (release_cadence_days is null or release_cadence_days >= 0)
        and (issue_response_days is null or issue_response_days >= 0)
        and (security_response_days is null or security_response_days >= 0)
        and security_advisory_count >= 0
        and critical_advisory_count between 0 and security_advisory_count
        and (scorecard_score is null or scorecard_score between 0 and 10)
        and (bundle_size_bytes is null or bundle_size_bytes >= 0)
    ),
    constraint package_candidate_evidence_array_bounds_check check (
        cardinality(runtime_compatibility) <= 50
        and cardinality(project_types) between 1 and 20
        and cardinality(compatibility_requirements) between 1 and 50
        and cardinality(exit_conditions) between 1 and 50
    ),
    constraint package_candidate_evidence_json_shapes_check check (
        jsonb_typeof(maintenance_signals) = 'object'
        and jsonb_typeof(security_signals) = 'object'
        and jsonb_typeof(popularity_signals) = 'object'
        and jsonb_typeof(evidence_links) = 'array'
    )
);

create index idx_package_candidate_evidence_pending
    on app.package_candidate_evidence (user_id, observed_at, id)
    where processed_at is null;

create table app.package_candidates (
    id uuid primary key default uuidv7(),
    user_id uuid not null,
    ecosystem text not null,
    package_name text not null,
    repository_url text not null,
    discovered_at timestamptz not null,
    discovery_source text not null,
    current_status text not null default 'assess',
    incumbent_package text not null,
    review_at timestamptz not null,
    version bigint not null default 1,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint package_candidates_user_id_fkey
        foreign key (user_id) references app.users (id) on delete cascade,
    constraint package_candidates_ecosystem_check
        check (ecosystem in ('go', 'npm', 'python', 'rust', 'jvm', 'other')),
    constraint package_candidates_package_name_check
        check (length(package_name) between 1 and 255),
    constraint package_candidates_repository_url_check check (repository_url ~ '^https://'),
    constraint package_candidates_discovery_source_check
        check (length(discovery_source) between 1 and 120),
    constraint package_candidates_current_status_check
        check (current_status in ('adopt', 'trial', 'assess', 'hold', 'reject')),
    constraint package_candidates_incumbent_package_check
        check (length(incumbent_package) between 1 and 255),
    constraint package_candidates_review_at_check check (review_at >= discovered_at),
    constraint package_candidates_version_check check (version > 0),
    constraint package_candidates_user_ecosystem_package_key
        unique (user_id, ecosystem, package_name)
);

create index idx_package_candidates_user_status_review_at
    on app.package_candidates (user_id, current_status, review_at, id);

create table app.package_metrics (
    id uuid primary key default uuidv7(),
    candidate_id uuid not null,
    evidence_id uuid not null,
    observed_at timestamptz not null,
    release_version text not null,
    license text not null,
    contributor_count integer not null,
    runtime_compatibility text[] not null,
    types_supported boolean not null,
    bundle_size_bytes bigint,
    maintenance_metrics jsonb not null,
    security_metrics jsonb not null,
    popularity_metrics jsonb not null,
    provenance_metrics jsonb not null,
    constraint package_metrics_candidate_id_fkey
        foreign key (candidate_id) references app.package_candidates (id) on delete cascade,
    constraint package_metrics_evidence_id_fkey
        foreign key (evidence_id) references app.package_candidate_evidence (id) on delete restrict,
    constraint package_metrics_release_version_check
        check (length(release_version) between 1 and 100),
    constraint package_metrics_license_check check (length(license) between 1 and 100),
    constraint package_metrics_count_check check (
        contributor_count >= 0 and (bundle_size_bytes is null or bundle_size_bytes >= 0)
    ),
    constraint package_metrics_json_shapes_check check (
        jsonb_typeof(maintenance_metrics) = 'object'
        and jsonb_typeof(security_metrics) = 'object'
        and jsonb_typeof(popularity_metrics) = 'object'
        and jsonb_typeof(provenance_metrics) = 'object'
    ),
    constraint package_metrics_candidate_evidence_key unique (candidate_id, evidence_id)
);

create index idx_package_metrics_candidate_observed_at
    on app.package_metrics (candidate_id, observed_at desc, id desc);

create table app.radar_comparisons (
    id uuid primary key default uuidv7(),
    candidate_id uuid not null,
    metric_id uuid not null,
    suggested_state text not null,
    confidence numeric(5, 4) not null,
    misleading boolean not null,
    dimensions jsonb not null,
    evidence_links jsonb not null,
    assessed_at timestamptz not null,
    constraint radar_comparisons_candidate_id_fkey
        foreign key (candidate_id) references app.package_candidates (id) on delete cascade,
    constraint radar_comparisons_metric_id_fkey
        foreign key (metric_id) references app.package_metrics (id) on delete cascade,
    constraint radar_comparisons_suggested_state_check
        check (suggested_state in ('assess', 'hold', 'reject')),
    constraint radar_comparisons_confidence_check check (confidence between 0 and 1),
    constraint radar_comparisons_json_shapes_check check (
        jsonb_typeof(dimensions) = 'array'
        and jsonb_array_length(dimensions) = 7
        and jsonb_typeof(evidence_links) = 'array'
    ),
    constraint radar_comparisons_metric_id_key unique (metric_id)
);

create index idx_radar_comparisons_candidate_assessed_at
    on app.radar_comparisons (candidate_id, assessed_at desc, id desc);

create table app.radar_decisions (
    id uuid primary key default uuidv7(),
    candidate_id uuid not null,
    owner_user_id uuid not null,
    state text not null,
    decision_source text not null,
    rationale text not null,
    evidence jsonb not null,
    decided_at timestamptz not null,
    review_at timestamptz not null,
    applicable_project_types text[] not null,
    compatibility_requirements text[] not null,
    exit_conditions text[] not null,
    created_at timestamptz not null default now(),
    constraint radar_decisions_candidate_id_fkey
        foreign key (candidate_id) references app.package_candidates (id) on delete cascade,
    constraint radar_decisions_owner_user_id_fkey
        foreign key (owner_user_id) references app.users (id) on delete cascade,
    constraint radar_decisions_state_check
        check (state in ('adopt', 'trial', 'assess', 'hold', 'reject')),
    constraint radar_decisions_source_check
        check (decision_source in ('owner', 'system')),
    constraint radar_decisions_adopt_owner_check
        check (state <> 'adopt' or decision_source = 'owner'),
    constraint radar_decisions_rationale_check check (length(rationale) between 3 and 4000),
    constraint radar_decisions_evidence_check check (jsonb_typeof(evidence) = 'object'),
    constraint radar_decisions_review_at_check check (review_at > decided_at),
    constraint radar_decisions_required_context_check check (
        cardinality(applicable_project_types) between 1 and 20
        and cardinality(compatibility_requirements) between 1 and 50
        and cardinality(exit_conditions) between 1 and 50
    )
);

create index idx_radar_decisions_candidate_decided_at
    on app.radar_decisions (candidate_id, decided_at desc, id desc);

-- +goose Down
drop table if exists app.radar_decisions;
drop table if exists app.radar_comparisons;
drop table if exists app.package_metrics;
drop table if exists app.package_candidates;
drop table if exists app.package_candidate_evidence;
drop table if exists app.radar_discovery_runs;
