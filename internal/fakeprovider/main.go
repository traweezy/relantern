package fakeprovider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/service"
)

func Run(arguments []string, logger *slog.Logger, kind Kind, defaultPort uint16) error {
	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		return Healthcheck(defaultPort)
	}
	if _, err := config.LoadCommon(); err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTP(defaultPort)
	if err != nil {
		return fmt.Errorf("load HTTP configuration: %w", err)
	}
	handler, err := New(kind, logger)
	if err != nil {
		return err
	}
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return service.RunHTTP(rootContext, logger, service.HTTPServerConfig{
		Host:            "0.0.0.0",
		Port:            httpConfig.Port,
		Handler:         handler,
		ShutdownTimeout: httpConfig.ShutdownTimeout,
	})
}

func Healthcheck(defaultPort uint16) error {
	httpConfig, err := config.LoadHTTP(defaultPort)
	if err != nil {
		return err
	}
	return service.CheckLocalHealth(httpConfig.Port, "/healthz")
}

func Main(kind Kind, defaultPort uint16) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := Run(os.Args[1:], logger, kind, defaultPort); err != nil {
		logger.Error("fake provider stopped", "provider", kind, "error", err)
		os.Exit(1)
	}
}
