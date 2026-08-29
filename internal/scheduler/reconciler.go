package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
)

const (
	reconciliationBatchSize          = 100
	maximumOccurrencesPerScheduleRun = 100
)

type Reconciler struct {
	pool          *pgxpool.Pool
	clock         clock.Clock
	lockNamespace string
	enqueuer      OccurrenceEnqueuer
	scheduleIDs   []string
}

type ReconcilerOption func(*Reconciler)

func WithScheduleIDs(scheduleIDs ...string) ReconcilerOption {
	ids := append([]string(nil), scheduleIDs...)
	return func(reconciler *Reconciler) {
		reconciler.scheduleIDs = ids
	}
}

type OccurrenceEnqueuer interface {
	EnqueueScheduleOccurrence(context.Context, pgx.Tx, string, string) (int64, bool, error)
}

type ReconcileResult struct {
	LockAcquired       bool
	OccurrencesCreated int64
	JobsEnqueued       int64
}

type dueSchedule struct {
	id            string
	timezone      string
	localTime     LocalTime
	weekdays      []int16
	catchupPolicy string
	catchupGrace  time.Duration
	nextDueAt     time.Time
	skipNextAt    *time.Time
}

func NewReconciler(
	pool *pgxpool.Pool,
	configuredClock clock.Clock,
	lockNamespace string,
	enqueuer OccurrenceEnqueuer,
	options ...ReconcilerOption,
) *Reconciler {
	reconciler := &Reconciler{
		pool:          pool,
		clock:         configuredClock,
		lockNamespace: lockNamespace,
		enqueuer:      enqueuer,
	}
	for _, option := range options {
		option(reconciler)
	}
	return reconciler
}

