package schema

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/config"
)

func TestReleaseSHAValidation(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, environment := range []config.Environment{
		config.EnvironmentLocal, config.EnvironmentTest,
		config.EnvironmentStaging, config.EnvironmentProduction,
	} {
		if err := ValidateReleaseSHA(environment, sha); err != nil {
			t.Fatalf("%s rejected full SHA: %v", environment, err)
		}
	}
	if err := ValidateReleaseSHA(config.EnvironmentLocal, "unknown"); err != nil {
		t.Fatalf("local unknown SHA rejected: %v", err)
	}
	for _, sha := range []string{"unknown", strings.Repeat("A", 40), "short", ""} {
		if err := ValidateReleaseSHA(config.EnvironmentStaging, sha); err == nil {
			t.Fatalf("staging accepted invalid SHA %q", sha)
		}
	}
}

func TestCheckAppliedVersionsRequiresEveryReviewedMigration(t *testing.T) {
	required := []int64{1, 2, 3}
	if err := checkAppliedVersions(required, map[int64]bool{1: true, 2: true, 3: true, 4: true}); err != nil {
		t.Fatalf("complete history rejected: %v", err)
	}
	for _, applied := range []map[int64]bool{
		{1: true, 3: true},
		{1: true, 2: false, 3: true},
	} {
		if err := checkAppliedVersions(required, applied); !errors.Is(err, ErrPending) {
			t.Fatalf("incomplete history error = %v, want ErrPending", err)
		}
	}
}

func TestSourceEntryFootprintRejectsRecordedVersionWithMissingObjects(t *testing.T) {
	if err := validateSourceEntryFootprint(5, 6); err != nil {
		t.Fatalf("complete migration 22 footprint rejected: %v", err)
	}
	if err := validateSourceEntryFootprint(0, 2); !errors.Is(err, ErrDrift) {
		t.Fatalf("drift error = %v, want ErrDrift", err)
	}
}

func TestWaitRetriesPendingMigrationAndStopsOnDrift(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	attempts := 0
	err := wait(context.Background(), logger, 35, 2*time.Second, func(context.Context) error {
		attempts++
		if attempts == 1 {
			return ErrPending
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("wait() = %v after %d attempts, want success after 2", err, attempts)
	}
	attempts = 0
	err = wait(context.Background(), logger, 35, time.Second, func(context.Context) error {
		attempts++
		return ErrDrift
	})
	if !errors.Is(err, ErrDrift) || attempts != 1 {
		t.Fatalf("drift wait() = %v after %d attempts", err, attempts)
	}
}

func TestWaitHasBoundedDeadline(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := wait(context.Background(), logger, 35, 20*time.Millisecond, func(context.Context) error {
		return ErrUnavailable
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want deadline exceeded", err)
	}
}
