package pgstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/readingstate"
)

type dueSnooze struct {
	itemID       string
	snoozedUntil time.Time
	storyID      string
	userID       string
	version      int64
}

func (store *Store) ReturnDueSnoozes(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, fmt.Errorf("%w: snooze return limit must be 1 through 500", readingstate.ErrInvalid)
	}
	now := store.clock().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("begin due-snooze return: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		select
			state.user_id::text,
			state.item_id::text,
			cluster.id::text,
			state.version,
			state.snoozed_until
		from app.user_item_states state
		join lateral (
			select candidate.id
			from app.story_clusters candidate
			where candidate.primary_item_id = state.item_id
			order by candidate.id
			limit 1
		) cluster on true
		where state.snoozed_until <= $1
		order by state.snoozed_until, state.user_id, state.item_id
		limit $2
		for update of state skip locked`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("select due snoozes: %w", err)
	}
	candidates := make([]dueSnooze, 0, limit)
	for rows.Next() {
		candidate := dueSnooze{}
		if err := rows.Scan(
			&candidate.userID,
			&candidate.itemID,
			&candidate.storyID,
			&candidate.version,
			&candidate.snoozedUntil,
		); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due snooze: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due snoozes: %w", err)
	}
	rows.Close()

	for _, candidate := range candidates {
		idempotencyKey := fmt.Sprintf(
			"return-snooze:%s:%d",
			candidate.itemID,
			candidate.snoozedUntil.UTC().UnixNano(),
		)
		mutation, mutationErr := store.mutateTx(ctx, tx, readingstate.Command{
			Action:          readingstate.ActionReturnSnoozed,
			ExpectedVersion: candidate.version,
			IdempotencyKey:  idempotencyKey,
			StoryID:         candidate.storyID,
			UserID:          candidate.userID,
		}, nil, now)
		if mutationErr != nil {
			return 0, mutationErr
		}
		if _, err := tx.Exec(ctx, `
			insert into app.outbox_events (
				event_type, aggregate_type, aggregate_id, payload, created_at
			) values (
				'reading_state.snooze_returned',
				'item',
				$1::uuid,
				jsonb_build_object(
					'idempotencyKey', $2::text,
					'mutationId', $3::text,
					'snoozedUntil', $4::timestamptz,
					'storyId', $5::text,
					'userId', $6::text,
					'version', $7::bigint
				),
				$8
			)`,
			candidate.itemID,
			idempotencyKey,
			mutation.MutationID,
			candidate.snoozedUntil.UTC(),
			candidate.storyID,
			candidate.userID,
			mutation.State.Version,
			now,
		); err != nil {
			return 0, fmt.Errorf("record returned-snooze outbox event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit due-snooze return: %w", err)
	}
	return len(candidates), nil
}
