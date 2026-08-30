package pgstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/digest"
)

func (store *Store) Preview(
	ctx context.Context,
	userID string,
	scheduleID string,
	now time.Time,
) (digest.DigestPreview, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
		IsoLevel:   pgx.RepeatableRead,
	})
	if err != nil {
		return digest.DigestPreview{}, fmt.Errorf("begin digest preview: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var settings occurrenceSettings
	var resolvedScheduleID, timezone string
	if err := transaction.QueryRow(ctx, `
		select schedule.id::text, schedule.user_id::text, schedule.timezone,
			schedule.next_due_at, schedule.maximum_items,
			schedule.minimum_score::double precision, schedule.include_coming_soon,
			schedule.include_radar_candidates, schedule.include_later_reminders,
			schedule.empty_behavior, schedule.channels
		from app.schedule_definitions schedule
		where schedule.schedule_type = 'daily_digest'
			and ($1 = '' or schedule.user_id::text = $1)
			and ($2 = '' or schedule.id::text = $2)
		order by schedule.user_id, schedule.id
		limit 1`, userID, scheduleID).Scan(
		&resolvedScheduleID, &settings.UserID, &timezone, &settings.ScheduledFor,
		&settings.MaximumItems, &settings.MinimumScore, &settings.IncludeComingSoon,
		&settings.IncludeRadarCandidates, &settings.IncludeLaterReminders,
		&settings.EmptyBehavior, &settings.Channels,
	); errors.Is(err, pgx.ErrNoRows) {
		return digest.DigestPreview{}, digest.ErrNotFound
	} else if err != nil {
		return digest.DigestPreview{}, fmt.Errorf("select digest preview schedule: %w", err)
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return digest.DigestPreview{}, fmt.Errorf("load digest preview timezone: %w", err)
	}
	windowEnd := settings.ScheduledFor.UTC().Add(-15 * time.Minute)
	if now.UTC().Before(windowEnd) {
		windowEnd = now.UTC()
	}
	windowStart := windowEnd.Add(-24 * time.Hour)
	var priorCutoff *time.Time
	if err := transaction.QueryRow(ctx, `
		select max(window_end)
		from app.digests
		where user_id = $1::uuid and channel = 'dashboard' and state = 'delivered'
			and window_end < $2`, settings.UserID, windowEnd).Scan(&priorCutoff); err != nil {
		return digest.DigestPreview{}, fmt.Errorf("select digest preview cutoff: %w", err)
	}
	if priorCutoff != nil {
		windowStart = priorCutoff.UTC()
	}
	candidates, err := store.captureCandidates(ctx, transaction, settings, windowStart, windowEnd)
	if err != nil {
		return digest.DigestPreview{}, err
	}
	selected := digest.Select(candidates, settings.MaximumItems, settings.MinimumScore)
	payload, _, err := digest.Render(digest.RenderInput{
		Channel: digest.ChannelDashboard, LocalDate: settings.ScheduledFor.In(location).Format(time.DateOnly),
		WindowStart: windowStart, WindowEnd: windowEnd, GeneratedAt: now.UTC(), Items: selected,
	})
	if err != nil {
		return digest.DigestPreview{}, err
	}
	channels := append([]string{digest.ChannelDashboard}, settings.Channels...)
	slices.Sort(channels)
	channels = slices.Compact(channels)
	if err := transaction.Commit(ctx); err != nil {
		return digest.DigestPreview{}, fmt.Errorf("commit digest preview: %w", err)
	}
	return digest.DigestPreview{
		ScheduleID: resolvedScheduleID, LocalDate: payload.LocalDate,
		CandidateCount: len(candidates), MaximumItems: settings.MaximumItems,
		Channels: channels, Rendered: payload,
	}, nil
}
