package fetcher

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostLimiterBoundsGlobalConcurrency(t *testing.T) {
	limiter, err := NewHostLimiter(1, 1, 0)
	if err != nil {
		t.Fatalf("NewHostLimiter() error = %v", err)
	}
	release, err := limiter.Acquire(context.Background(), "source.example")
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := limiter.Acquire(ctx, "other.example"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Acquire() error = %v, want deadline", err)
	}
	release()
	secondRelease, err := limiter.Acquire(context.Background(), "other.example")
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	secondRelease()
}

func TestHostLimiterTokenBucketHonorsCancellation(t *testing.T) {
	limiter, err := NewHostLimiter(2, 1, time.Second)
	if err != nil {
		t.Fatalf("NewHostLimiter() error = %v", err)
	}
	release, err := limiter.Acquire(context.Background(), "source.example")
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := limiter.Acquire(ctx, "source.example"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rate-limited Acquire() error = %v, want deadline", err)
	}
}

func TestHostLimiterRejectsInvalidBounds(t *testing.T) {
	tests := []struct {
		global  int
		perHost int
		gap     time.Duration
	}{
		{global: 0, perHost: 1},
		{global: 1, perHost: 2},
		{global: 1, perHost: 1, gap: -time.Second},
	}
	for _, test := range tests {
		if _, err := NewHostLimiter(test.global, test.perHost, test.gap); err == nil {
			t.Fatalf("NewHostLimiter(%d, %d, %s) succeeded", test.global, test.perHost, test.gap)
		}
	}
}