func (reconciler *Reconciler) Reconcile(ctx context.Context, runID string) (ReconcileResult, error) {
	tx, err := reconciler.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("begin scheduler reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var acquired bool
	if err := tx.QueryRow(
		ctx,
		"select pg_try_advisory_xact_lock(hashtextextended($1, 0))",
		reconciler.lockNamespace,
	).Scan(&acquired); err != nil {
		return ReconcileResult{}, fmt.Errorf("acquire scheduler advisory lock: %w", err)
	}
	if !acquired {
		return ReconcileResult{}, nil
	}
	result := ReconcileResult{LockAcquired: true}

	now := reconciler.clock.Now().UTC()
	rows, err := tx.Query(ctx, `
		select
			id::text,
			timezone,
			to_char(local_time, 'HH24:MI'),
			days_of_week,
			catchup_policy,
			(extract(epoch from catchup_grace) * 1000000)::bigint,
			next_due_at,
			skip_next_at
		from app.schedule_definitions
		where enabled
			and paused_at is null
			and next_due_at <= $1
			and (coalesce(cardinality($3::uuid[]), 0) = 0 or id = any($3::uuid[]))
		order by next_due_at, id
		for update skip locked
		limit $2`, now, reconciliationBatchSize, reconciler.scheduleIDs)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("select due schedules: %w", err)
	}
	var schedules []dueSchedule
	for rows.Next() {
		var schedule dueSchedule
		var localClock string
		var catchupGraceMicroseconds int64
		if err := rows.Scan(
			&schedule.id,
			&schedule.timezone,
			&localClock,
			&schedule.weekdays,
			&schedule.catchupPolicy,
			&catchupGraceMicroseconds,
			&schedule.nextDueAt,
			&schedule.skipNextAt,
		); err != nil {
			return ReconcileResult{}, fmt.Errorf("scan due schedule: %w", err)
		}
		parsedLocalTime, err := parseLocalTime(localClock)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("parse schedule %s local time: %w", schedule.id, err)
		}
		schedule.localTime = parsedLocalTime
		schedule.catchupGrace = time.Duration(catchupGraceMicroseconds) * time.Microsecond
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReconcileResult{}, fmt.Errorf("iterate due schedules: %w", err)
	}
	rows.Close()

	for _, schedule := range schedules {
		definition, err := NewDefinition(schedule.timezone, schedule.localTime, schedule.weekdays)
		if err != nil {
			return ReconcileResult{}, fmt.Errorf("validate schedule %s: %w", schedule.id, err)
		}
		nextDueAt := schedule.nextDueAt
		clearSkipNext := false
		for range maximumOccurrencesPerScheduleRun {
			if nextDueAt.After(now) {
				break
			}
			created, enqueued, skipped, err := reconciler.reconcileOccurrence(
				ctx,
				tx,
				schedule,
				nextDueAt,
				now,
				runID,
			)
			if err != nil {
				return ReconcileResult{}, err
			}
			if created {
				result.OccurrencesCreated++
			}
			if enqueued {
				result.JobsEnqueued++
			}
			clearSkipNext = clearSkipNext || skipped

			nextDueAt, err = NextOccurrence(nextDueAt, definition)
			if err != nil {
				return ReconcileResult{}, fmt.Errorf("calculate next occurrence for schedule %s: %w", schedule.id, err)
			}
		}
		if _, err := tx.Exec(ctx, `
			update app.schedule_definitions
			set next_due_at = $2,
				skip_next_at = case when $4 then null else skip_next_at end,
				version = version + 1,
				updated_at = $3
			where id = $1::uuid`, schedule.id, nextDueAt, now, clearSkipNext); err != nil {
			return ReconcileResult{}, fmt.Errorf("advance schedule %s: %w", schedule.id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ReconcileResult{}, fmt.Errorf("commit scheduler reconciliation: %w", err)
	}
	return result, nil
}

func (reconciler *Reconciler) reconcileOccurrence(
	ctx context.Context,
	tx pgx.Tx,
	schedule dueSchedule,
	scheduledFor time.Time,
	now time.Time,
	runID string,
) (bool, bool, bool, error) {
	location, err := time.LoadLocation(schedule.timezone)
	if err != nil {
		return false, false, false, fmt.Errorf("load schedule %s timezone: %w", schedule.id, err)
	}
	localized := scheduledFor.In(location)
	_, offsetSeconds := localized.Zone()
	idempotencyKey := schedule.id + ":" + scheduledFor.UTC().Format(time.RFC3339Nano)
	initialState, triggerType, shouldEnqueue, skipped := classifyOccurrence(schedule, scheduledFor, now)

	var occurrenceID string
	var occurrenceState string
	var occurrenceInserted bool
	if err := tx.QueryRow(ctx, `
		with inserted as (
			insert into app.schedule_occurrences (
				schedule_id,
				scheduled_for,
				local_date,
				local_offset_seconds,
				state,
				trigger_type,
				idempotency_key
			) values ($1::uuid, $2, $3, $4, $5, $6, $7)
			on conflict (schedule_id, scheduled_for) do nothing
			returning id, state
		)
		select id::text, state, true from inserted
		union all
		select id::text, state, false
		from app.schedule_occurrences
		where schedule_id = $1::uuid
			and scheduled_for = $2
			and not exists (select 1 from inserted)
		limit 1`,
		schedule.id,
		scheduledFor.UTC(),
		localized.Format(time.DateOnly),
		offsetSeconds,
		initialState,
		triggerType,
		idempotencyKey,
	).Scan(&occurrenceID, &occurrenceState, &occurrenceInserted); err != nil {
		return false, false, false, fmt.Errorf("insert occurrence for schedule %s: %w", schedule.id, err)
	}

	if occurrenceState != "due" {
		return occurrenceInserted, false, skipped, nil
	}
	if !shouldEnqueue {
		if _, err := tx.Exec(ctx, `
			update app.schedule_occurrences
			set state = $2, trigger_type = $3
			where id = $1::uuid and state = 'due'`, occurrenceID, initialState, triggerType); err != nil {
			return false, false, false, fmt.Errorf("finalize occurrence %s without enqueue: %w", occurrenceID, err)
		}
		return occurrenceInserted, false, skipped, nil
	}

	jobID, jobInserted, err := reconciler.enqueuer.EnqueueScheduleOccurrence(ctx, tx, occurrenceID, runID)
	if err != nil {
		return false, false, false, err
	}
	if _, err := tx.Exec(ctx, `
		update app.schedule_occurrences
		set state = 'enqueued',
			river_job_id = $2::bigint
		where id = $1::uuid and state = 'due'`, occurrenceID, jobID); err != nil {
		return false, false, false, fmt.Errorf("link occurrence %s to River job: %w", occurrenceID, err)
	}
	return occurrenceInserted, jobInserted, skipped, nil
}

func classifyOccurrence(
	schedule dueSchedule,
	scheduledFor time.Time,
	now time.Time,
) (string, string, bool, bool) {
	if schedule.skipNextAt != nil && schedule.skipNextAt.Equal(scheduledFor) {
		return "skipped", "scheduled", false, true
	}
	if !now.After(scheduledFor) {
		return "due", "scheduled", true, false
	}
	if schedule.catchupPolicy == "catch_up" && now.Sub(scheduledFor) <= schedule.catchupGrace {
		return "due", "catch_up", true, false
	}
	return "missed", "scheduled", false, false
}

func parseLocalTime(value string) (LocalTime, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return LocalTime{}, fmt.Errorf("expected HH:MM, got %q", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return LocalTime{}, fmt.Errorf("parse hour: %w", err)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return LocalTime{}, fmt.Errorf("parse minute: %w", err)
	}
	return LocalTime{Hour: hour, Minute: minute}, nil
}
