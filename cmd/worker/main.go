package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/scheduler"
	"github.com/traweezy/relantern/internal/service"
	"github.com/traweezy/relantern/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		httpConfig, err := config.LoadHTTP(8081)
		if err != nil {
			return err
		}
		return service.CheckLocalHealth(httpConfig.Port, "/healthz")
	}
	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	workerConfig, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTP(8081)
	if err != nil {
		return fmt.Errorf("load HTTP configuration: %w", err)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(rootContext, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()

	reconciler := scheduler.NewReconciler(pool, common.Clock, "relantern:schedule-reconciler:v1")
	processor := worker.NewProcessor(pool, workerConfig.DeliveryURL, workerConfig.RequestTimeout)
	runner := worker.NewRunner(reconciler, processor, workerConfig.ReconcileInterval)
	if len(arguments) > 0 && arguments[0] == "once" {
		return runner.Once(rootContext, logger)
	}
	return runner.Run(rootContext, logger, httpConfig.Port, httpConfig.ShutdownTimeout)
}
