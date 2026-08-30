package pgstore

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/traweezy/relantern/internal/controlplane"
)

func (store *Store) Operations(
	ctx context.Context,
	userID string,
	now time.Time,
	deployment controlplane.DeploymentMetadata,
) (controlplane.OperationsSnapshot, error) {
	snapshot := controlplane.OperationsSnapshot{
		GeneratedAt: now.UTC(),
		Deployment:  deployment,
		Restore: controlplane.RestoreStatus{
			State:       "not_recorded",
			Explanation: "No successful restore rehearsal is recorded yet; PR18 owns backup and restore evidence.",
		},
	}
	queues, err := store.queueDepths(ctx)
	if err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	snapshot.Queues = queues
	stuckJobs, err := store.stuckJobs(ctx, now)
	if err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	snapshot.StuckJobs = stuckJobs
	if err := store.pool.QueryRow(ctx, `
		select count(*)::bigint
		from app.ai_runs
		where background and state in ('pending', 'running', 'failed_retryable')`).Scan(
		&snapshot.OpenAIBackgroundPending,
	); err != nil {
		return controlplane.OperationsSnapshot{}, fmt.Errorf("count pending OpenAI background runs: %w", err)
	}
	if err := store.loadDeliveryAttemptCount(ctx, &snapshot.DeliveryAttempts); err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	errorBudgets, err := store.sourceErrorBudgets(ctx, now)
	if err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	snapshot.SourceErrorBudgets = errorBudgets
	schedules, err := store.listSchedules(ctx, userID)
	if err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	snapshot.Schedules = schedules
	occurrences, err := store.scheduleOccurrences(ctx, userID)
	if err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	snapshot.Occurrences = occurrences
	if err := store.loadSchedulerHealth(ctx, now, &snapshot); err != nil {
		return controlplane.OperationsSnapshot{}, err
	}
	return snapshot, nil
}

