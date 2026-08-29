package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/service"
)

type SchedulerHealth struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	interval time.Duration
	lastOK   atomic.Int64
}

func NewSchedulerHealth(
	pool *pgxpool.Pool,
	configuredClock clock.Clock,
	interval time.Duration,
) *SchedulerHealth {
	return &SchedulerHealth{pool: pool, clock: configuredClock, interval: interval}
}

func (health *SchedulerHealth) RecordSuccess() {
	health.lastOK.Store(health.clock.Now().UTC().UnixNano())
}

type Runner struct {
	client          *river.Client[pgx.Tx]
	pool            *pgxpool.Pool
	health          *SchedulerHealth
	newRunID        func() string
	shutdownTimeout time.Duration
}

func NewRunner(
	client *river.Client[pgx.Tx],
	pool *pgxpool.Pool,
	health *SchedulerHealth,
	shutdownTimeout time.Duration,
) *Runner {
	return &Runner{
		client:          client,
		pool:            pool,
		health:          health,
		newRunID:        uuid.NewString,
		shutdownTimeout: shutdownTimeout,
	}
}

func (runner *Runner) Once(ctx context.Context, logger *slog.Logger) error {
	return runner.once(ctx, logger, runner.newRunID())
}

func (runner *Runner) once(ctx context.Context, logger *slog.Logger, runID string) error {
	result, err := runner.client.Insert(
		ctx,
		jobqueue.ReconcileSchedulesArgs{RunID: runID},
		nil,
	)
	if err != nil {
		return fmt.Errorf("insert one-shot schedule reconciliation: %w", err)
	}
	if err := runner.client.Start(ctx); err != nil {
		return fmt.Errorf("start one-shot River client: %w", err)
	}

	cycleErr := runner.waitForCycle(ctx, result.Job.ID, runID)
	stopContext, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), runner.shutdownTimeout)
	defer cancelStop()
	if stopErr := runner.client.Stop(stopContext); stopErr != nil {
		if cycleErr != nil {
			return fmt.Errorf("%w; stop one-shot River client: %v", cycleErr, stopErr)
		}
		return fmt.Errorf("stop one-shot River client: %w", stopErr)
	}
	if cycleErr != nil {
		return cycleErr
	}
	logger.InfoContext(ctx, "one-shot River cycle complete", "reconcile_job_id", result.Job.ID)
	return nil
}

func (runner *Runner) Run(ctx context.Context, logger *slog.Logger, port uint16) error {
	serverError := make(chan error, 1)
	go func() {
		serverError <- service.RunHTTP(ctx, logger, service.HTTPServerConfig{
			Host:            "0.0.0.0",
			Port:            port,
			Handler:         runner.healthHandler(),
			ShutdownTimeout: runner.shutdownTimeout,
		})
	}()

	if err := runner.client.Start(ctx); err != nil {
		return fmt.Errorf("start River client: %w", err)
	}

	select {
	case err := <-serverError:
		stopContext, cancelStop := context.WithTimeout(context.Background(), runner.shutdownTimeout)
		defer cancelStop()
		_ = runner.client.StopAndCancel(stopContext)
		return err
	case <-runner.client.Stopped():
		if ctx.Err() == nil {
			return errors.New("River client stopped unexpectedly")
		}
	case <-ctx.Done():
	}

	stopContext, cancelStop := context.WithTimeout(context.Background(), runner.shutdownTimeout)
	defer cancelStop()
	if err := runner.client.Stop(stopContext); err != nil {
		return fmt.Errorf("stop River client: %w", err)
	}
	return <-serverError
}

func (runner *Runner) waitForCycle(ctx context.Context, reconcileJobID int64, runID string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var reconcileState string
		var activeCount int64
		var failedCount int64
		err := runner.pool.QueryRow(ctx, `
			select
				(select state::text from river.river_job where id = $1),
				count(*) filter (where state not in ('cancelled', 'completed', 'discarded')),
				count(*) filter (where state in ('cancelled', 'discarded'))
			from river.river_job
		where id = $1
			or (kind = $3 and args ->> 'runId' = $2)`,
			reconcileJobID,
			runID,
			jobqueue.ScheduleOccurrenceKind,
		).Scan(&reconcileState, &activeCount, &failedCount)
		if err != nil {
			return fmt.Errorf("inspect one-shot River cycle: %w", err)
		}
		if failedCount > 0 {
			return fmt.Errorf("one-shot River cycle produced %d cancelled or discarded jobs", failedCount)
		}
		if reconcileState == "completed" && activeCount == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for one-shot River cycle: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (runner *Runner) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"service": "worker", "status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		lastOKUnixNano := runner.health.lastOK.Load()
		if lastOKUnixNano == 0 || runner.health.clock.Now().Sub(time.Unix(0, lastOKUnixNano)) > 3*runner.health.interval {
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "No successful scheduler tick is recorded in the last three intervals.")
			return
		}

		readinessContext, cancel := context.WithTimeout(request.Context(), time.Second)
		defer cancel()
		var oldestOverdue *time.Time
		if err := runner.pool.QueryRow(readinessContext, `
			select min(scheduled_for)
			from app.schedule_occurrences
			where state in ('due', 'enqueued', 'preparing', 'ready', 'delivering', 'failed')
				and scheduled_for < now()`).Scan(&oldestOverdue); err != nil {
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The occurrence ledger could not be inspected.")
			return
		}

		payload := map[string]any{
			"lastSuccessfulSchedulerTick": time.Unix(0, lastOKUnixNano).UTC(),
			"status":                      "ready",
		}
		if oldestOverdue != nil {
			payload["oldestOverdueOccurrence"] = oldestOverdue.UTC()
		}
		httpx.WriteJSON(response, http.StatusOK, payload)
	})
	return httpx.SecurityHeaders(mux)
}
