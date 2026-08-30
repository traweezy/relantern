package pgstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/jobqueue"
)

const digestSelect = `
	select
		current_digest.id::text, current_digest.schedule_occurrence_id::text,
		current_digest.local_digest_date::text, current_digest.window_start,
		current_digest.window_end, current_digest.channel, current_digest.state,
		current_digest.item_limit, current_digest.minimum_score::double precision,
		current_digest.empty_behavior, current_digest.executive_summary,
		current_digest.rendered_payload, current_digest.provider_idempotency_key,
		current_digest.generated_at, current_digest.completed_at,
		coalesce(current_digest.error_code, ''), coalesce(delivery.attempt_count, 0),
		coalesce(
			jsonb_agg(item.snapshot order by item.sort_order)
				filter (where item.digest_id is not null),
			'[]'::jsonb
		)
	from app.digests current_digest
	left join app.delivery_attempts delivery on delivery.digest_id = current_digest.id
	left join app.digest_items item on item.digest_id = current_digest.id`

const digestGroup = `
	group by current_digest.id, delivery.attempt_count`

func (store *Store) Snapshot(ctx context.Context, userID string, now time.Time) (digest.DigestSnapshot, error) {
	rows, err := store.pool.Query(ctx, digestSelect+`
		where current_digest.user_id = $1::uuid`+digestGroup+`
		order by current_digest.generated_at desc, current_digest.id desc
		limit 50`, userID)
	if err != nil {
		return digest.DigestSnapshot{}, fmt.Errorf("list owner digests: %w", err)
	}
	defer rows.Close()
	snapshot := digest.DigestSnapshot{GeneratedAt: now.UTC(), Digests: make([]digest.DigestRecord, 0)}
	for rows.Next() {
		current, scanErr := scanDigest(rows)
		if scanErr != nil {
			return digest.DigestSnapshot{}, scanErr
		}
		snapshot.Digests = append(snapshot.Digests, current)
	}
	if err := rows.Err(); err != nil {
		return digest.DigestSnapshot{}, fmt.Errorf("iterate owner digests: %w", err)
	}
	return snapshot, nil
}

func (store *Store) Get(ctx context.Context, userID string, digestID string) (digest.DigestRecord, error) {
	row := store.pool.QueryRow(ctx, digestSelect+`
		where current_digest.user_id = $1::uuid and current_digest.id = $2::uuid`+digestGroup,
		userID, digestID)
	current, err := scanDigest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return digest.DigestRecord{}, digest.ErrNotFound
	}
	return current, err
}

type digestScanner interface {
	Scan(...any) error
}

func scanDigest(row digestScanner) (digest.DigestRecord, error) {
	var current digest.DigestRecord
	var rendered, items []byte
	if err := row.Scan(
		&current.ID, &current.OccurrenceID, &current.LocalDate,
		&current.WindowStart, &current.WindowEnd, &current.Channel, &current.State,
		&current.ItemLimit, &current.MinimumScore, &current.EmptyBehavior,
		&current.ExecutiveSummary, &rendered, &current.ProviderIdempotencyKey,
		&current.GeneratedAt, &current.CompletedAt, &current.ErrorCode,
		&current.AttemptCount, &items,
	); err != nil {
		return digest.DigestRecord{}, err
	}
	if err := json.Unmarshal(rendered, &current.Rendered); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("decode immutable digest payload: %w", err)
	}
	if err := json.Unmarshal(items, &current.Items); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("decode immutable digest items: %w", err)
	}
	current.WindowStart = current.WindowStart.UTC()
	current.WindowEnd = current.WindowEnd.UTC()
	current.GeneratedAt = current.GeneratedAt.UTC()
	if current.CompletedAt != nil {
		completedAt := current.CompletedAt.UTC()
		current.CompletedAt = &completedAt
	}
	return current, nil
}

func (store *Store) Retry(ctx context.Context, userID string, digestID string, now time.Time) (digest.DigestRecord, error) {
	return retrySerializableValue(ctx, func() (digest.DigestRecord, error) {
		return store.retry(ctx, userID, digestID, now)
	})
}

func (store *Store) retry(ctx context.Context, userID string, digestID string, now time.Time) (digest.DigestRecord, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return digest.DigestRecord{}, fmt.Errorf("begin digest retry: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var channel, state string
	var payloadHash []byte
	if err := transaction.QueryRow(ctx, `
		select channel, state, payload_sha256
		from app.digests
		where id = $1::uuid and user_id = $2::uuid
		for update`, digestID, userID).Scan(&channel, &state, &payloadHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digest.DigestRecord{}, digest.ErrNotFound
		}
		return digest.DigestRecord{}, fmt.Errorf("select digest retry: %w", err)
	}
	if channel == digest.ChannelDashboard || state != "failed" {
		return digest.DigestRecord{}, fmt.Errorf("%w: only failed external deliveries can be retried", digest.ErrConflict)
	}
	if _, err := transaction.Exec(ctx, `
		update app.digests set state = 'ready', completed_at = null, error_code = null
		where id = $1::uuid`, digestID); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("reset failed digest delivery: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.delivery_attempts
		set provider_id = null, state = 'pending', completed_at = null,
			error_code = null, updated_at = $2
		where digest_id = $1::uuid`, digestID, now.UTC()); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("reset digest delivery ledger: %w", err)
	}
	if _, _, err := store.jobs.EnqueueDeliverDigest(ctx, transaction, jobqueue.DeliverDigestArgs{DigestID: digestID}); err != nil {
		return digest.DigestRecord{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"channel": channel, "payloadSha256": hex.EncodeToString(payloadHash), "requestedAt": now.UTC(),
	})
	if err != nil {
		return digest.DigestRecord{}, fmt.Errorf("encode digest retry audit metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.audit_events (
			actor_type, actor_id, action, target_type, target_id, metadata
		) values ('owner', $1, 'digest_delivery_retried', 'digest', $2, $3::jsonb)`,
		userID, digestID, metadata); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("record digest retry audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		insert into app.outbox_events (event_type, aggregate_type, aggregate_id, payload)
		values ('digest_delivery_retried', 'digest', $1::uuid, $2::jsonb)`, userID, metadata); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("record digest retry outbox event: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return digest.DigestRecord{}, fmt.Errorf("commit digest retry: %w", err)
	}
	return store.Get(ctx, userID, digestID)
}
