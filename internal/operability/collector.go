package operability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Snapshot struct {
	SourcePollDue          int64     `json:"sourcePollDue"`
	SourcePollSuccess      int64     `json:"sourcePollSuccess"`
	SourcePollFailure      int64     `json:"sourcePollFailure"`
	PriorityFreshness      float64   `json:"priorityFreshnessSeconds"`
	ParseFailures          int64     `json:"parseFailures"`
	QueueDepth             int64     `json:"queueDepth"`
	OldestJobAge           float64   `json:"oldestJobAgeSeconds"`
	AIRuns                 int64     `json:"aiRuns"`
	AISchemaFailures       int64     `json:"aiSchemaFailures"`
	AICostUSD              float64   `json:"aiCostUsd"`
	AITokens               int64     `json:"aiTokens"`
	DigestDeliveries       int64     `json:"digestDeliveries"`
	DigestFailures         int64     `json:"digestFailures"`
	DelayedDigests         int64     `json:"delayedDigests"`
	OutboxLag              float64   `json:"outboxLagSeconds"`
	Feedback               int64     `json:"feedback"`
	LastRetentionState     string    `json:"lastRetentionState"`
	LastRetentionCompleted time.Time `json:"lastRetentionCompleted,omitempty"`
	LastRestoreState       string    `json:"lastRestoreState"`
	LastRestoreCompleted   time.Time `json:"lastRestoreCompleted,omitempty"`
	LastRestoreRPOSeconds  int64     `json:"lastRestoreRpoSeconds"`
	LastRestoreRTOSeconds  int64     `json:"lastRestoreRtoSeconds"`
	LastRestoreRPOTarget   int64     `json:"lastRestoreRpoTargetSeconds"`
	LastRestoreRTOTarget   int64     `json:"lastRestoreRtoTargetSeconds"`
	GeneratedAt            time.Time `json:"generatedAt"`
}

type Alert struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

type Collector struct {
	pool *pgxpool.Pool
}

func NewCollector(pool *pgxpool.Pool) (*Collector, error) {
	if pool == nil {
		return nil, errors.New("operability collector requires a database pool")
	}
	return &Collector{pool: pool}, nil
}

