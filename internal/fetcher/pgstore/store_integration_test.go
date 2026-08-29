package pgstore_test

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/fetcher/pgstore"
)

func TestRecordPersistsFetchCheckpointAndRawMetadataAtomically(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for the fetch integration test")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	attemptedAt := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	payloadDigest := sha256.Sum256([]byte("fixture"))
	result := fetcher.Result{
		Outcome: fetcher.OutcomeStored,
		Attempts: []fetcher.Attempt{
			{
				AttemptedAt:     attemptedAt,
				CompletedAt:     attemptedAt.Add(250 * time.Millisecond),
				StatusCode:      200,
				FinalURL:        "https://go.dev/blog/feed.atom",
				ContentType:     "application/atom+xml",
				CompressedBytes: 7,
				Bytes:           7,
				Duration:        250 * time.Millisecond,
				ETag:            `"fixture-v1"`,
			},
		},
		Checkpoint: fetcher.Checkpoint{
			ETag:          `"fixture-v1"`,
			ProviderState: map[string]any{"page": 1},
		},
		ObjectKey: "raw/go-blog/2026/08/29/fixture.xml",
		SHA256:    payloadDigest,
		Bytes:     7,
	}
	if err := (pgstore.Store{}).Record(ctx, transaction, "go-blog", "go-blog", fetcher.ContentPolicyLinkAndExcerpt, result); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var fetchCount int
	var checkpointETag string
	var documentCount int
	var healthState string
	if err := transaction.QueryRow(ctx, `
		select count(*)
		from app.source_fetches source_fetch
		join app.source_endpoints endpoint on endpoint.id = source_fetch.endpoint_id
		where endpoint.registry_id = 'go-blog' and source_fetch.attempted_at = $1`, attemptedAt).Scan(&fetchCount); err != nil {
		t.Fatalf("count source fetches: %v", err)
	}
	if err := transaction.QueryRow(ctx, `
		select checkpoint.etag
		from app.source_checkpoints checkpoint
		join app.source_endpoints endpoint on endpoint.id = checkpoint.endpoint_id
		where endpoint.registry_id = 'go-blog'`).Scan(&checkpointETag); err != nil {
		t.Fatalf("select checkpoint: %v", err)
	}
	if err := transaction.QueryRow(ctx, `
		select count(*) from app.raw_documents
		where source_id = 'go-blog' and raw_sha256 = $1`, payloadDigest[:]).Scan(&documentCount); err != nil {
		t.Fatalf("count raw documents: %v", err)
	}
	if err := transaction.QueryRow(ctx, `
		select health_state from app.source_endpoints where registry_id = 'go-blog'`).Scan(&healthState); err != nil {
		t.Fatalf("select endpoint health: %v", err)
	}
	if fetchCount != 1 || checkpointETag != `"fixture-v1"` || documentCount != 1 || healthState != "paused" {
		t.Fatalf("fetchCount=%d checkpoint=%q documents=%d health=%q", fetchCount, checkpointETag, documentCount, healthState)
	}
}

