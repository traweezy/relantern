package schema

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/migrations"
)

const (
	startupWait       = 8 * time.Minute
	checkTimeout      = 2 * time.Second
	initialRetryDelay = 500 * time.Millisecond
	maximumRetryDelay = 5 * time.Second
)

var (
	ErrDrift       = errors.New("database schema differs from an applied migration")
	ErrUnavailable = errors.New("database schema check unavailable")
	ErrPending     = errors.New("database migrations pending")
	fullSHA        = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// Guard compares the committed SQL migration set with Goose's latest recorded
// state, checks the migration 22 catalog footprint, and requires the matching
// post-success release completion marker.
type Guard struct {
	pool       *pgxpool.Pool
	required   []int64
	releaseSHA string
}

func New(pool *pgxpool.Pool, environment config.Environment, releaseSHA string) (*Guard, error) {
	if pool == nil {
		return nil, errors.New("database pool is required")
	}
	if err := ValidateReleaseSHA(environment, releaseSHA); err != nil {
		return nil, err
	}
	versions, err := migrations.RequiredVersions()
	if err != nil {
		return nil, fmt.Errorf("load reviewed migrations: %w", err)
	}
	return &Guard{pool: pool, required: versions, releaseSHA: releaseSHA}, nil
}

func ValidateReleaseSHA(environment config.Environment, releaseSHA string) error {
	switch environment {
	case config.EnvironmentLocal, config.EnvironmentTest:
		if releaseSHA == "unknown" || fullSHA.MatchString(releaseSHA) {
			return nil
		}
	case config.EnvironmentStaging, config.EnvironmentProduction:
		if fullSHA.MatchString(releaseSHA) {
			return nil
		}
	}
	return errors.New("GIT_SHA must be a full lowercase release SHA (or unknown in local/test)")
}

// Wait allows the one-shot migrator to finish before a new API or worker starts.
// Catalog drift is terminal because waiting cannot repair an already applied
// migration; incomplete Goose history gets a bounded retry window.
func (guard *Guard) Wait(ctx context.Context, logger *slog.Logger) error {
	if logger == nil {
		return errors.New("schema guard logger is required")
	}
	return wait(ctx, logger, guard.required[len(guard.required)-1], startupWait, guard.Check)
}

func (guard *Guard) Check(ctx context.Context) error {
	checkContext, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := guard.checkGooseAndCatalog(checkContext); err != nil {
		return err
	}
	return guard.checkCompletion(checkContext)
}

// CheckAppliedSchema rejects known catalog drift before the migrator changes
// River tables or synchronizes the source registry.
func (guard *Guard) CheckAppliedSchema(ctx context.Context) error {
	checkContext, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return guard.checkGooseAndCatalog(checkContext)
}

func (guard *Guard) checkGooseAndCatalog(ctx context.Context) error {
	rows, err := guard.pool.Query(ctx, `
		select version_id, is_applied
		from public.goose_db_version
		order by id`)
	if err != nil {
		return fmt.Errorf("read Goose migration history: %w", ErrUnavailable)
	}
	applied := make(map[int64]bool, len(guard.required))
	for rows.Next() {
		var version int64
		var isApplied bool
		if err := rows.Scan(&version, &isApplied); err != nil {
			rows.Close()
			return fmt.Errorf("decode Goose migration history: %w", ErrDrift)
		}
		applied[version] = isApplied
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read Goose migration history: %w", ErrUnavailable)
	}
	if err := checkAppliedVersions(guard.required, applied); err != nil {
		return err
	}
	if slices.Contains(guard.required, 22) {
		return guard.checkSourceEntrySchema(ctx)
	}
	return nil
}

func (guard *Guard) checkCompletion(ctx context.Context) error {
	var exists bool
	if err := guard.pool.QueryRow(ctx, `
		select to_regclass('app.migration_completions') is not null`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect migration completion table: %w", ErrUnavailable)
	}
	if !exists {
		return fmt.Errorf("%w: migration completion table is missing", ErrDrift)
	}
	var completed bool
	if err := guard.pool.QueryRow(ctx, `
		select exists (
			select 1 from app.migration_completions
			where release_sha = $1 and goose_version = $2
		)`, guard.releaseSHA, guard.required[len(guard.required)-1]).Scan(&completed); err != nil {
		return fmt.Errorf("inspect migration completion marker: %w", ErrUnavailable)
	}
	if !completed {
		return fmt.Errorf("%w: migration completion marker is absent for this release", ErrPending)
	}
	return nil
}

// RecordCompletion is called by the one-shot migrator only after River and
// source-registry work have succeeded. The committed marker releases readers.
func (guard *Guard) RecordCompletion(ctx context.Context) error {
	checkContext, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := guard.checkGooseAndCatalog(checkContext); err != nil {
		return err
	}
	if _, err := guard.pool.Exec(checkContext, `
		insert into app.migration_completions (release_sha, goose_version)
		values ($1, $2)
		on conflict (release_sha, goose_version) do update
		set completed_at = excluded.completed_at`,
		guard.releaseSHA, guard.required[len(guard.required)-1]); err != nil {
		return fmt.Errorf("record migration completion: %w", ErrUnavailable)
	}
	return nil
}

func checkAppliedVersions(required []int64, applied map[int64]bool) error {
	for _, version := range required {
		if !applied[version] {
			return fmt.Errorf("%w: Goose migration %d is not applied", ErrPending, version)
		}
	}
	return nil
}

func (guard *Guard) checkSourceEntrySchema(ctx context.Context) error {
	columns := []string{
		"source_registry_id", "source_connector", "source_content_type",
		"ingestion_error_code", "ingestion_failed_at",
	}
	var presentColumns int
	if err := guard.pool.QueryRow(ctx, `
		select count(*)
		from pg_catalog.pg_attribute
		where attrelid = to_regclass('app.raw_documents')
			and attname = any($1::text[])
			and not attisdropped`, columns).Scan(&presentColumns); err != nil {
		return fmt.Errorf("inspect migration 22 columns: %w", ErrUnavailable)
	}
	var validIndexes int
	if err := guard.pool.QueryRow(ctx, `
		select count(*)
		from pg_catalog.pg_index index_state
		join pg_catalog.pg_class index_name on index_name.oid = index_state.indexrelid
		join pg_catalog.pg_class table_name on table_name.oid = index_state.indrelid
		join pg_catalog.pg_namespace namespace on namespace.oid = table_name.relnamespace
		where namespace.nspname = 'app'
			and index_state.indisvalid and index_state.indisready
			and (
				(table_name.relname = 'source_fetches'
					and index_name.relname = 'idx_source_fetches_object_key_partial')
				or (table_name.relname = 'raw_documents' and index_name.relname = any($1::text[]))
			)`, []string{
		"idx_raw_documents_parent_source_url_sha256",
		"idx_raw_documents_entry_sha256",
		"idx_raw_documents_source_entry_first_seen_at",
		"idx_raw_documents_parent_raw_document_id_partial",
		"idx_raw_documents_ingestion_failed_partial",
	}).Scan(&validIndexes); err != nil {
		return fmt.Errorf("inspect migration 22 indexes: %w", ErrUnavailable)
	}
	return validateSourceEntryFootprint(presentColumns, validIndexes)
}

func validateSourceEntryFootprint(presentColumns, validIndexes int) error {
	if presentColumns != 5 || validIndexes != 6 {
		return fmt.Errorf("%w: migration 22 has %d/5 expected columns and %d/6 valid indexes",
			ErrDrift, presentColumns, validIndexes)
	}
	return nil
}

func wait(
	ctx context.Context,
	logger *slog.Logger,
	requiredVersion int64,
	limit time.Duration,
	check func(context.Context) error,
) error {
	start := time.Now()
	waitContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	delay := initialRetryDelay
	for attempt := 1; ; attempt++ {
		err := check(waitContext)
		if err == nil {
			logger.InfoContext(waitContext, "database schema ready",
				"required_version", requiredVersion, "attempts", attempt,
				"wait_duration_ms", time.Since(start).Milliseconds())
			return nil
		}
		if errors.Is(err, ErrDrift) {
			return err
		}
		if waitContext.Err() != nil {
			return fmt.Errorf("database migration wait ended after %d attempts: %w: last check: %v",
				attempt, waitContext.Err(), err)
		}
		if attempt == 1 || attempt%5 == 0 {
			logger.WarnContext(waitContext, "waiting for database migrations",
				"required_version", requiredVersion, "attempt", attempt, "error", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-waitContext.Done():
			timer.Stop()
			return fmt.Errorf("database migration wait ended after %d attempts: %w: last check: %v",
				attempt, waitContext.Err(), err)
		}
		delay = min(delay*2, maximumRetryDelay)
	}
}
