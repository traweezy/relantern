-- +goose NO TRANSACTION
-- +goose Up
-- A stopped concurrent build can leave an invalid index. Rebuild both indexes
-- without blocking outbox writers when Goose retries this migration.
drop index concurrently if exists app.idx_outbox_events_live_id;
create index concurrently idx_outbox_events_live_id
    on app.outbox_events (id)
    where event_type in ('story-created', 'story-updated');

drop index concurrently if exists app.idx_outbox_events_live_story_aggregate;
create index concurrently idx_outbox_events_live_story_aggregate
    on app.outbox_events (aggregate_id, created_at desc)
    where aggregate_type = 'story'
        and event_type in ('story-created', 'story-updated');

-- Keep the event payload immutable so replay preserves what the owner saw at publication.
-- +goose StatementBegin
create or replace function app.live_story_payload(requested_story_id uuid) returns jsonb
language sql stable as $$
    select jsonb_build_object(
        'confidence', story.confidence,
        'firstSeenAt', story.first_seen_at,
        'headline', story.headline,
        'id', story.story_id::text,
        'lastChangedAt', story.last_changed_at,
        'primarySourceUrl', story.primary_source_url,
        'readTimeMinutes', story.read_time_minutes,
        'recommendedAction', story.recommended_action,
        'signal', story.signal,
        'sourceCount', story.source_count,
        'sourceTier', story.source_tier,
        'status', story.status,
        'summary', story.summary,
        'whyItMatters', story.why_it_matters
    )
    from app.v_story_summaries story
    where story.story_id = requested_story_id
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function app.notify_live_outbox() returns trigger
language plpgsql as $$
begin
    perform pg_notify('relantern_live_events', new.id::text);
    return new;
end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
begin
    perform set_config('lock_timeout', '5s', true);
    if not exists (
        select 1 from pg_trigger
        where tgrelid = 'app.outbox_events'::regclass
            and tgname = 'trg_outbox_events_live_notify'
            and not tgisinternal
    ) then
        create trigger trg_outbox_events_live_notify
        after insert on app.outbox_events
        for each row
        when (new.event_type in ('story-created', 'story-updated'))
        execute function app.notify_live_outbox();
    end if;
end $$;
-- +goose StatementEnd

-- Existing published briefs need one replay identity. Commit each 100-story
-- batch so the publication lock and row writes have bounded hold time. The
-- anti-join makes a stopped migration safe to retry without duplicate events.
-- +goose StatementBegin
do $$
declare
    changed integer;
begin
    loop
        perform set_config('lock_timeout', '5s', true);
        perform set_config('statement_timeout', '30s', true);
        perform pg_advisory_xact_lock(hashtextextended('relantern:live-publication', 0));
        with batch as (
            select story.story_id, story.status, story.last_changed_at
            from app.v_story_summaries story
            where story.last_changed_at >= now() - interval '7 days'
                and not exists (
                    select 1 from app.outbox_events event
                    where event.aggregate_type = 'story'
                        and event.aggregate_id = story.story_id
                        and event.event_type in ('story-created', 'story-updated')
                        and event.created_at >= now() - interval '7 days'
                )
            order by story.last_changed_at, story.story_id
            limit 100
        )
        insert into app.outbox_events (event_type, aggregate_type, aggregate_id, payload, created_at)
        select
            case when batch.status = 'updated' then 'story-updated' else 'story-created' end,
            'story',
            batch.story_id,
            jsonb_build_object('story', app.live_story_payload(batch.story_id),
                'observedAt', batch.last_changed_at),
            batch.last_changed_at
        from batch;
        get diagnostics changed = row_count;
        exit when changed = 0;
        commit;
    end loop;
end $$;
-- +goose StatementEnd