func (store *Store) queueDepths(ctx context.Context) ([]controlplane.QueueDepth, error) {
	queueNames := []string{"ai_fast", "ai_research", "critical", "delivery", "fetch", "maintenance", "parse"}
	byName := make(map[string]int, len(queueNames))
	queues := make([]controlplane.QueueDepth, len(queueNames))
	for index, name := range queueNames {
		queues[index].Queue = name
		byName[name] = index
	}
	rows, err := store.pool.Query(ctx, `
		select queue, state::text, count(*)::bigint
		from river.river_job
		group by queue, state
		order by queue, state`)
	if err != nil {
		return nil, fmt.Errorf("load queue depths: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var queue, state string
		var count int64
		if err := rows.Scan(&queue, &state, &count); err != nil {
			return nil, fmt.Errorf("scan queue depth: %w", err)
		}
		index, exists := byName[queue]
		if !exists {
			queues = append(queues, controlplane.QueueDepth{Queue: queue})
			index = len(queues) - 1
			byName[queue] = index
		}
		metric := &queues[index]
		switch state {
		case "available", "pending":
			metric.Available += count
		case "running":
			metric.Running += count
		case "retryable":
			metric.Retryable += count
		case "scheduled":
			metric.Scheduled += count
		case "completed":
			metric.Completed += count
		case "discarded":
			metric.Discarded += count
		case "cancelled":
			metric.Cancelled += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queue depths: %w", err)
	}
	return queues, nil
}

func (store *Store) stuckJobs(
	ctx context.Context,
	now time.Time,
) ([]controlplane.StuckJob, error) {
	rows, err := store.pool.Query(ctx, `
		select id, queue, kind, attempt, attempted_at
		from river.river_job
		where state = 'running'
			and attempted_at < $1::timestamptz - interval '2 minutes'
		order by attempted_at, id
		limit 50`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("load stuck jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]controlplane.StuckJob, 0)
	for rows.Next() {
		var job controlplane.StuckJob
		if err := rows.Scan(&job.ID, &job.Queue, &job.Kind, &job.Attempt, &job.AttemptedAt); err != nil {
			return nil, fmt.Errorf("scan stuck job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stuck jobs: %w", err)
	}
	return jobs, nil
}

func (store *Store) loadDeliveryAttemptCount(ctx context.Context, count *int64) error {
	var tableExists bool
	if err := store.pool.QueryRow(ctx, `select to_regclass('app.delivery_attempts') is not null`).Scan(&tableExists); err != nil {
		return fmt.Errorf("check delivery-attempt ledger: %w", err)
	}
	if !tableExists {
		*count = 0
		return nil
	}
	if err := store.pool.QueryRow(ctx, `select count(*)::bigint from app.delivery_attempts`).Scan(count); err != nil {
		return fmt.Errorf("count delivery attempts: %w", err)
	}
	return nil
}

func (store *Store) sourceErrorBudgets(
	ctx context.Context,
	now time.Time,
) ([]controlplane.SourceErrorBudget, error) {
	rows, err := store.pool.Query(ctx, `
		select
			source.id,
			source.name,
			count(fetch_record.id)::bigint,
			count(fetch_record.id) filter (where fetch_record.outcome = 'failed')::bigint
		from app.sources source
		left join app.source_endpoints endpoint on endpoint.source_id = source.id
		left join app.source_fetches fetch_record
			on fetch_record.endpoint_id = endpoint.id
			and fetch_record.attempted_at >= $1::timestamptz - interval '24 hours'
		group by source.id, source.name
		order by source.name, source.id`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("load source error budgets: %w", err)
	}
	defer rows.Close()
	budgets := make([]controlplane.SourceErrorBudget, 0)
	for rows.Next() {
		var budget controlplane.SourceErrorBudget
		if err := rows.Scan(&budget.SourceID, &budget.SourceName, &budget.Attempts, &budget.Failures); err != nil {
			return nil, fmt.Errorf("scan source error budget: %w", err)
		}
		if budget.Attempts > 0 {
			budget.FailureRate = math.Round(float64(budget.Failures)/float64(budget.Attempts)*10_000) / 10_000
		}
		budget.State = errorBudgetState(budget.Attempts-budget.Failures, budget.Failures)
		budgets = append(budgets, budget)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source error budgets: %w", err)
	}
	return budgets, nil
}

func (store *Store) scheduleOccurrences(
	ctx context.Context,
	userID string,
) ([]controlplane.ScheduleOccurrence, error) {
	rows, err := store.pool.Query(ctx, `
		select
			occurrence.id::text,
			occurrence.schedule_id::text,
			schedule.schedule_type,
			occurrence.scheduled_for,
			occurrence.local_date::text,
			occurrence.state,
			occurrence.trigger_type,
			occurrence.idempotency_key,
			occurrence.started_at,
			occurrence.completed_at,
			coalesce(occurrence.error_code, '')
		from app.schedule_occurrences occurrence
		join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id
		where schedule.user_id = $1::uuid
		order by occurrence.scheduled_for desc, occurrence.id desc
		limit 50`, userID)
	if err != nil {
		return nil, fmt.Errorf("load schedule occurrence ledger: %w", err)
	}
	defer rows.Close()
	occurrences := make([]controlplane.ScheduleOccurrence, 0)
	for rows.Next() {
		var occurrence controlplane.ScheduleOccurrence
		if err := rows.Scan(
			&occurrence.ID,
			&occurrence.ScheduleID,
			&occurrence.ScheduleType,
			&occurrence.ScheduledFor,
			&occurrence.LocalDate,
			&occurrence.State,
			&occurrence.TriggerType,
			&occurrence.IdempotencyKey,
			&occurrence.StartedAt,
			&occurrence.CompletedAt,
			&occurrence.ErrorCode,
		); err != nil {
			return nil, fmt.Errorf("scan schedule occurrence: %w", err)
		}
		occurrences = append(occurrences, occurrence)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule occurrences: %w", err)
	}
	return occurrences, nil
}

func (store *Store) loadSchedulerHealth(
	ctx context.Context,
	now time.Time,
	snapshot *controlplane.OperationsSnapshot,
) error {
	if err := store.pool.QueryRow(ctx, `
		select max(finalized_at)
		from river.river_job
		where kind = 'reconcile_schedules' and state = 'completed'`).Scan(&snapshot.SchedulerLastSuccess); err != nil {
		return fmt.Errorf("load scheduler reconciliation health: %w", err)
	}
	if err := store.pool.QueryRow(ctx, `
		select min(occurrence.scheduled_for)
		from app.schedule_occurrences occurrence
		where occurrence.state in ('due', 'enqueued', 'preparing', 'ready', 'delivering', 'failed')
			and occurrence.scheduled_for < $1`, now.UTC()).Scan(&snapshot.OldestOverdueOccurrence); err != nil {
		return fmt.Errorf("load overdue schedule health: %w", err)
	}
	return nil
}
