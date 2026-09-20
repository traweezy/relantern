package pgstore_test

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/fetcher/pgstore"
)

func TestSuccessfulInFlightFetchDoesNotClearParserFailure(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for fetch integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update app.source_endpoints
		set health_state = 'failed', next_poll_at = null where registry_id = 'go-blog'`); err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	result := fetcher.Result{
		Outcome: fetcher.OutcomeNotModified,
		Attempts: []fetcher.Attempt{{
			AttemptedAt: completedAt.Add(-time.Second), CompletedAt: completedAt,
			StatusCode: 304, FinalURL: "https://go.dev/blog/feed.atom",
		}},
	}
	if err := (pgstore.Store{}).Record(ctx, tx, "go-blog", "go-blog", fetcher.ContentPolicyLinkAndExcerpt, result); err != nil {
		t.Fatal(err)
	}
	var health string
	var successAt time.Time
	if err := tx.QueryRow(ctx, `select health_state, last_success_at
		from app.source_endpoints where registry_id = 'go-blog'`).Scan(&health, &successAt); err != nil {
		t.Fatal(err)
	}
	if health != "failed" || !successAt.Equal(completedAt) {
		t.Fatalf("successful fetch erased parser failure: health=%q success=%s", health, successAt)
	}
}

func TestStoredRefetchRestoresPrunedParentObject(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for fetch integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	firstSeenAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	refetchedAt := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	canonicalURL := "https://go.dev/blog/relantern-raw-restore-test"
	digest := sha256.Sum256([]byte("pruned source fixture"))
	if _, err := tx.Exec(ctx, `insert into app.raw_documents (
		source_id, canonical_url, raw_sha256, first_seen_at, first_fetched_at,
		content_policy, raw_pruned_at, source_registry_id, source_connector,
		source_content_type
	) values ('go-blog', $1, $2, $3, $3, 'link-and-excerpt', $4,
		'go-blog', 'atom', 'application/atom+xml')`,
		canonicalURL, digest[:], firstSeenAt, firstSeenAt.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	key := "raw/go-blog/2026/09/19/relantern-raw-restore-test.xml"
	result := fetcher.Result{
		Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: key,
		Attempts: []fetcher.Attempt{{
			AttemptedAt: refetchedAt.Add(-time.Second), CompletedAt: refetchedAt,
			StatusCode: 200, FinalURL: canonicalURL, ContentType: "application/atom+xml",
		}},
	}
	if err := (pgstore.Store{}).Record(ctx, tx, "go-blog", "go-blog", fetcher.ContentPolicyLinkAndExcerpt, result); err != nil {
		t.Fatal(err)
	}
	var restoredKey string
	var prunedAt *time.Time
	var observedAt time.Time
	if err := tx.QueryRow(ctx, `select object_key, raw_pruned_at, first_seen_at
		from app.raw_documents where source_id = 'go-blog' and canonical_url = $1 and raw_sha256 = $2`,
		canonicalURL, digest[:]).Scan(&restoredKey, &prunedAt, &observedAt); err != nil {
		t.Fatal(err)
	}
	if restoredKey != key || prunedAt != nil || !observedAt.Equal(firstSeenAt) {
		t.Fatalf("pruned parent was not restored: key=%q pruned=%v first_seen=%s", restoredKey, prunedAt, observedAt)
	}
	metadataURL := "https://go.dev/blog/relantern-metadata-restore-test"
	metadataDigest := sha256.Sum256([]byte("metadata-only source fixture"))
	if _, err := tx.Exec(ctx, `insert into app.raw_documents (
		source_id, canonical_url, raw_sha256, first_seen_at, first_fetched_at,
		content_policy, source_registry_id, source_connector, source_content_type
	) values ('go-blog', $1, $2, $3, $3, 'metadata-only',
		'go-blog', 'atom', 'application/atom+xml')`,
		metadataURL, metadataDigest[:], firstSeenAt); err != nil {
		t.Fatal(err)
	}
	metadataKey := "raw/go-blog/2026/09/19/relantern-metadata-restore-test.xml"
	metadataResult := fetcher.Result{
		Outcome: fetcher.OutcomeStored, SHA256: metadataDigest, ObjectKey: metadataKey,
		Attempts: []fetcher.Attempt{{
			AttemptedAt: refetchedAt.Add(-time.Second), CompletedAt: refetchedAt,
			StatusCode: 200, FinalURL: metadataURL, ContentType: "application/atom+xml",
		}},
	}
	if err := (pgstore.Store{}).Record(ctx, tx, "go-blog", "go-blog", fetcher.ContentPolicyLinkAndExcerpt, metadataResult); err != nil {
		t.Fatal(err)
	}
	var restoredPolicy string
	if err := tx.QueryRow(ctx, `select object_key, content_policy, raw_pruned_at, first_seen_at
		from app.raw_documents where source_id = 'go-blog' and canonical_url = $1 and raw_sha256 = $2`,
		metadataURL, metadataDigest[:]).Scan(&restoredKey, &restoredPolicy, &prunedAt, &observedAt); err != nil {
		t.Fatal(err)
	}
	if restoredKey != metadataKey || restoredPolicy != "link-and-excerpt" || prunedAt != nil || !observedAt.Equal(firstSeenAt) {
		t.Fatalf("metadata parent was not restored: key=%q policy=%q pruned=%v first_seen=%s",
			restoredKey, restoredPolicy, prunedAt, observedAt)
	}
}

func TestLaterFetchCannotClaimAmbiguousLegacyRawProvenance(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for fetch integration")
	}
	configuration, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sourceID := "test-legacy-fetch-" + uuid.NewString()
	registryA := sourceID + "-a"
	registryB := sourceID + "-b"
	firstSeen := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	firstFetched := firstSeen.Add(time.Second)
	later := firstSeen.Add(24 * time.Hour)
	if _, err := tx.Exec(ctx, `insert into app.sources (
		id, name, trust_tier, owner, origin, validation_state, homepage_url,
		content_policy, enabled, topics, reviewed_at
	) values ($1, 'Legacy fetch fixture', 'T0', 'system', 'owner', 'active',
		'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, firstSeen); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ registryID, connector, url, contentType string }{
		{registryA, "rss", "https://example.com/feed-a.xml", "application/rss+xml"},
		{registryB, "atom", "https://example.com/feed-b.xml", "application/atom+xml"},
	} {
		if _, err := tx.Exec(ctx, `insert into app.source_endpoints (
			registry_id, source_id, connector, url, poll_interval, priority,
			robots_policy, expected_content_types, max_response_bytes, fixture_suite
		) values ($1, $2, $3, $4, interval '5 minutes', 'normal',
			'feed', array[$5], 1048576, 'rss-v1')`,
			endpoint.registryID, sourceID, endpoint.connector, endpoint.url, endpoint.contentType); err != nil {
			t.Fatal(err)
		}
	}
	insertOriginalFetch := func(registryID, url, key, contentType string, digest [sha256.Size]byte) {
		t.Helper()
		if _, err := tx.Exec(ctx, `insert into app.source_fetches (
			endpoint_id, attempted_at, completed_at, outcome, status_code,
			final_url, content_type, duration_ms, raw_sha256, object_key
		) select endpoint.id, $2, $3, 'stored', 200, $4, $5, 1000, $6, $7
		from app.source_endpoints endpoint where endpoint.registry_id = $1`,
			registryID, firstSeen, firstFetched, url, contentType, digest[:], key); err != nil {
			t.Fatal(err)
		}
	}
	recordLater := func(url, key, contentType string, digest [sha256.Size]byte) {
		t.Helper()
		result := fetcher.Result{
			Outcome: fetcher.OutcomeStored, SHA256: digest, ObjectKey: key,
			Attempts: []fetcher.Attempt{{
				AttemptedAt: later, CompletedAt: later.Add(time.Second),
				StatusCode: 200, FinalURL: url, ContentType: contentType,
			}},
		}
		if err := (pgstore.Store{}).Record(ctx, tx, registryB, sourceID,
			fetcher.ContentPolicyLinkAndExcerpt, result); err != nil {
			t.Fatal(err)
		}
	}
	const uniqueURL = "https://example.com/unique.xml"
	uniqueDigest := sha256.Sum256([]byte("unique legacy raw"))
	uniqueKey := "raw/" + sourceID + "/unique.xml"
	if _, err := tx.Exec(ctx, `insert into app.raw_documents (
		source_id, canonical_url, object_key, raw_sha256, first_seen_at,
		first_fetched_at, content_policy
	) values ($1, $2, $3, $4, $5, $6, 'link-and-excerpt')`,
		sourceID, uniqueURL, uniqueKey, uniqueDigest[:], firstSeen, firstFetched); err != nil {
		t.Fatal(err)
	}
	insertOriginalFetch(registryA, uniqueURL, uniqueKey, "application/rss+xml", uniqueDigest)
	recordLater(uniqueURL, "raw/"+sourceID+"/later.xml", "application/atom+xml", uniqueDigest)
	var owner, connector, contentType string
	if err := tx.QueryRow(ctx, `select source_registry_id, source_connector, source_content_type
		from app.raw_documents where source_id = $1 and canonical_url = $2`,
		sourceID, uniqueURL).Scan(&owner, &connector, &contentType); err != nil {
		t.Fatal(err)
	}
	if owner != registryA || connector != "rss" || contentType != "application/rss+xml" {
		t.Fatalf("later fetch claimed original raw: owner=%q connector=%q type=%q",
			owner, connector, contentType)
	}

	const ambiguousURL = "https://example.com/ambiguous.xml"
	ambiguousDigest := sha256.Sum256([]byte("ambiguous legacy raw"))
	ambiguousKey := "raw/" + sourceID + "/ambiguous.xml"
	if _, err := tx.Exec(ctx, `insert into app.raw_documents (
		source_id, canonical_url, raw_sha256, first_seen_at,
		first_fetched_at, content_policy, raw_pruned_at
	) values ($1, $2, $3, $4, $5, 'link-and-excerpt', $6)`,
		sourceID, ambiguousURL, ambiguousDigest[:], firstSeen, firstFetched, later); err != nil {
		t.Fatal(err)
	}
	insertOriginalFetch(registryA, ambiguousURL, ambiguousKey, "application/rss+xml", ambiguousDigest)
	insertOriginalFetch(registryB, ambiguousURL, ambiguousKey, "application/atom+xml", ambiguousDigest)
	recordLater(ambiguousURL, ambiguousKey, "application/atom+xml", ambiguousDigest)
	var unresolvedOwner *string
	var restoredKey string
	var prunedAt *time.Time
	if err := tx.QueryRow(ctx, `select source_registry_id, object_key, raw_pruned_at
		from app.raw_documents where source_id = $1 and canonical_url = $2`,
		sourceID, ambiguousURL).Scan(&unresolvedOwner, &restoredKey, &prunedAt); err != nil {
		t.Fatal(err)
	}
	if unresolvedOwner != nil || restoredKey != ambiguousKey || prunedAt != nil {
		t.Fatalf("ambiguous pruned raw changed owner or evidence: owner=%v key=%q pruned=%v",
			unresolvedOwner, restoredKey, prunedAt)
	}
}

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
