package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/jobqueue"
)

type occurrenceSettings struct {
	UserID                 string
	LocalDate              time.Time
	ScheduledFor           time.Time
	State                  string
	MaximumItems           int
	MinimumScore           float64
	IncludeComingSoon      bool
	IncludeRadarCandidates bool
	IncludeLaterReminders  bool
	EmptyBehavior          string
	Channels               []string
	TriggerType            string
	ExternalDelivery       bool
	Stage                  string
}

func (store *Store) Prepare(ctx context.Context, occurrenceID string, now time.Time) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin daily digest preparation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	settings, err := selectOccurrenceSettings(ctx, transaction, occurrenceID, true)
	if err != nil {
		return err
	}
	if terminalOccurrence(settings.State) {
		return transaction.Commit(ctx)
	}
	if settings.Stage == "prepared" || settings.Stage == "finalized" {
		if _, _, err := store.jobs.EnqueueFinalizeDailyDigest(ctx, transaction, jobqueue.FinalizeDailyDigestArgs{
			OccurrenceID: occurrenceID,
		}); err != nil {
			return err
		}
		return transaction.Commit(ctx)
	}

	windowEnd := settings.ScheduledFor.UTC().Add(-15 * time.Minute)
	windowStart := windowEnd.Add(-24 * time.Hour)
	var priorCutoff *time.Time
	if err := transaction.QueryRow(ctx, `
		select max(window_end)
		from app.digests
		where user_id = $1::uuid and channel = 'dashboard' and state = 'delivered'
			and window_end < $2`, settings.UserID, windowEnd).Scan(&priorCutoff); err != nil {
		return fmt.Errorf("select prior delivered digest cutoff: %w", err)
	}
	if priorCutoff != nil {
		windowStart = priorCutoff.UTC()
	}
	if !windowEnd.After(windowStart) {
		return fmt.Errorf("%w: digest window cutoff is not after its start", digest.ErrInvalid)
	}
	candidates, err := store.captureCandidates(ctx, transaction, settings, windowStart, windowEnd)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		encoded, marshalErr := json.Marshal(candidate)
		if marshalErr != nil {
			return fmt.Errorf("encode digest candidate %s: %w", candidate.ID, marshalErr)
		}
		_, insertErr := transaction.Exec(ctx, `
			insert into app.digest_candidates (
				occurrence_id, candidate_type, candidate_id, story_id, item_id,
				radar_candidate_id, score, category, reason, snapshot, captured_at
			) values (
				$1::uuid, $2, $3::uuid,
				case when $2 = 'story' then $3::uuid end,
				case when $2 = 'story' then $4::uuid end,
				case when $2 = 'radar' then $3::uuid end,
				$5, $6, $7, $8::jsonb, $9
			)
			on conflict (occurrence_id, candidate_type, candidate_id) do nothing`,
			occurrenceID, candidate.Type, candidate.ID, nullableUUID(candidate.ItemID),
			candidate.Score, candidate.Category, candidate.Reason, encoded, now.UTC())
		if insertErr != nil {
			return fmt.Errorf("persist digest candidate %s: %w", candidate.ID, insertErr)
		}
	}
	metadata, err := json.Marshal(map[string]any{
		"digestStage":    "prepared",
		"preparedAt":     now.UTC(),
		"windowStart":    windowStart.UTC(),
		"windowEnd":      windowEnd.UTC(),
		"backlogWindow":  windowEnd.Sub(windowStart) > 36*time.Hour,
		"candidateCount": len(candidates),
	})
	if err != nil {
		return fmt.Errorf("encode digest preparation metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'preparing', metadata = metadata || $2::jsonb
		where id = $1::uuid`, occurrenceID, metadata); err != nil {
		return fmt.Errorf("record digest preparation: %w", err)
	}
	if _, _, err := store.jobs.EnqueueFinalizeDailyDigest(ctx, transaction, jobqueue.FinalizeDailyDigestArgs{
		OccurrenceID: occurrenceID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit daily digest preparation: %w", err)
	}
	return nil
}

func (store *Store) Finalize(ctx context.Context, occurrenceID string, now time.Time) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin daily digest finalization: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	settings, err := selectOccurrenceSettings(ctx, transaction, occurrenceID, true)
	if err != nil {
		return err
	}
	if terminalOccurrence(settings.State) {
		return transaction.Commit(ctx)
	}
	var existing int
	if err := transaction.QueryRow(ctx, `select count(*)::integer from app.digests where schedule_occurrence_id = $1::uuid`, occurrenceID).Scan(&existing); err != nil {
		return fmt.Errorf("count finalized digests: %w", err)
	}
	if existing > 0 {
		return store.resumeFinalized(ctx, transaction, occurrenceID)
	}
	if settings.Stage != "prepared" {
		return fmt.Errorf("%w: digest candidates are not prepared", digest.ErrConflict)
	}
	windowStart, windowEnd, backlog, err := selectFrozenWindow(ctx, transaction, occurrenceID)
	if err != nil {
		return err
	}
	candidates, err := loadEligibleCandidates(ctx, transaction, occurrenceID, settings, now.UTC())
	if err != nil {
		return err
	}
	selected := digest.Select(candidates, settings.MaximumItems, settings.MinimumScore)
	channels := append([]string{digest.ChannelDashboard}, settings.Channels...)
	if !settings.ExternalDelivery {
		channels = []string{digest.ChannelDashboard}
	}
	slices.Sort(channels)
	channels = slices.Compact(channels)
	if len(selected) == 0 && settings.EmptyBehavior != "all_clear" {
		channels = []string{digest.ChannelDashboard}
	}
	generatedAt := now.UTC()
	executiveSummary := ""
	if backlog {
		executiveSummary = fmt.Sprintf(
			"%d evidence-backed changes are ready for review from an extended window; the remaining candidates stay available in Search.",
			len(selected),
		)
	}
	externalCount := 0
	for _, channel := range channels {
		if channel != digest.ChannelDashboard && channel != digest.ChannelDiscord && channel != digest.ChannelEmail {
			return fmt.Errorf("%w: unsupported digest channel %q", digest.ErrInvalid, channel)
		}
		payload, hash, renderErr := digest.Render(digest.RenderInput{
			Channel: channel, LocalDate: settings.LocalDate.Format(time.DateOnly),
			WindowStart: windowStart, WindowEnd: windowEnd, GeneratedAt: generatedAt,
			ExecutiveSummary: executiveSummary, Items: selected,
		})
		if renderErr != nil {
			return renderErr
		}
		state := "ready"
		var completedAt *time.Time
		if channel == digest.ChannelDashboard {
			state = "delivered"
			completedAt = &generatedAt
		} else {
			externalCount++
		}
		encodedPayload, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return fmt.Errorf("encode immutable digest payload: %w", marshalErr)
		}
		var digestID string
		idempotencyKey := "digest:" + occurrenceID + ":" + channel
		if err := transaction.QueryRow(ctx, `
			insert into app.digests (
				user_id, schedule_occurrence_id, local_digest_date, window_start, window_end,
				channel, state, item_limit, minimum_score, empty_behavior, executive_summary,
				rendered_payload, payload_sha256, provider_idempotency_key, generated_at, completed_at
			) values ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11,
				$12::jsonb, $13, $14, $15, $16)
			returning id::text`, settings.UserID, occurrenceID, settings.LocalDate, windowStart, windowEnd,
			channel, state, settings.MaximumItems, settings.MinimumScore, settings.EmptyBehavior,
			payload.ExecutiveSummary, encodedPayload, hash, idempotencyKey, generatedAt, completedAt).Scan(&digestID); err != nil {
			return fmt.Errorf("persist immutable %s digest: %w", channel, err)
		}
		for position, candidate := range selected {
			encoded, marshalErr := json.Marshal(candidate)
			if marshalErr != nil {
				return fmt.Errorf("encode selected digest item: %w", marshalErr)
			}
			if _, err := transaction.Exec(ctx, `
				insert into app.digest_items (
					digest_id, candidate_type, candidate_id, sort_order, score, category, reason, snapshot
				) values ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8::jsonb)`,
				digestID, candidate.Type, candidate.ID, position, candidate.Score,
				candidate.Category, candidate.Reason, encoded); err != nil {
				return fmt.Errorf("persist selected digest item: %w", err)
			}
		}
		if channel != digest.ChannelDashboard {
			if _, err := transaction.Exec(ctx, `
				insert into app.delivery_attempts (
					digest_id, channel, idempotency_key, state, payload_sha256
				) values ($1::uuid, $2, $3, 'pending', $4)`, digestID, channel, idempotencyKey, hash); err != nil {
				return fmt.Errorf("create digest delivery ledger: %w", err)
			}
			if _, _, err := store.jobs.EnqueueDeliverDigest(ctx, transaction, jobqueue.DeliverDigestArgs{DigestID: digestID}); err != nil {
				return err
			}
		}
	}
	state := "delivered"
	var completedAt *time.Time
	if externalCount > 0 {
		state = "ready"
	} else {
		completedAt = &generatedAt
	}
	metadata, err := json.Marshal(map[string]any{
		"digestStage": "finalized", "finalizedAt": generatedAt,
		"selectedCount": len(selected), "externalDeliveryCount": externalCount,
	})
	if err != nil {
		return fmt.Errorf("encode digest finalization metadata: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.schedule_occurrences
		set state = $2, completed_at = $3, error_code = null, metadata = metadata || $4::jsonb
		where id = $1::uuid`, occurrenceID, state, completedAt, metadata); err != nil {
		return fmt.Errorf("record digest finalization: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit daily digest finalization: %w", err)
	}
	return nil
}

