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
	"github.com/traweezy/relantern/internal/database/schema"
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
	if err := schema.ValidateReleaseSHA(common.Environment, common.GitSHA); err != nil {
		return fmt.Errorf("validate migration release identity: %w", err)
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
	if command != "up" {
		pingContext, cancelPing := context.WithTimeout(ctx, 3*time.Second)
		defer cancelPing()
		if err := databaseHandle.PingContext(pingContext); err != nil {
			return errors.New("migration database unavailable")
		}
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	const migrationsDirectory = "migrations"
	switch command {
	case "up":
		migrationContext, cancelMigration := context.WithCancel(ctx)
		defer cancelMigration()
		lock, err := acquireMigrationLock(migrationContext, databaseConfig.URL, logger)
		if err != nil {
			return err
		}
		stopWatchdog := startLockWatchdog(
			migrationContext, cancelMigration, lock.Ping, logger, lockWatchInterval,
		)
		defer func() {
			stopWatchdog()
			if err := lock.Release(); err != nil {
				logger.Error("migration lock release failed", "error", err)
			}
		}()
		logger.Info("applying forward migrations")
		if err := goose.UpContext(migrationContext, databaseHandle, migrationsDirectory); err != nil {
			return err
		}
		pool, err := database.Open(migrationContext, databaseConfig)
		if err != nil {
			return err
		}
		defer pool.Close()
		releaseGuard, err := schema.New(pool, common.Environment, common.GitSHA)
		if err != nil {
			return fmt.Errorf("create migration completion guard: %w", err)
		}
		if err := releaseGuard.CheckAppliedSchema(migrationContext); err != nil {
			return fmt.Errorf("validate applied schema before follow-up migrations: %w", err)
		}
		if err := applyRiverMigrations(migrationContext, databaseConfig, logger); err != nil {
			return err
		}
		if err := syncSourceRegistry(migrationContext, databaseConfig, common.Clock.Now()); err != nil {
			return err
		}
		logger.Info("reviewed source registry synchronized")
		if err := releaseGuard.RecordCompletion(migrationContext); err != nil {
			return err
		}
		logger.Info("migration completion recorded", "release_sha", common.GitSHA)
		return nil
	case "status":
		if err := goose.StatusContext(ctx, databaseHandle, migrationsDirectory); err != nil {
			return err
		}
		if err := validateRiverMigrations(ctx, databaseConfig, logger); err != nil {
			return err
		}
		pool, err := database.Open(ctx, databaseConfig)
		if err != nil {
			return err
		}
		defer pool.Close()
		releaseGuard, err := schema.New(pool, common.Environment, common.GitSHA)
		if err != nil {
			return err
		}
		return releaseGuard.Check(ctx)
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
