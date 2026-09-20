package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/intelligence"
)

const replayWindow = 7 * 24 * time.Hour

func (store *Store) LatestCursor(ctx context.Context, at time.Time) (int64, error) {
	var cursor int64
	err := store.pool.QueryRow(ctx, `
		select coalesce(max(id), 0) from app.outbox_events
		where event_type in ('story-created', 'story-updated')
			and created_at >= $1`, at.Add(-replayWindow)).Scan(&cursor)
	if err != nil {
		return 0, fmt.Errorf("select latest live cursor: %w", err)
	}
	return cursor, nil
}

func (store *Store) Live(ctx context.Context, generatedAt time.Time) (intelligence.LiveSnapshot, error) {
	cursor, err := store.LatestCursor(ctx, generatedAt)
	if err != nil {
		return intelligence.LiveSnapshot{}, err
	}
	rows, err := store.pool.Query(ctx, `
		select id, event_type, payload
		from app.outbox_events
		where event_type in ('story-created', 'story-updated')
			and id <= $1 and created_at >= $2
		order by id desc limit 50`, cursor, generatedAt.Add(-replayWindow))
	if err != nil {
		return intelligence.LiveSnapshot{}, fmt.Errorf("select latest live events: %w", err)
	}
	defer rows.Close()
	events := make([]intelligence.LiveEvent, 0, 50)
	for rows.Next() {
		var id int64
		var eventType string
		var payload []byte
		if err := rows.Scan(&id, &eventType, &payload); err != nil {
			return intelligence.LiveSnapshot{}, fmt.Errorf("scan live event: %w", err)
		}
		event, err := decodeLiveEvent(id, eventType, payload)
		if err != nil {
			return intelligence.LiveSnapshot{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return intelligence.LiveSnapshot{}, fmt.Errorf("iterate latest live events: %w", err)
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return intelligence.LiveSnapshot{
		Cursor: strconv.FormatInt(cursor, 10), Events: events, GeneratedAt: generatedAt.UTC(),
	}, nil
}

func (store *Store) Replay(ctx context.Context, after int64, now time.Time, limit int) (intelligence.ReplayBatch, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return intelligence.ReplayBatch{}, errors.New("live replay requires a nonnegative cursor and limit from 1 to 100")
	}
	cutoff := now.Add(-replayWindow)
	if after > 0 {
		var retained bool
		if err := store.pool.QueryRow(ctx, `
			select exists (
				select 1 from app.outbox_events
				where id = $1 and event_type in ('story-created', 'story-updated')
					and created_at >= $2
			)`, after, cutoff).Scan(&retained); err != nil {
			return intelligence.ReplayBatch{}, fmt.Errorf("validate live cursor: %w", err)
		}
		if !retained {
			return intelligence.ReplayBatch{ResetRequired: true}, nil
		}
	}
	rows, err := store.pool.Query(ctx, `
		select id, event_type, payload
		from app.outbox_events
		where id > $1 and event_type in ('story-created', 'story-updated')
			and created_at >= $2
		order by id limit $3`, after, cutoff, limit+1)
	if err != nil {
		return intelligence.ReplayBatch{}, fmt.Errorf("select replay events: %w", err)
	}
	defer rows.Close()
	batch := intelligence.ReplayBatch{Events: make([]intelligence.ReplayEvent, 0, limit)}
	for rows.Next() {
		var id int64
		var eventType string
		var payload []byte
		if err := rows.Scan(&id, &eventType, &payload); err != nil {
			return intelligence.ReplayBatch{}, fmt.Errorf("scan replay event: %w", err)
		}
		if len(batch.Events) == limit {
			batch.HasMore = true
			break
		}
		event, err := decodeLiveEvent(id, eventType, payload)
		if err != nil {
			return intelligence.ReplayBatch{}, err
		}
		batch.Events = append(batch.Events, intelligence.ReplayEvent{Cursor: id, Event: event})
	}
	if err := rows.Err(); err != nil {
		return intelligence.ReplayBatch{}, fmt.Errorf("iterate replay events: %w", err)
	}
	return batch, nil
}

func decodeLiveEvent(id int64, eventType string, payload []byte) (intelligence.LiveEvent, error) {
	var decoded struct {
		ObservedAt time.Time                 `json:"observedAt"`
		Story      intelligence.StorySummary `json:"story"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return intelligence.LiveEvent{}, fmt.Errorf("decode live outbox payload: %w", err)
	}
	if decoded.Story.ID == "" || decoded.ObservedAt.IsZero() ||
		(eventType != "story-created" && eventType != "story-updated") {
		return intelligence.LiveEvent{}, errors.New("live outbox event is incomplete")
	}
	return intelligence.LiveEvent{
		ID: strconv.FormatInt(id, 10), ObservedAt: decoded.ObservedAt.UTC(),
		Story: decoded.Story, Type: eventType,
	}, nil
}

type liveSubscription struct {
	connection *pgx.Conn
}

func (store *Store) Subscribe(ctx context.Context) (intelligence.LiveSubscription, error) {
	// A long-lived LISTEN must not reserve a pooled query connection.
	connection, err := pgx.ConnectConfig(ctx, store.pool.Config().ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("connect live notification listener: %w", err)
	}
	if _, err := connection.Exec(ctx, `listen relantern_live_events`); err != nil {
		closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = connection.Close(closeContext)
		return nil, fmt.Errorf("listen for live events: %w", err)
	}
	return &liveSubscription{connection: connection}, nil
}

func (subscription *liveSubscription) Wait(ctx context.Context) error {
	_, err := subscription.connection.WaitForNotification(ctx)
	return err
}

func (subscription *liveSubscription) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = subscription.connection.Close(ctx)
}