func TestRecordHandlesMetadataNotModifiedAndFailedAttempts(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for the fetch integration test")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()
	baseTime := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("metadata fixture"))
	tests := []struct {
		name             string
		registryID       string
		contentPolicy    string
		result           fetcher.Result
		wantCheckpoints  int
		wantRawDocuments int
	}{
		{
			name:          "metadata only",
			registryID:    "railway-status",
			contentPolicy: fetcher.ContentPolicyMetadataOnly,
			result: fetcher.Result{
				Outcome: fetcher.OutcomeMetadataOnly,
				Attempts: []fetcher.Attempt{{
					AttemptedAt: baseTime, CompletedAt: baseTime.Add(time.Second), StatusCode: 200,
					FinalURL: "https://api.railwaystatus.com/status", ContentType: "application/json", Bytes: 16, Duration: time.Second,
				}},
				Checkpoint: fetcher.Checkpoint{ETag: `"metadata-v1"`},
				SHA256:     digest,
				Bytes:      16,
			},
			wantCheckpoints:  1,
			wantRawDocuments: 1,
		},
		{
			name:          "not modified",
			registryID:    "go-release-history",
			contentPolicy: fetcher.ContentPolicyLinkAndExcerpt,
			result: fetcher.Result{
				Outcome: fetcher.OutcomeNotModified,
				Attempts: []fetcher.Attempt{{
					AttemptedAt: baseTime.Add(time.Minute), CompletedAt: baseTime.Add(time.Minute + time.Second), StatusCode: 304,
					FinalURL: "https://go.dev/doc/devel/release", Duration: time.Second,
				}},
				Checkpoint: fetcher.Checkpoint{ETag: `"release-v1"`},
			},
			wantCheckpoints: 1,
		},
		{
			name:          "failed after retry",
			registryID:    "go-security",
			contentPolicy: fetcher.ContentPolicyLinkAndExcerpt,
			result: fetcher.Result{
				Outcome: fetcher.OutcomeFailed,
				Attempts: []fetcher.Attempt{
					{
						AttemptedAt: baseTime.Add(2 * time.Minute), CompletedAt: baseTime.Add(2*time.Minute + time.Second), StatusCode: 500,
						FinalURL: "https://go.dev/doc/security/", Duration: time.Second, ErrorCode: fetcher.ErrorUnexpectedStatus,
					},
					{
						AttemptedAt: baseTime.Add(3 * time.Minute), CompletedAt: baseTime.Add(3*time.Minute + time.Second),
						Duration: time.Second, ErrorCode: fetcher.ErrorTransport,
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatalf("BeginTx() error = %v", err)
			}
			defer func() { _ = transaction.Rollback(ctx) }()
			if err := (pgstore.Store{}).Record(ctx, transaction, test.registryID, test.registryID, test.contentPolicy, test.result); err != nil {
				t.Fatalf("Record() error = %v", err)
			}
			var attempts int
			var checkpoints int
			var documents int
			if err := transaction.QueryRow(ctx, `
				select count(*)
				from app.source_fetches source_fetch
				join app.source_endpoints endpoint on endpoint.id = source_fetch.endpoint_id
				where endpoint.registry_id = $1 and source_fetch.attempted_at >= $2`, test.registryID, baseTime).Scan(&attempts); err != nil {
				t.Fatalf("count attempts: %v", err)
			}
			if err := transaction.QueryRow(ctx, `
				select count(*)
				from app.source_checkpoints checkpoint
				join app.source_endpoints endpoint on endpoint.id = checkpoint.endpoint_id
				where endpoint.registry_id = $1`, test.registryID).Scan(&checkpoints); err != nil {
				t.Fatalf("count checkpoints: %v", err)
			}
			if err := transaction.QueryRow(ctx, `select count(*) from app.raw_documents where source_id = $1`, test.registryID).Scan(&documents); err != nil {
				t.Fatalf("count raw documents: %v", err)
			}
			if attempts != len(test.result.Attempts) || checkpoints != test.wantCheckpoints || documents != test.wantRawDocuments {
				t.Fatalf("attempts=%d checkpoints=%d documents=%d", attempts, checkpoints, documents)
			}
		})
	}
}

func TestRecordRejectsInvalidAtomicState(t *testing.T) {
	if err := (pgstore.Store{}).Record(context.Background(), nil, "go-blog", "go-blog", fetcher.ContentPolicyLinkAndExcerpt, fetcher.Result{}); err == nil {
		t.Fatal("Record() accepted a nil transaction")
	}
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for the fetch integration test")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()
	ctx := context.Background()
	attemptedAt := time.Date(2026, time.August, 29, 14, 0, 0, 0, time.UTC)
	baseResult := fetcher.Result{
		Outcome: fetcher.OutcomeStored,
		Attempts: []fetcher.Attempt{{
			AttemptedAt: attemptedAt, CompletedAt: attemptedAt.Add(time.Second), StatusCode: 200,
			FinalURL: "https://go.dev/blog/feed.atom", ContentType: "application/atom+xml", Duration: time.Second,
		}},
		ObjectKey: "raw/go-blog/2026/08/29/invalid-state.xml",
		SHA256:    sha256.Sum256([]byte("fixture")),
	}
	tests := []struct {
		name       string
		registryID string
		result     fetcher.Result
	}{
		{name: "unknown endpoint", registryID: "does-not-exist", result: baseResult},
		{name: "unsupported outcome", registryID: "go-blog", result: func() fetcher.Result {
			candidate := baseResult
			candidate.Outcome = fetcher.Outcome("unsupported")
			return candidate
		}()},
		{name: "missing final URL", registryID: "go-blog", result: func() fetcher.Result {
			candidate := baseResult
			candidate.Attempts = append([]fetcher.Attempt{}, baseResult.Attempts...)
			candidate.Attempts[0].FinalURL = ""
			return candidate
		}()},
		{name: "invalid provider state", registryID: "go-blog", result: func() fetcher.Result {
			candidate := baseResult
			candidate.Checkpoint.ProviderState = map[string]any{"invalid": make(chan struct{})}
			return candidate
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatalf("BeginTx() error = %v", err)
			}
			defer func() { _ = transaction.Rollback(ctx) }()
			if err := (pgstore.Store{}).Record(ctx, transaction, test.registryID, "go-blog", fetcher.ContentPolicyLinkAndExcerpt, test.result); err == nil {
				t.Fatal("Record() accepted invalid state")
			}
		})
	}
}
