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
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/sources/pgstore"
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
		if err := goose.UpContext(ctx, databaseHandle, migrationsDirectory); err != nil {
			return err
		}
		if err := applyRiverMigrations(ctx, databaseConfig, logger); err != nil {
			return err
		}
		if err := syncSourceRegistry(ctx, databaseConfig, common.Clock.Now()); err != nil {
			return err
		}
		logger.Info("reviewed source registry synchronized")
		return nil
	case "status":
		if err := goose.StatusContext(ctx, databaseHandle, migrationsDirectory); err != nil {
			return err
		}
		return validateRiverMigrations(ctx, databaseConfig, logger)
	case "down":
		logger.Warn("rolling back one local migration")
		return goose.DownContext(ctx, databaseHandle, migrationsDirectory)
	default:
		return fmt.Errorf("unsupported migration command %q", command)
	}
}

func applyRiverMigrations(
	ctx context.Context,
	databaseConfig config.Database,
	logger *slog.Logger,
) error {
	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(
		riverpgxv5.New(pool),
		&rivermigrate.Config{Logger: logger, Schema: jobqueue.Schema},
	)
	if err != nil {
		return fmt.Errorf("create River migrator: %w", err)
	}
	result, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("apply River migrations: %w", err)
	}
	logger.Info("River migrations applied", "migration_count", len(result.Versions))
	return nil
}

func validateRiverMigrations(
	ctx context.Context,
	databaseConfig config.Database,
	logger *slog.Logger,
) error {
	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(
		riverpgxv5.New(pool),
		&rivermigrate.Config{Logger: logger, Schema: jobqueue.Schema},
	)
	if err != nil {
		return fmt.Errorf("create River migrator: %w", err)
	}
	result, err := migrator.Validate(ctx, nil)
	if err != nil {
		return fmt.Errorf("validate River migrations: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("River migrations are incomplete: %v", result.Messages)
	}
	logger.Info("River migrations are current")
	return nil
}

func syncSourceRegistry(ctx context.Context, databaseConfig config.Database, now time.Time) error {
	sourceConfig := config.LoadSources()
	registry, err := sources.LoadRegistry(sourceConfig.RegistryPath)
	if err != nil {
		return err
	}
	fixtures, err := sources.LoadFixtureCatalog(sourceConfig.FixturesPath)
	if err != nil {
		return err
	}
	if err := sources.Validate(registry, fixtures, now); err != nil {
		return fmt.Errorf("validate source registry: %w", err)
	}

	pool, err := database.Open(ctx, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin source registry sync: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if err := (pgstore.Store{}).Sync(ctx, transaction, registry); err != nil {
		return fmt.Errorf("sync source registry: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit source registry sync: %w", err)
	}
	return nil
}
