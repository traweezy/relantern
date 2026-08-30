package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/controlplane"
)

func (store *Store) Settings(
	ctx context.Context,
	userID string,
	now time.Time,
) (controlplane.SettingsSnapshot, error) {
	settings := controlplane.SettingsSnapshot{GeneratedAt: now.UTC()}
	if err := store.pool.QueryRow(ctx, `
		select
			owner.timezone,
			to_char(settings.quiet_hours_start, 'HH24:MI'),
			to_char(settings.quiet_hours_end, 'HH24:MI'),
			settings.critical_alerts_bypass,
			settings.monthly_soft_budget_usd::text,
			settings.monthly_hard_budget_usd::text,
			settings.raw_retention_days,
			settings.audit_retention_days,
			settings.version
		from app.users owner
		join app.owner_settings settings on settings.user_id = owner.id
		where owner.id = $1::uuid`, userID).Scan(
		&settings.Owner.Timezone,
		&settings.Owner.QuietHoursStart,
		&settings.Owner.QuietHoursEnd,
		&settings.Owner.CriticalAlertsBypass,
		&settings.Owner.MonthlySoftBudgetUSD,
		&settings.Owner.MonthlyHardBudgetUSD,
		&settings.Owner.RawRetentionDays,
		&settings.Owner.AuditRetentionDays,
		&settings.Owner.Version,
	); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.SettingsSnapshot{}, controlplane.ErrNotFound
	} else if err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("load owner settings: %w", err)
	}
	if err := store.loadInterestProfile(ctx, userID, &settings.Profile); err != nil {
		return controlplane.SettingsSnapshot{}, err
	}
	technologies, err := store.loadWatchedTechnologies(ctx, userID)
	if err != nil {
		return controlplane.SettingsSnapshot{}, err
	}
	settings.Technologies = technologies
	schedules, err := store.listSchedules(ctx, userID)
	if err != nil {
		return controlplane.SettingsSnapshot{}, err
	}
	settings.Schedules = schedules
	return settings, nil
}

