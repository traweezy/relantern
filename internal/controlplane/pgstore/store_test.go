package pgstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetrySerializableValueRetriesConcurrencyFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		code string
	}{
		{name: "serialization failure", code: "40001"},
		{name: "deadlock", code: "40P01"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			attempts := 0
			value, err := retrySerializableValue(context.Background(), func() (string, error) {
				attempts++
				if attempts < 3 {
					return "", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: test.code})
				}
				return "committed", nil
			})
			if err != nil || value != "committed" || attempts != 3 {
				t.Fatalf("retrySerializableValue() = %q, %v after %d attempts", value, err, attempts)
			}
		})
	}
}

func TestRetrySerializableValueStopsAtBound(t *testing.T) {
	t.Parallel()
	attempts := 0
	want := &pgconn.PgError{Code: "40001"}
	_, err := retrySerializableValue(context.Background(), func() (struct{}, error) {
		attempts++
		return struct{}{}, want
	})
	if !errors.Is(err, want) || attempts != serializableAttempts {
		t.Fatalf("retrySerializableValue() = %v after %d attempts", err, attempts)
	}
}

func TestRetrySerializableValueDoesNotRetryDomainFailure(t *testing.T) {
	t.Parallel()
	attempts := 0
	want := errors.New("invalid request")
	_, err := retrySerializableValue(context.Background(), func() (struct{}, error) {
		attempts++
		return struct{}{}, want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("retrySerializableValue() = %v after %d attempts", err, attempts)
	}
}

func TestRetrySerializableValueHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := retrySerializableValue(ctx, func() (struct{}, error) {
		attempts++
		cancel()
		return struct{}{}, &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("retrySerializableValue() = %v after %d attempts", err, attempts)
	}
}
