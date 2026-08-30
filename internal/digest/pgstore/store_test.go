package pgstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetrySerializableRetriesConcurrencyFailures(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{name: "serialization failure", code: "40001"},
		{name: "deadlock", code: "40P01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			err := retrySerializable(context.Background(), func() error {
				attempts++
				if attempts < 3 {
					return fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: test.code})
				}
				return nil
			})
			if err != nil {
				t.Fatalf("retrySerializable() error = %v", err)
			}
			if attempts != 3 {
				t.Fatalf("attempts = %d, want 3", attempts)
			}
		})
	}
}

func TestRetrySerializableStopsAtBound(t *testing.T) {
	attempts := 0
	want := &pgconn.PgError{Code: "40001"}
	err := retrySerializable(context.Background(), func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retrySerializable() error = %v, want final PostgreSQL error", err)
	}
	if attempts != serializableAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, serializableAttempts)
	}
}

func TestRetrySerializableDoesNotRetryOtherFailures(t *testing.T) {
	attempts := 0
	want := errors.New("invalid occurrence")
	err := retrySerializable(context.Background(), func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("retrySerializable() = %v after %d attempts", err, attempts)
	}
}

func TestRetrySerializableHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retrySerializable(ctx, func() error {
		attempts++
		cancel()
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("retrySerializable() = %v after %d attempts", err, attempts)
	}
}