func (store *Store) loadInterestProfile(
	ctx context.Context,
	userID string,
	profile *controlplane.InterestProfile,
) error {
	if err := store.pool.QueryRow(ctx, `
		select id::text, name, profile_summary, version
		from app.interest_profiles
		where user_id = $1::uuid and is_active`, userID).Scan(
		&profile.ID,
		&profile.Name,
		&profile.ProfileSummary,
		&profile.Version,
	); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("load active interest profile: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
		select topic_id, priority, weight::double precision, keywords, exclusions
		from app.interest_topics
		where profile_id = $1::uuid
		order by priority, topic_id`, profile.ID)
	if err != nil {
		return fmt.Errorf("load interest topics: %w", err)
	}
	defer rows.Close()
	profile.Topics = make([]controlplane.InterestTopic, 0)
	for rows.Next() {
		var topic controlplane.InterestTopic
		if err := rows.Scan(
			&topic.TopicID,
			&topic.Priority,
			&topic.Weight,
			&topic.Keywords,
			&topic.Exclusions,
		); err != nil {
			return fmt.Errorf("scan interest topic: %w", err)
		}
		profile.Topics = append(profile.Topics, topic)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate interest topics: %w", err)
	}
	return nil
}

func (store *Store) loadWatchedTechnologies(
	ctx context.Context,
	userID string,
) ([]controlplane.WatchedTechnology, error) {
	rows, err := store.pool.Query(ctx, `
		select
			id::text,
			technology,
			package_name,
			current_version,
			version_constraint,
			status,
			source,
			last_verified_at
		from app.watched_technologies
		where user_id = $1::uuid
		order by technology, package_name`, userID)
	if err != nil {
		return nil, fmt.Errorf("load watched technologies: %w", err)
	}
	defer rows.Close()
	technologies := make([]controlplane.WatchedTechnology, 0)
	for rows.Next() {
		var technology controlplane.WatchedTechnology
		if err := rows.Scan(
			&technology.ID,
			&technology.Technology,
			&technology.PackageName,
			&technology.CurrentVersion,
			&technology.VersionConstraint,
			&technology.Status,
			&technology.Source,
			&technology.LastVerifiedAt,
		); err != nil {
			return nil, fmt.Errorf("scan watched technology: %w", err)
		}
		technologies = append(technologies, technology)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watched technologies: %w", err)
	}
	return technologies, nil
}

func (store *Store) UpdateSettings(
	ctx context.Context,
	request controlplane.UpdateSettingsRequest,
	now time.Time,
) (controlplane.SettingsSnapshot, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("begin owner settings update: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var currentVersion int64
	if err := transaction.QueryRow(ctx, `
		select version
		from app.owner_settings
		where user_id = $1::uuid
		for update`, request.UserID).Scan(&currentVersion); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.SettingsSnapshot{}, controlplane.ErrNotFound
	} else if err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("lock owner settings: %w", err)
	}
	if currentVersion != request.ExpectedVersion {
		return controlplane.SettingsSnapshot{}, controlplane.ErrConflict
	}
	if _, err := transaction.Exec(ctx, `
		update app.users
		set timezone = $2, updated_at = $3
		where id = $1::uuid`, request.UserID, request.Timezone, now); err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("update owner timezone: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		update app.owner_settings
		set quiet_hours_start = $2::time,
			quiet_hours_end = $3::time,
			critical_alerts_bypass = $4,
			monthly_soft_budget_usd = $5::numeric,
			monthly_hard_budget_usd = $6::numeric,
			raw_retention_days = $7,
			audit_retention_days = $8,
			version = version + 1,
			updated_at = $9
		where user_id = $1::uuid`,
		request.UserID,
		request.QuietHoursStart,
		request.QuietHoursEnd,
		request.CriticalAlertsBypass,
		request.MonthlySoftBudgetUSD,
		request.MonthlyHardBudgetUSD,
		request.RawRetentionDays,
		request.AuditRetentionDays,
		now,
	); err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("update owner settings: %w", err)
	}
	var profileID string
	if err := transaction.QueryRow(ctx, `
		update app.interest_profiles
		set name = $2,
			profile_summary = $3,
			version = version + 1,
			updated_at = $4
		where user_id = $1::uuid and is_active
		returning id::text`, request.UserID, request.ProfileName, request.ProfileSummary, now).Scan(&profileID); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.SettingsSnapshot{}, controlplane.ErrNotFound
	} else if err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("update interest profile: %w", err)
	}
	if _, err := transaction.Exec(ctx, `delete from app.interest_topics where profile_id = $1::uuid`, profileID); err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("replace interest topics: %w", err)
	}
	for _, topic := range request.Topics {
		if _, err := transaction.Exec(ctx, `
			insert into app.interest_topics (
				profile_id, topic_id, priority, weight, keywords, exclusions, created_at, updated_at
			) values ($1::uuid, $2, $3, $4, $5, $6, $7, $7)`,
			profileID,
			topic.TopicID,
			topic.Priority,
			topic.Weight,
			topic.Keywords,
			topic.Exclusions,
			now,
		); err != nil {
			return controlplane.SettingsSnapshot{}, fmt.Errorf("insert interest topic %q: %w", topic.TopicID, err)
		}
	}
	if _, err := transaction.Exec(ctx, `delete from app.watched_technologies where user_id = $1::uuid`, request.UserID); err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("replace watched technologies: %w", err)
	}
	for _, technology := range request.Technologies {
		if _, err := transaction.Exec(ctx, `
			insert into app.watched_technologies (
				user_id, technology, package_name, current_version,
				version_constraint, status, source, last_verified_at,
				created_at, updated_at
			) values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
			request.UserID,
			technology.Technology,
			technology.PackageName,
			technology.CurrentVersion,
			technology.VersionConstraint,
			technology.Status,
			technology.Source,
			technology.LastVerifiedAt,
			now,
		); err != nil {
			return controlplane.SettingsSnapshot{}, fmt.Errorf("insert watched technology %q: %w", technology.PackageName, err)
		}
	}
	if err := recordMutation(ctx, transaction, request.UserID, "owner_settings_updated", "owner_settings", request.UserID, map[string]any{
		"interestTopicCount": len(request.Topics),
		"technologyCount":    len(request.Technologies),
		"version":            currentVersion + 1,
	}); err != nil {
		return controlplane.SettingsSnapshot{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return controlplane.SettingsSnapshot{}, fmt.Errorf("commit owner settings update: %w", err)
	}
	return store.Settings(ctx, request.UserID, now)
}

func (store *Store) Schedule(
	ctx context.Context,
	userID string,
	scheduleID string,
) (controlplane.ScheduleDefinition, error) {
	row := store.pool.QueryRow(ctx, scheduleSelect+` where schedule.user_id = $1::uuid and schedule.id = $2::uuid`, userID, scheduleID)
	schedule, err := scanSchedule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return controlplane.ScheduleDefinition{}, controlplane.ErrNotFound
	}
	if err != nil {
		return controlplane.ScheduleDefinition{}, fmt.Errorf("load schedule: %w", err)
	}
	return schedule, nil
}

func (store *Store) listSchedules(
	ctx context.Context,
	userID string,
) ([]controlplane.ScheduleDefinition, error) {
	rows, err := store.pool.Query(ctx, scheduleSelect+`
		where schedule.user_id = $1::uuid
		order by schedule.schedule_type, schedule.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	schedules := make([]controlplane.ScheduleDefinition, 0)
	for rows.Next() {
		schedule, scanErr := scanSchedule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan schedule: %w", scanErr)
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}
	return schedules, nil
}

const scheduleSelect = `
	select
		schedule.id::text,
		schedule.schedule_type,
		schedule.timezone,
		to_char(schedule.local_time, 'HH24:MI'),
		schedule.days_of_week,
		schedule.enabled,
		schedule.catchup_policy,
		(extract(epoch from schedule.catchup_grace) / 60)::integer,
		schedule.next_due_at,
		schedule.skip_next_at,
		schedule.paused_at,
		schedule.paused_until,
		schedule.weekend_mode,
		schedule.maximum_items,
		schedule.minimum_score::double precision,
		schedule.include_coming_soon,
		schedule.include_radar_candidates,
		schedule.include_later_reminders,
		schedule.empty_behavior,
		schedule.channels,
		schedule.version
	from app.schedule_definitions schedule`

type rowScanner interface {
	Scan(...any) error
}

func scanSchedule(row rowScanner) (controlplane.ScheduleDefinition, error) {
	var schedule controlplane.ScheduleDefinition
	err := row.Scan(
		&schedule.ID,
		&schedule.ScheduleType,
		&schedule.Timezone,
		&schedule.LocalTime,
		&schedule.DaysOfWeek,
		&schedule.Enabled,
		&schedule.CatchupPolicy,
		&schedule.CatchupGraceMinutes,
		&schedule.NextDueAt,
		&schedule.SkipNextAt,
		&schedule.PausedAt,
		&schedule.PausedUntil,
		&schedule.WeekendMode,
		&schedule.MaximumItems,
		&schedule.MinimumScore,
		&schedule.IncludeComingSoon,
		&schedule.IncludeRadarCandidates,
		&schedule.IncludeLaterReminders,
		&schedule.EmptyBehavior,
		&schedule.Channels,
		&schedule.Version,
	)
	return schedule, err
}

func (store *Store) UpdateSchedule(
	ctx context.Context,
	request controlplane.UpdateScheduleRequest,
	now time.Time,
) (controlplane.ScheduleDefinition, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return controlplane.ScheduleDefinition{}, fmt.Errorf("begin schedule update: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	command, err := transaction.Exec(ctx, `
		update app.schedule_definitions
		set timezone = $4,
			local_time = $5::time,
			days_of_week = $6,
			enabled = $7,
			catchup_policy = $8,
			catchup_grace = make_interval(mins => $9),
			weekend_mode = $10,
			maximum_items = $11,
			minimum_score = $12,
			include_coming_soon = $13,
			include_radar_candidates = $14,
			include_later_reminders = $15,
			empty_behavior = $16,
			channels = $17,
			next_due_at = $18,
			skip_next_at = null,
			version = version + 1,
			updated_at = $19
		where id = $1::uuid and user_id = $2::uuid and version = $3`,
		request.ScheduleID,
		request.UserID,
		request.ExpectedVersion,
		request.Timezone,
		request.LocalTime,
		request.DaysOfWeek,
		request.Enabled,
		request.CatchupPolicy,
		request.CatchupGraceMinutes,
		request.WeekendMode,
		request.MaximumItems,
		request.MinimumScore,
		request.IncludeComingSoon,
		request.IncludeRadarCandidates,
		request.IncludeLaterReminders,
		request.EmptyBehavior,
		request.Channels,
		request.NextDueAt,
		now,
	)
	if err != nil {
		return controlplane.ScheduleDefinition{}, fmt.Errorf("update schedule: %w", err)
	}
	if command.RowsAffected() != 1 {
		var exists bool
		if err := transaction.QueryRow(ctx, `
			select exists (
				select 1 from app.schedule_definitions
				where id = $1::uuid and user_id = $2::uuid
			)`, request.ScheduleID, request.UserID).Scan(&exists); err != nil {
			return controlplane.ScheduleDefinition{}, fmt.Errorf("check schedule update conflict: %w", err)
		}
		if !exists {
			return controlplane.ScheduleDefinition{}, controlplane.ErrNotFound
		}
		return controlplane.ScheduleDefinition{}, controlplane.ErrConflict
	}
	if err := recordMutation(ctx, transaction, request.UserID, "schedule_updated", "schedule", request.ScheduleID, map[string]any{
		"expectedVersion": request.ExpectedVersion,
		"nextDueAt":       request.NextDueAt,
	}); err != nil {
		return controlplane.ScheduleDefinition{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return controlplane.ScheduleDefinition{}, fmt.Errorf("commit schedule update: %w", err)
	}
	return store.Schedule(ctx, request.UserID, request.ScheduleID)
}

func (store *Store) ActOnSchedule(
	ctx context.Context,
	request controlplane.ScheduleActionRequest,
	now time.Time,
) (controlplane.ScheduleActionResult, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return controlplane.ScheduleActionResult{}, fmt.Errorf("begin schedule action: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var timezone string
	var nextDueAt time.Time
	if err := transaction.QueryRow(ctx, `
		select timezone, next_due_at
		from app.schedule_definitions
		where id = $1::uuid and user_id = $2::uuid
		for update`, request.ScheduleID, request.UserID).Scan(&timezone, &nextDueAt); errors.Is(err, pgx.ErrNoRows) {
		return controlplane.ScheduleActionResult{}, controlplane.ErrNotFound
	} else if err != nil {
		return controlplane.ScheduleActionResult{}, fmt.Errorf("lock schedule action target: %w", err)
	}
	result := controlplane.ScheduleActionResult{}
	switch request.Action {
	case "skip_next":
		if _, err := transaction.Exec(ctx, `
			update app.schedule_definitions
			set skip_next_at = next_due_at, version = version + 1, updated_at = $3
			where id = $1::uuid and user_id = $2::uuid`, request.ScheduleID, request.UserID, now); err != nil {
			return result, fmt.Errorf("skip next schedule occurrence: %w", err)
		}
		result.Message = "The next scheduled occurrence will be recorded as skipped."
	case "pause":
		if _, err := transaction.Exec(ctx, `
			update app.schedule_definitions
			set paused_at = $3, paused_until = $4, next_due_at = $5,
				version = version + 1, updated_at = $3
			where id = $1::uuid and user_id = $2::uuid`,
			request.ScheduleID, request.UserID, now, request.PausedUntil, request.NextDueAt); err != nil {
			return result, fmt.Errorf("pause schedule: %w", err)
		}
		result.Message = "The schedule is paused through the selected instant."
	case "resume":
		if _, err := transaction.Exec(ctx, `
			update app.schedule_definitions
			set paused_at = null, paused_until = null, next_due_at = $3,
				version = version + 1, updated_at = $4
			where id = $1::uuid and user_id = $2::uuid`,
			request.ScheduleID, request.UserID, request.NextDueAt, now); err != nil {
			return result, fmt.Errorf("resume schedule: %w", err)
		}
		result.Message = "The schedule resumed with a newly calculated future occurrence."
	case "run_now":
		location, loadErr := time.LoadLocation(timezone)
		if loadErr != nil {
			return result, fmt.Errorf("load run-now timezone: %w", loadErr)
		}
		localized := now.In(location)
		_, offsetSeconds := localized.Zone()
		ledgerKey := "run-now:" + request.ScheduleID + ":" + request.IdempotencyKey
		if err := transaction.QueryRow(ctx, `
			with inserted as (
				insert into app.schedule_occurrences (
					schedule_id, scheduled_for, local_date, local_offset_seconds,
					state, trigger_type, idempotency_key, metadata
				) values (
					$1::uuid, $2, $3, $4, 'ready', 'run_now', $5,
					'{"previewOnly":true,"externalDelivery":false}'::jsonb
				)
				on conflict (idempotency_key) do nothing
				returning id::text
			)
			select id from inserted
			union all
			select id::text
			from app.schedule_occurrences
			where schedule_id = $1::uuid and idempotency_key = $5
				and not exists (select 1 from inserted)
			limit 1`,
			request.ScheduleID,
			now,
			localized.Format(time.DateOnly),
			offsetSeconds,
			ledgerKey,
		).Scan(&result.OccurrenceID); errors.Is(err, pgx.ErrNoRows) {
			return result, controlplane.ErrConflict
		} else if err != nil {
			return result, fmt.Errorf("create run-now preview occurrence: %w", err)
		}
		result.Message = "A preview-only run-now occurrence is ready; external delivery remains disabled until PR17."
	}
	if err := recordMutation(ctx, transaction, request.UserID, "schedule_"+request.Action, "schedule", request.ScheduleID, map[string]any{
		"occurrenceId": result.OccurrenceID,
		"reason":       request.Reason,
	}); err != nil {
		return controlplane.ScheduleActionResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return controlplane.ScheduleActionResult{}, fmt.Errorf("commit schedule action: %w", err)
	}
	result.Schedule, err = store.Schedule(ctx, request.UserID, request.ScheduleID)
	if err != nil {
		return controlplane.ScheduleActionResult{}, err
	}
	return result, nil
}

func (store *Store) PreviewSchedule(
	ctx context.Context,
	userID string,
	scheduleID string,
	now time.Time,
) (controlplane.SchedulePreview, error) {
	schedule, err := store.Schedule(ctx, userID, scheduleID)
	if err != nil {
		return controlplane.SchedulePreview{}, err
	}
	var candidateCount int
	if err := store.pool.QueryRow(ctx, `
		select least(count(distinct story_id), $1)::integer
		from app.v_story_summaries
		where brief_created_at >= $2::timestamptz - interval '36 hours'`, schedule.MaximumItems, now).Scan(&candidateCount); err != nil {
		return controlplane.SchedulePreview{}, fmt.Errorf("count schedule preview candidates: %w", err)
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return controlplane.SchedulePreview{}, fmt.Errorf("load schedule preview timezone: %w", err)
	}
	return controlplane.SchedulePreview{
		ScheduleID:       schedule.ID,
		ScheduleType:     schedule.ScheduleType,
		LocalDate:        schedule.NextDueAt.In(location).Format(time.DateOnly),
		NextRunAt:        schedule.NextDueAt,
		CandidateCount:   candidateCount,
		MaximumItems:     schedule.MaximumItems,
		ExternalDelivery: false,
		Explanation:      "Preview counts currently published, evidence-backed briefs in the bounded 36-hour window. No provider call or delivery occurs.",
	}, nil
}
