-- +goose Up
alter table app.feedback add column mutation_id uuid;

alter table app.feedback
    add constraint feedback_mutation_id_fkey
    foreign key (mutation_id) references app.item_state_mutations (id) on delete set null;

create unique index idx_feedback_mutation_id
    on app.feedback (mutation_id)
    where mutation_id is not null;

-- +goose Down
drop index if exists app.idx_feedback_mutation_id;
alter table app.feedback drop constraint if exists feedback_mutation_id_fkey;
alter table app.feedback drop column if exists mutation_id;