func (store *Store) resumeFinalized(ctx context.Context, transaction pgx.Tx, occurrenceID string) error {
	rows, err := transaction.Query(ctx, `
		select id::text from app.digests
		where schedule_occurrence_id = $1::uuid and channel <> 'dashboard' and state = 'ready'
		order by channel`, occurrenceID)
	if err != nil {
		return fmt.Errorf("select ready digest deliveries: %w", err)
	}
	defer rows.Close()
	digestIDs := make([]string, 0, 2)
	for rows.Next() {
		var digestID string
		if err := rows.Scan(&digestID); err != nil {
			return fmt.Errorf("scan ready digest delivery: %w", err)
		}
		digestIDs = append(digestIDs, digestID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate ready digest deliveries: %w", err)
	}
	for _, digestID := range digestIDs {
		if _, _, err := store.jobs.EnqueueDeliverDigest(ctx, transaction, jobqueue.DeliverDigestArgs{DigestID: digestID}); err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}

func selectOccurrenceSettings(
	ctx context.Context,
	transaction pgx.Tx,
	occurrenceID string,
	lock bool,
) (occurrenceSettings, error) {
	query := `
		select schedule.user_id::text, occurrence.local_date, occurrence.scheduled_for,
			occurrence.state, schedule.maximum_items, schedule.minimum_score::double precision,
			schedule.include_coming_soon, schedule.include_radar_candidates,
			schedule.include_later_reminders, schedule.empty_behavior, schedule.channels,
			occurrence.trigger_type,
			coalesce((occurrence.metadata->>'externalDelivery')::boolean, occurrence.trigger_type <> 'run_now'),
			coalesce(occurrence.metadata->>'digestStage', '')
		from app.schedule_occurrences occurrence
		join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
		where occurrence.id = $1::uuid and schedule.schedule_type = 'daily_digest'`
	if lock {
		query += " for update"
	}
	var settings occurrenceSettings
	if err := transaction.QueryRow(ctx, query, occurrenceID).Scan(
		&settings.UserID, &settings.LocalDate, &settings.ScheduledFor, &settings.State,
		&settings.MaximumItems, &settings.MinimumScore, &settings.IncludeComingSoon,
		&settings.IncludeRadarCandidates, &settings.IncludeLaterReminders,
		&settings.EmptyBehavior, &settings.Channels, &settings.TriggerType,
		&settings.ExternalDelivery, &settings.Stage,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return occurrenceSettings{}, digest.ErrNotFound
		}
		return occurrenceSettings{}, fmt.Errorf("select daily digest occurrence settings: %w", err)
	}
	return settings, nil
}

func selectFrozenWindow(ctx context.Context, transaction pgx.Tx, occurrenceID string) (time.Time, time.Time, bool, error) {
	var windowStart, windowEnd time.Time
	var backlog bool
	if err := transaction.QueryRow(ctx, `
		select (metadata->>'windowStart')::timestamptz,
			(metadata->>'windowEnd')::timestamptz,
			coalesce((metadata->>'backlogWindow')::boolean, false)
		from app.schedule_occurrences where id = $1::uuid`, occurrenceID).Scan(&windowStart, &windowEnd, &backlog); err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("select frozen digest window: %w", err)
	}
	return windowStart.UTC(), windowEnd.UTC(), backlog, nil
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
