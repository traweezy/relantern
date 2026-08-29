package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/scheduler"
	"github.com/traweezy/relantern/internal/service"
	"github.com/traweezy/relantern/internal/storage/s3store"
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
	objectStorageConfig, err := config.LoadObjectStorage(common.Environment)
	if err != nil {
		return fmt.Errorf("load object-storage configuration: %w", err)
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
	rawStore, err := s3store.New(s3store.Config{
		Endpoint:  objectStorageConfig.Endpoint,
		Bucket:    objectStorageConfig.Bucket,
		Region:    objectStorageConfig.Region,
		AccessKey: objectStorageConfig.AccessKey,
		SecretKey: objectStorageConfig.SecretKey,
	})
	if err != nil {
		return fmt.Errorf("create raw object store: %w", err)
	}
	storageContext, cancelStorageCheck := context.WithTimeout(rootContext, 5*time.Second)
	defer cancelStorageCheck()
	if err := rawStore.Check(storageContext); err != nil {
		return fmt.Errorf("check raw object store: %w", err)
	}

	reconciler := scheduler.NewReconciler(pool, common.Clock, "relantern:schedule-reconciler:v1")
	processor := worker.NewProcessor(pool, workerConfig.DeliveryURL, workerConfig.RequestTimeout)
	runner := worker.NewRunner(reconciler, processor, workerConfig.ReconcileInterval)
	if len(arguments) > 0 && arguments[0] == "once" {
		return runner.Once(rootContext, logger)
	}
	return runner.Run(rootContext, logger, httpConfig.Port, httpConfig.ShutdownTimeout)
}
