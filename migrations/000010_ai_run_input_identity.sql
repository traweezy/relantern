-- +goose Up
alter table app.ai_runs
    drop constraint ai_runs_identity_key;

alter table app.ai_runs
    add constraint ai_runs_identity_key unique (
        item_id,
        revision_id,
        purpose,
        model_config_id,
        prompt_version_id,
        input_sha256
    );

-- +goose Down
alter table app.ai_runs
    drop constraint ai_runs_identity_key;

alter table app.ai_runs
    add constraint ai_runs_identity_key unique (
        item_id,
        revision_id,
        purpose,
        model_config_id,
        prompt_version_id
    );
