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

const reconciliationBatchSize = 100

type Reconciler struct {
	pool          *pgxpool.Pool
	clock         clock.Clock
	lockNamespace string
}

type dueSchedule struct {
	id        string
	timezone  string
	localTime LocalTime
	weekdays  []int16
	nextDueAt time.Time
}

func NewReconciler(pool *pgxpool.Pool, configuredClock clock.Clock, lockNamespace string) *Reconciler {
	return &Reconciler{pool: pool, clock: configuredClock, lockNamespace: lockNamespace}
}

func (reconciler *Reconciler) Reconcile(ctx context.Context) (int64, error) {
	tx, err := reconciler.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("begin scheduler reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var acquired bool
	if err := tx.QueryRow(
		ctx,
		"select pg_try_advisory_xact_lock(hashtextextended($1, 0))",
		reconciler.lockNamespace,
	).Scan(&acquired); err != nil {
		return 0, fmt.Errorf("acquire scheduler advisory lock: %w", err)
	}
	if !acquired {
		return 0, nil
	}

	now := reconciler.clock.Now().UTC()
	rows, err := tx.Query(ctx, `
		select
			id::text,
			timezone,
			to_char(local_time, 'HH24:MI'),
			days_of_week,
			next_due_at
		from app.schedule_definitions
		where enabled
			and paused_at is null
			and next_due_at <= $1
		order by next_due_at, id
		for update skip locked
		limit $2`, now, reconciliationBatchSize)
	if err != nil {
		return 0, fmt.Errorf("select due schedules: %w", err)
	}
	var schedules []dueSchedule
	for rows.Next() {
		var schedule dueSchedule
		var localClock string
		if err := rows.Scan(
			&schedule.id,
			&schedule.timezone,
			&localClock,
			&schedule.weekdays,
			&schedule.nextDueAt,
		); err != nil {
			return 0, fmt.Errorf("scan due schedule: %w", err)
		}
		parsedLocalTime, err := parseLocalTime(localClock)
		if err != nil {
			return 0, fmt.Errorf("parse schedule %s local time: %w", schedule.id, err)
		}
		schedule.localTime = parsedLocalTime
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due schedules: %w", err)
	}
	rows.Close()

	var inserted int64
	for _, schedule := range schedules {
		definition, err := NewDefinition(schedule.timezone, schedule.localTime, schedule.weekdays)
		if err != nil {
			return 0, fmt.Errorf("validate schedule %s: %w", schedule.id, err)
		}
		location, err := time.LoadLocation(schedule.timezone)
		if err != nil {
			return 0, fmt.Errorf("load schedule %s timezone: %w", schedule.id, err)
		}
		localized := schedule.nextDueAt.In(location)
		_, offsetSeconds := localized.Zone()
		idempotencyKey := schedule.id + ":" + schedule.nextDueAt.UTC().Format(time.RFC3339Nano)

		result, err := tx.Exec(ctx, `
			insert into app.schedule_occurrences (
				schedule_id,
				scheduled_for,
				local_date,
				local_offset_seconds,
				state,
				trigger_type,
				idempotency_key
			) values ($1::uuid, $2, $3, $4, 'due', 'scheduled', $5)
			on conflict do nothing`,
			schedule.id,
			schedule.nextDueAt.UTC(),
			localized.Format(time.DateOnly),
			offsetSeconds,
			idempotencyKey,
		)
		if err != nil {
			return 0, fmt.Errorf("insert occurrence for schedule %s: %w", schedule.id, err)
		}
		inserted += result.RowsAffected()

		nextDueAt, err := NextOccurrence(schedule.nextDueAt, definition)
		if err != nil {
			return 0, fmt.Errorf("calculate next occurrence for schedule %s: %w", schedule.id, err)
		}
		if _, err := tx.Exec(ctx, `
			update app.schedule_definitions
			set next_due_at = $2, version = version + 1, updated_at = $3
			where id = $1::uuid`, schedule.id, nextDueAt, now); err != nil {
			return 0, fmt.Errorf("advance schedule %s: %w", schedule.id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit scheduler reconciliation: %w", err)
	}
	return inserted, nil
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
