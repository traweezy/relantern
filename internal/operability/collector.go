package operability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/sources"
)

type Snapshot struct {
	SourcePollDue               int64     `json:"sourcePollDue"`
	SourcePollSuccess           int64     `json:"sourcePollSuccess"`
	SourcePollFailure           int64     `json:"sourcePollFailure"`
	PriorityFreshness           float64   `json:"priorityFreshnessSeconds"`
	ParseFailures               int64     `json:"parseFailures"`
	QueueDepth                  int64     `json:"queueDepth"`
	OldestJobAge                float64   `json:"oldestJobAgeSeconds"`
	AIRuns                      int64     `json:"aiRuns"`
	AISchemaFailures            int64     `json:"aiSchemaFailures"`
	AICostUSD                   float64   `json:"aiCostUsd"`
	AITokens                    int64     `json:"aiTokens"`
	DigestDeliveries            int64     `json:"digestDeliveries"`
	DigestFailures              int64     `json:"digestFailures"`
	DelayedDigests              int64     `json:"delayedDigests"`
	UndeliveredCritical         int64     `json:"undeliveredCriticalAlerts"`
	CorrectedCritical           int64     `json:"correctedCriticalAlerts"`
	SuppressedDeliveries        int64     `json:"suppressedCriticalDeliveries"`
	AdvisoryScanPending         int64     `json:"advisoryScanPending"`
	AdvisoryScanAge             float64   `json:"advisoryScanAgeSeconds"`
	AdvisoryScanIssues          int64     `json:"advisoryScanIssues"`
	AdvisoryObservationsPending int64     `json:"advisoryObservationsPending"`
	AdvisoryObservationAge      float64   `json:"advisoryObservationOldestAgeSeconds"`
	OutboxLag                   float64   `json:"outboxLagSeconds"`
	Feedback                    int64     `json:"feedback"`
	LastRetentionState          string    `json:"lastRetentionState"`
	LastRetentionCompleted      time.Time `json:"lastRetentionCompleted,omitempty"`
	LastRestoreState            string    `json:"lastRestoreState"`
	LastRestoreCompleted        time.Time `json:"lastRestoreCompleted,omitempty"`
	LastRestoreRPOSeconds       int64     `json:"lastRestoreRpoSeconds"`
	LastRestoreRTOSeconds       int64     `json:"lastRestoreRtoSeconds"`
	LastRestoreRPOTarget        int64     `json:"lastRestoreRpoTargetSeconds"`
	LastRestoreRTOTarget        int64     `json:"lastRestoreRtoTargetSeconds"`
	GeneratedAt                 time.Time `json:"generatedAt"`
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
			(select count(*)::bigint
			 from app.source_endpoints endpoint
			 join app.sources source on source.id = endpoint.source_id
			 left join app.source_runtime_overrides runtime on runtime.source_id = source.id
			 where source.enabled and source.validation_state = 'active'
			   and coalesce(runtime.polling_enabled, true)
			   and endpoint.health_state not in ('paused', 'failed')
			   and (endpoint.next_poll_at is null or endpoint.next_poll_at <= $1)),
			(select count(*)::bigint from app.source_fetches where outcome <> 'failed' and attempted_at >= $1 - interval '24 hours'),
			(select count(*)::bigint from app.source_fetches where outcome = 'failed' and attempted_at >= $1 - interval '24 hours'),
			coalesce((
				select max(extract(epoch from ($1 - coalesce(success.last_success_at, endpoint.created_at))))
				from app.source_endpoints endpoint
				join app.sources source on source.id = endpoint.source_id
				left join app.source_runtime_overrides runtime on runtime.source_id = source.id
				left join lateral (
					select max(fetch_record.completed_at) as last_success_at
					from app.source_fetches fetch_record
					where fetch_record.endpoint_id = endpoint.id and fetch_record.outcome <> 'failed'
				) success on true
				where endpoint.priority = 'critical'
				  and source.enabled and source.validation_state = 'active'
				  and coalesce(runtime.polling_enabled, true)
				  and endpoint.health_state <> 'paused'
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
			(select count(*)::bigint from app.critical_alert_deliveries delivery
			 join app.critical_alerts alert on alert.id = delivery.alert_id
			 where alert.corrected_at is null and delivery.state not in ('sent', 'suppressed') and (
			   delivery.state = 'permanent' or
			   coalesce((select min(attempted_at) from app.critical_alert_attempts attempt
			     where attempt.delivery_id = delivery.id),
			     delivery.last_attempt_at, delivery.next_attempt_at) <= $1::timestamptz - interval '10 minutes'
			 )),
			(select count(*)::bigint from app.critical_alerts where corrected_at is not null),
			(select count(*)::bigint from app.critical_alert_deliveries where state = 'suppressed'),
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
		&snapshot.UndeliveredCritical,
		&snapshot.CorrectedCritical,
		&snapshot.SuppressedDeliveries,
		&snapshot.OutboxLag,
		&snapshot.Feedback,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("collect operational metrics: %w", err)
	}
	if err := collector.loadAdvisoryScan(ctx, &snapshot, now); err != nil {
		return Snapshot{}, err
	}
	if err := collector.loadAdvisoryObservations(ctx, &snapshot, now); err != nil {
		return Snapshot{}, err
	}
	if err := collector.loadRetention(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := collector.loadRestore(ctx, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (collector *Collector) loadAdvisoryScan(ctx context.Context, snapshot *Snapshot, now time.Time) error {
	err := collector.pool.QueryRow(ctx, `
		select count(*)::bigint,
			coalesce(max(extract(epoch from ($1::timestamptz -
				coalesce(nullif(checkpoint.provider_state->>'advisoryScanStartedAt', '')::timestamptz,
					checkpoint.updated_at)))), 0),
			count(*) filter (where checkpoint.provider_state->>'advisoryScanIssue' is not null)::bigint
		from app.source_endpoints endpoint
		join app.sources source on source.id = endpoint.source_id
		join app.source_checkpoints checkpoint on checkpoint.endpoint_id = endpoint.id
		left join app.source_runtime_overrides runtime on runtime.source_id = source.id
		where endpoint.url = $2 and checkpoint.cursor is not null
			and source.enabled and source.validation_state = 'active'
			and coalesce(runtime.polling_enabled, true)
			and endpoint.health_state <> 'paused'`, now, sources.GlobalReviewedAdvisoriesURL).Scan(
		&snapshot.AdvisoryScanPending, &snapshot.AdvisoryScanAge, &snapshot.AdvisoryScanIssues,
	)
	if err != nil {
		return fmt.Errorf("collect reviewed advisory pagination metric: %w", err)
	}
	return nil
}

func (collector *Collector) loadAdvisoryObservations(ctx context.Context, snapshot *Snapshot, now time.Time) error {
	err := collector.pool.QueryRow(ctx, `
		select count(*)::bigint,
			coalesce(greatest(extract(epoch from ($1::timestamptz - min(observed_at))), 0), 0)
		from app.advisory_collection_observations
		where state = 'pending'`, now).Scan(
		&snapshot.AdvisoryObservationsPending, &snapshot.AdvisoryObservationAge,
	)
	if err != nil {
		return fmt.Errorf("collect pending advisory observation metric: %w", err)
	}
	return nil
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
	appendAlert(snapshot.UndeliveredCritical > 0, "critical_advisory_undelivered", "critical", "A confirmed critical advisory external delivery is overdue or permanently failed.")
	appendAlert(snapshot.AdvisoryScanPending > 0 && snapshot.AdvisoryScanAge > 24*3600,
		"reviewed_advisory_scan_stalled", "warning", "Reviewed GitHub advisory pagination has not completed within 24 hours.")
	appendAlert(snapshot.AdvisoryScanIssues > 0, "reviewed_advisory_scan_invalid", "warning", "Reviewed GitHub advisory pagination encountered a cursor cycle or page limit.")
	appendAlert(snapshot.AdvisoryObservationsPending > 0 && snapshot.AdvisoryObservationAge > 10*60,
		"advisory_observation_stalled", "warning", "An official advisory observation has awaited complete assessment for more than ten minutes.")
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
		{"critical_alert_undelivered_count", snapshot.UndeliveredCritical},
		{"critical_alert_corrected_count", snapshot.CorrectedCritical},
		{"critical_alert_delivery_suppressed_count", snapshot.SuppressedDeliveries},
		{"reviewed_advisory_scan_pending", snapshot.AdvisoryScanPending},
		{"reviewed_advisory_scan_age_seconds", snapshot.AdvisoryScanAge},
		{"reviewed_advisory_scan_issues", snapshot.AdvisoryScanIssues},
		{"advisory_observation_pending", snapshot.AdvisoryObservationsPending},
		{"advisory_observation_oldest_age_seconds", snapshot.AdvisoryObservationAge},
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
