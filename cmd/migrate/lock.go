package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	migrationLockWait = 8 * time.Minute
	lockRetryDelay    = 2 * time.Second
	lockWatchInterval = 2 * time.Second
	lockQueryTimeout  = 3 * time.Second
	lockReleaseWait   = 5 * time.Second
	migrationLockA    = int32(0x52454c41) // RELA
	migrationLockB    = int32(0x4d494752) // MIGR
)

type migrationLock struct {
	connection   *pgx.Conn
	once         sync.Once
	releaseError error
}

// A session-level advisory lock spans Goose, River, source registry sync, and
// marker recording. Closing the dedicated connection releases it on all exits.
func acquireMigrationLock(ctx context.Context, databaseURL string, logger *slog.Logger) (*migrationLock, error) {
	if logger == nil {
		return nil, errors.New("migration lock logger is required")
	}
	waitContext, cancelWait := context.WithTimeout(ctx, migrationLockWait)
	defer cancelWait()
	var connection *pgx.Conn
	for attempt := 1; ; attempt++ {
		reason := "held by another migration job"
		if connection == nil {
			connectContext, cancelConnect := context.WithTimeout(waitContext, lockQueryTimeout)
			var err error
			connection, err = pgx.Connect(connectContext, databaseURL)
			cancelConnect()
			if err != nil {
				reason = "database unavailable"
			}
		}
		if connection != nil {
			queryContext, cancelQuery := context.WithTimeout(waitContext, lockQueryTimeout)
			var acquired bool
			err := connection.QueryRow(queryContext, `
				select pg_try_advisory_lock($1::integer, $2::integer)`,
				migrationLockA, migrationLockB).Scan(&acquired)
			cancelQuery()
			if err != nil {
				closeMigrationLockConnection(connection)
				connection = nil
				reason = "database unavailable"
			} else if acquired {
				logger.InfoContext(ctx, "migration lock acquired", "attempts", attempt)
				return &migrationLock{connection: connection}, nil
			}
		}
		if waitContext.Err() != nil {
			if connection != nil {
				closeMigrationLockConnection(connection)
			}
			return nil, errors.Join(errors.New("migration lock wait ended"), waitContext.Err())
		}
		if attempt == 1 || attempt%15 == 0 {
			logger.WarnContext(waitContext, "waiting for migration lock", "attempt", attempt, "reason", reason)
		}
		timer := time.NewTimer(lockRetryDelay)
		select {
		case <-timer.C:
		case <-waitContext.Done():
			timer.Stop()
			if connection != nil {
				closeMigrationLockConnection(connection)
			}
			return nil, errors.Join(errors.New("migration lock wait ended"), waitContext.Err())
		}
	}
}

func (lock *migrationLock) Release() error {
	lock.once.Do(func() {
		releaseContext, cancelRelease := context.WithTimeout(context.Background(), lockReleaseWait)
		defer cancelRelease()
		var released bool
		if err := lock.connection.QueryRow(releaseContext, `
			select pg_advisory_unlock($1::integer, $2::integer)`,
			migrationLockA, migrationLockB).Scan(&released); err != nil || !released {
			lock.releaseError = errors.New("migration advisory lock did not release cleanly")
		}
		if err := lock.connection.Close(releaseContext); err != nil {
			lock.releaseError = errors.New("close migration lock connection: database unavailable")
		}
	})
	return lock.releaseError
}

func (lock *migrationLock) Ping(ctx context.Context) error {
	if err := lock.connection.Ping(ctx); err != nil {
		return errors.New("migration lock session was lost")
	}
	return nil
}

// The connection holding a session lock is otherwise idle while the migrate
// steps use separate pools. A lost session cancels those steps promptly.
func startLockWatchdog(
	ctx context.Context,
	cancel context.CancelFunc,
	ping func(context.Context) error,
	logger *slog.Logger,
	interval time.Duration,
) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingContext, cancelPing := context.WithTimeout(ctx, lockQueryTimeout)
				err := ping(pingContext)
				cancelPing()
				if err != nil {
					if ctx.Err() == nil {
						logger.ErrorContext(ctx, "migration lock session lost")
						cancel()
					}
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

func closeMigrationLockConnection(connection *pgx.Conn) {
	closeContext, cancel := context.WithTimeout(context.Background(), lockReleaseWait)
	defer cancel()
	_ = connection.Close(closeContext)
}
