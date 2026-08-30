-- +goose Up
alter table app.tags
    add constraint tags_user_id_id_key unique (user_id, id);

alter table app.item_tags
    add constraint item_tags_user_tag_fkey
    foreign key (user_id, tag_id) references app.tags (user_id, id) on delete cascade;

create index idx_user_item_states_location_read_updated_at
    on app.user_item_states (user_id, location, is_read, updated_at desc, item_id);

alter table app.feedback add column idempotency_key text;

alter table app.feedback
    add constraint feedback_idempotency_key_check check (
        idempotency_key is null or length(idempotency_key) between 16 and 255
    );

create unique index idx_feedback_user_idempotency_key_partial
    on app.feedback (user_id, idempotency_key)
    where idempotency_key is not null;

-- +goose Down
drop index if exists app.idx_feedback_user_idempotency_key_partial;
alter table app.feedback drop constraint if exists feedback_idempotency_key_check;
alter table app.feedback drop column if exists idempotency_key;
drop index if exists app.idx_user_item_states_location_read_updated_at;
alter table app.item_tags drop constraint if exists item_tags_user_tag_fkey;
alter table app.tags drop constraint if exists tags_user_id_id_key;
