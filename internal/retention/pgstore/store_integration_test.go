package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/retention"
	retentionstore "github.com/traweezy/relantern/internal/retention/pgstore"
)

func TestRetentionRunLedgerIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for retention integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := retentionstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "retention:2099-12-31"
	_, _ = pool.Exec(ctx, `delete from app.retention_runs where idempotency_key = $1`, key)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from app.retention_runs where idempotency_key = $1`, key)
	})
	now := time.Date(2099, 12, 31, 12, 0, 0, 0, time.UTC)
	runID, counts, execute, err := store.Start(ctx, key, retention.DefaultPolicy(), now)
	if err != nil || !execute || runID == "" || counts != (retention.Counts{}) {
		t.Fatalf("Start() = %q, %+v, %t, %v", runID, counts, execute, err)
	}
	want := retention.Counts{FetchAttemptsPruned: 2, OutboxEventsPruned: 1}
	if err := store.Complete(ctx, runID, want, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	secondID, secondCounts, secondExecute, err := store.Start(ctx, key, retention.DefaultPolicy(), now.Add(time.Minute))
	if err != nil || secondExecute || secondID != runID || secondCounts != want {
		t.Fatalf("replayed Start() = %q, %+v, %t, %v", secondID, secondCounts, secondExecute, err)
	}
	if raw, err := store.RawCandidates(ctx, time.Unix(0, 0), 10); err != nil || len(raw) != 0 {
		t.Fatalf("RawCandidates() = %+v, %v", raw, err)
	}
	if normalized, err := store.NormalizedCandidates(ctx, time.Unix(0, 0), 10); err != nil || len(normalized) != 0 {
		t.Fatalf("NormalizedCandidates() = %+v, %v", normalized, err)
	}
}

func TestFailedRetentionRunPreservesCountsWhenResumed(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for retention integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := retentionstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "retention:2099-12-30"
	_, _ = pool.Exec(ctx, `delete from app.retention_runs where idempotency_key = $1`, key)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from app.retention_runs where idempotency_key = $1`, key)
	})
	now := time.Date(2099, 12, 30, 12, 0, 0, 0, time.UTC)
	runID, _, execute, err := store.Start(ctx, key, retention.DefaultPolicy(), now)
	if err != nil || !execute {
		t.Fatalf("Start() = %q, %t, %v", runID, execute, err)
	}
	want := retention.Counts{RawObjectsPruned: 2, MutationStatesCompacted: 1}
	if err := store.Fail(ctx, runID, "fixture_failure", want, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	resumedID, resumedCounts, resumed, err := store.Start(ctx, key, retention.DefaultPolicy(), now.Add(time.Minute))
	if err != nil || !resumed || resumedID != runID || resumedCounts != want {
		t.Fatalf("resumed Start() = %q, %+v, %t, %v", resumedID, resumedCounts, resumed, err)
	}
}
