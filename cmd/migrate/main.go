package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/traweezy/relantern/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("migration command failed", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}

	command := "up"
	if len(arguments) > 0 {
		command = arguments[0]
	}
	if command == "down" && common.Environment != config.EnvironmentLocal && common.Environment != config.EnvironmentTest {
		return errors.New("down migrations are forbidden outside local and test environments")
	}

	databaseHandle, err := sql.Open("pgx", databaseConfig.URL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer databaseHandle.Close()
	databaseHandle.SetMaxOpenConns(1)
	databaseHandle.SetMaxIdleConns(1)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := databaseHandle.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	const migrationsDirectory = "migrations"
	switch command {
	case "up":
		logger.Info("applying forward migrations")
		return goose.UpContext(ctx, databaseHandle, migrationsDirectory)
	case "status":
		return goose.StatusContext(ctx, databaseHandle, migrationsDirectory)
	case "down":
		logger.Warn("rolling back one local migration")
		return goose.DownContext(ctx, databaseHandle, migrationsDirectory)
	default:
		return fmt.Errorf("unsupported migration command %q", command)
	}
}
