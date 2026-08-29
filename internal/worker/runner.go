package worker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/scheduler"
	"github.com/traweezy/relantern/internal/service"
)

type Runner struct {
	reconciler *scheduler.Reconciler
	processor  *Processor
	interval   time.Duration
	lastOKUnix atomic.Int64
}

func NewRunner(reconciler *scheduler.Reconciler, processor *Processor, interval time.Duration) *Runner {
	return &Runner{reconciler: reconciler, processor: processor, interval: interval}
}

func (runner *Runner) Once(ctx context.Context, logger *slog.Logger) error {
	created, err := runner.reconciler.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("reconcile schedules: %w", err)
	}
	delivered, err := runner.processor.DeliverDue(ctx)
	if err != nil {
		return fmt.Errorf("deliver due occurrences: %w", err)
	}
	runner.lastOKUnix.Store(time.Now().UTC().Unix())
	logger.InfoContext(ctx, "worker reconciliation complete", "occurrences_created", created, "deliveries_captured", delivered)
	return nil
}

func (runner *Runner) Run(ctx context.Context, logger *slog.Logger, port uint16, shutdownTimeout time.Duration) error {
	serverError := make(chan error, 1)
	go func() {
		serverError <- service.RunHTTP(ctx, logger, service.HTTPServerConfig{
			Host:            "0.0.0.0",
			Port:            port,
			Handler:         runner.healthHandler(),
			ShutdownTimeout: shutdownTimeout,
		})
	}()

	if err := runner.Once(ctx, logger); err != nil {
		logger.ErrorContext(ctx, "initial worker reconciliation failed", "error", err)
	}
	ticker := time.NewTicker(runner.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return <-serverError
		case err := <-serverError:
			return err
		case <-ticker.C:
			if err := runner.Once(ctx, logger); err != nil {
				logger.ErrorContext(ctx, "worker reconciliation failed", "error", err)
			}
		}
	}
}

func (runner *Runner) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"service": "worker", "status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		lastOK := runner.lastOKUnix.Load()
		if lastOK == 0 || time.Since(time.Unix(lastOK, 0)) > 3*runner.interval {
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "No recent successful reconciliation is recorded.")
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"status": "ready"})
	})
	return httpx.SecurityHeaders(mux)
}
