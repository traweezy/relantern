package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/config"
)

func TestMigrationLockSerializesRuns(t *testing.T) {
	if os.Getenv("APP_ENV") != "test" ||
		(os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "") {
		t.Skip("local test PostgreSQL is required")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(settings.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains([]string{"postgres", "localhost", "127.0.0.1"}, address.Hostname()) {
		t.Skip("advisory lock test is only allowed on local PostgreSQL")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, err := acquireMigrationLock(context.Background(), settings.URL, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })
	waitContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if second, err := acquireMigrationLock(waitContext, settings.URL, logger); second != nil ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlapping lock returned release=%t and error=%v, want deadline",
			second != nil, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireMigrationLock(context.Background(), settings.URL, logger)
	if err != nil {
		t.Fatalf("lock did not become available after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockWatchdogCancelsMigrationWhenSessionIsLost(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := startLockWatchdog(ctx, cancel, func(context.Context) error {
		return errors.New("simulated connection loss")
	}, logger, time.Millisecond)
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("migration context was not canceled after lock-session loss")
	}
}

func TestMigrationLockRetriesUnavailableDatabaseUntilDeadline(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	lock, err := acquireMigrationLock(ctx, "postgres://relantern@127.0.0.1:1/relantern?sslmode=disable", logger)
	if lock != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unavailable database returned lock=%t and error=%v, want deadline",
			lock != nil, err)
	}
}