func (collector *Collector) Collect(ctx context.Context, now time.Time) (Snapshot, error) {
	now = now.UTC()
	snapshot := Snapshot{GeneratedAt: now, LastRetentionState: "not_recorded", LastRestoreState: "not_recorded"}
	err := collector.pool.QueryRow(ctx, `
		select
			(select count(*)::bigint from app.source_endpoints where health_state <> 'paused' and next_poll_at <= $1),
			(select count(*)::bigint from app.source_fetches where outcome <> 'failed' and attempted_at >= $1 - interval '24 hours'),
			(select count(*)::bigint from app.source_fetches where outcome = 'failed' and attempted_at >= $1 - interval '24 hours'),
			coalesce((
				select max(extract(epoch from ($1 - coalesce(success.last_success_at, endpoint.created_at))))
				from app.source_endpoints endpoint
				left join lateral (
					select max(fetch_record.completed_at) as last_success_at
					from app.source_fetches fetch_record
					where fetch_record.endpoint_id = endpoint.id and fetch_record.outcome <> 'failed'
				) success on true
				where endpoint.priority = 'p0' and endpoint.health_state <> 'paused'
			), 0),
			(select count(*)::bigint from app.source_parse_attempts where outcome = 'failed' and attempted_at >= $1 - interval '24 hours'),
			(select count(*)::bigint from river.river_job where state in ('available', 'pending', 'retryable', 'running', 'scheduled')),
			coalesce((select extract(epoch from ($1 - min(created_at))) from river.river_job where state in ('available', 'pending', 'retryable')), 0),
			(select count(*)::bigint from app.ai_runs where started_at >= date_trunc('month', $1)),
			(select count(*)::bigint from app.ai_runs where started_at >= $1 - interval '24 hours' and error_code like '%schema%'),
			coalesce((select sum(estimated_cost_usd)::double precision from app.ai_run_attempts where started_at >= date_trunc('month', $1)), 0),
			coalesce((select sum(input_tokens + output_tokens)::bigint from app.ai_run_attempts where started_at >= date_trunc('month', $1)), 0),
			(select count(*)::bigint from app.delivery_attempts where state = 'delivered'),
			(select count(*)::bigint from app.delivery_attempts where state = 'failed'),
			(select count(*)::bigint from app.schedule_occurrences occurrence join app.schedule_definitions schedule on schedule.id = occurrence.schedule_id where schedule.schedule_type = 'daily_digest' and occurrence.state not in ('delivered', 'skipped', 'missed') and occurrence.scheduled_for < $1 - interval '15 minutes'),
			coalesce((select extract(epoch from ($1 - min(created_at))) from app.outbox_events), 0),
			(select count(*)::bigint from app.feedback)`, now).Scan(
		&snapshot.SourcePollDue,
		&snapshot.SourcePollSuccess,
		&snapshot.SourcePollFailure,
		&snapshot.PriorityFreshness,
		&snapshot.ParseFailures,
		&snapshot.QueueDepth,
		&snapshot.OldestJobAge,
		&snapshot.AIRuns,
		&snapshot.AISchemaFailures,
		&snapshot.AICostUSD,
		&snapshot.AITokens,
		&snapshot.DigestDeliveries,
		&snapshot.DigestFailures,
		&snapshot.DelayedDigests,
		&snapshot.OutboxLag,
		&snapshot.Feedback,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("collect operational metrics: %w", err)
	}
	if err := collector.loadRetention(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := collector.loadRestore(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (collector *Collector) loadRetention(ctx context.Context, snapshot *Snapshot) error {
	var completedAt *time.Time
	err := collector.pool.QueryRow(ctx, `
		select state, completed_at
		from app.retention_runs
		order by started_at desc, id desc
		limit 1`).Scan(&snapshot.LastRetentionState, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("collect retention metric: %w", err)
	}
	if completedAt != nil {
		snapshot.LastRetentionCompleted = completedAt.UTC()
	}
	return nil
}

func (collector *Collector) loadRestore(ctx context.Context, snapshot *Snapshot) error {
	err := collector.pool.QueryRow(ctx, `
		select state, completed_at, rpo_seconds, rto_seconds, rpo_target_seconds, rto_target_seconds
		from app.restore_drills
		order by completed_at desc, id desc
		limit 1`).Scan(
		&snapshot.LastRestoreState,
		&snapshot.LastRestoreCompleted,
		&snapshot.LastRestoreRPOSeconds,
		&snapshot.LastRestoreRTOSeconds,
		&snapshot.LastRestoreRPOTarget,
		&snapshot.LastRestoreRTOTarget,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("collect restore metric: %w", err)
	}
	snapshot.LastRestoreCompleted = snapshot.LastRestoreCompleted.UTC()
	return nil
}

func Evaluate(snapshot Snapshot, now time.Time) []Alert {
	alerts := make([]Alert, 0)
	appendAlert := func(condition bool, name string, severity string, detail string) {
		if condition {
			alerts = append(alerts, Alert{Name: name, Severity: severity, Detail: detail})
		}
	}
	appendAlert(snapshot.PriorityFreshness > 15*60, "priority_source_freshness", "warning", "Priority source freshness exceeds 15 minutes.")
	appendAlert(snapshot.OldestJobAge > 15*60, "queue_backlog", "warning", "The oldest retryable or available job exceeds 15 minutes.")
	appendAlert(snapshot.DelayedDigests > 0, "digest_delayed", "warning", "A daily digest is more than 15 minutes late.")
	appendAlert(snapshot.LastRetentionState == "failed", "retention_failed", "warning", "The latest retention run failed.")
	appendAlert(snapshot.LastRestoreState == "failed", "restore_integrity", "critical", "The latest database restore drill failed.")
	appendAlert(
		snapshot.LastRestoreState == "passed" &&
			(snapshot.LastRestoreRPOSeconds > snapshot.LastRestoreRPOTarget || snapshot.LastRestoreRTOSeconds > snapshot.LastRestoreRTOTarget),
		"restore_objective",
		"critical",
		"The latest database restore drill missed its RPO or RTO.",
	)
	appendAlert(
		snapshot.LastRestoreState == "passed" && now.UTC().Sub(snapshot.LastRestoreCompleted) > 35*24*time.Hour,
		"restore_drill_stale",
		"warning",
		"No successful database restore drill is recorded in the last 35 days.",
	)
	return alerts
}

func WritePrometheus(writer io.Writer, snapshot Snapshot) error {
	metrics := []struct {
		name  string
		value any
	}{
		{"source_poll_due_total", snapshot.SourcePollDue},
		{"source_poll_success_total", snapshot.SourcePollSuccess},
		{"source_poll_failure_total", snapshot.SourcePollFailure},
		{"source_freshness_seconds", snapshot.PriorityFreshness},
		{"parse_failure_total", snapshot.ParseFailures},
		{"river_queue_depth", snapshot.QueueDepth},
		{"river_job_latency_seconds", snapshot.OldestJobAge},
		{"ai_run_total", snapshot.AIRuns},
		{"ai_schema_failure_total", snapshot.AISchemaFailures},
		{"ai_cost_usd", snapshot.AICostUSD},
		{"ai_tokens_total", snapshot.AITokens},
		{"digest_delivery_total", snapshot.DigestDeliveries},
		{"digest_delivery_failure_total", snapshot.DigestFailures},
		{"digest_delayed_total", snapshot.DelayedDigests},
		{"outbox_lag_seconds", snapshot.OutboxLag},
		{"feedback_total", snapshot.Feedback},
		{"restore_rpo_seconds", snapshot.LastRestoreRPOSeconds},
		{"restore_rto_seconds", snapshot.LastRestoreRTOSeconds},
	}
	for _, metric := range metrics {
		if _, err := fmt.Fprintf(writer, "# TYPE %s gauge\n%s %v\n", metric.name, metric.name, metric.value); err != nil {
			return fmt.Errorf("write metric %s: %w", metric.name, err)
		}
	}
	return nil
}
