package pgstore_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/parsing/pgstore"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

func TestStorePersistsIdempotentRevisionChainAndFailures(t *testing.T) {
	pool := openIntegrationDatabase(t)
	ctx := context.Background()
	canonicalURL := fmt.Sprintf("https://go.dev/blog/feed.atom?parser-integration=%d", time.Now().UnixNano())
	baseTime := time.Date(2026, time.August, 29, 15, 0, 0, 0, time.UTC)
	rawDocumentIDs := []string{
		insertRawDocument(t, pool, canonicalURL, "raw revision one", baseTime),
		insertRawDocument(t, pool, canonicalURL, "raw revision two", baseTime.Add(time.Minute)),
		insertRawDocument(t, pool, canonicalURL, "raw revision three with reverted normalized content", baseTime.Add(2*time.Minute)),
	}
	t.Cleanup(func() {
		cleanupRawDocuments(t, pool, rawDocumentIDs)
	})

	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first := parseStructuredResult(t, "operational")
	second := parseStructuredResult(t, "degraded")

	initial := recordSuccess(t, store, rawDocumentIDs[0], first, baseTime)
	if initial.Outcome != "created" || initial.MaterialChange || initial.PreviousRevisionID != "" {
		t.Fatalf("initial result = %+v", initial)
	}
	unchanged := recordSuccess(t, store, rawDocumentIDs[0], first, baseTime.Add(10*time.Second))
	if unchanged.Outcome != "unchanged" || unchanged.RevisionID != initial.RevisionID {
		t.Fatalf("unchanged result = %+v, initial = %+v", unchanged, initial)
	}
	changed := recordSuccess(t, store, rawDocumentIDs[1], second, baseTime.Add(time.Minute))
	if changed.Outcome != "created" || !changed.MaterialChange || changed.PreviousRevisionID != initial.RevisionID {
		t.Fatalf("changed result = %+v, initial = %+v", changed, initial)
	}
	reverted := recordSuccess(t, store, rawDocumentIDs[2], first, baseTime.Add(2*time.Minute))
	if reverted.Outcome != "created" || !reverted.MaterialChange || reverted.PreviousRevisionID != changed.RevisionID || reverted.RevisionID == initial.RevisionID {
		t.Fatalf("reverted result = %+v, initial = %+v, changed = %+v", reverted, initial, changed)
	}

	if err := store.RecordFailure(ctx, parsing.FailureRequest{
		RawDocumentID: rawDocumentIDs[2],
		ParserName:    "structured-api-json",
		ErrorCode:     parsing.ErrorInvalidDocument,
		Warnings:      []parsing.Warning{},
		AttemptedAt:   baseTime.Add(3 * time.Minute),
		CompletedAt:   baseTime.Add(3*time.Minute + 25*time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}

	var revisionCount int
	var attemptCount int
	var failedCount int
	if err := pool.QueryRow(ctx, `
		select count(*)
		from app.content_revisions revision
		join app.raw_documents raw on raw.id = revision.raw_document_id
		where raw.canonical_url = $1`, canonicalURL).Scan(&revisionCount); err != nil {
		t.Fatalf("count content revisions: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*), count(*) filter (where outcome = 'failed')
		from app.source_parse_attempts
		where raw_document_id = any($1::uuid[])`, rawDocumentIDs).Scan(&attemptCount, &failedCount); err != nil {
		t.Fatalf("count parse attempts: %v", err)
	}
	if revisionCount != 3 || attemptCount != 5 || failedCount != 1 {
		t.Fatalf("revisions=%d attempts=%d failures=%d", revisionCount, attemptCount, failedCount)
	}
}

func TestStoreRejectsMismatchedObjectIdentityAndInvalidAttempts(t *testing.T) {
	pool := openIntegrationDatabase(t)
	canonicalURL := fmt.Sprintf("https://go.dev/blog/feed.atom?parser-validation=%d", time.Now().UnixNano())
	observedAt := time.Date(2026, time.August, 29, 16, 0, 0, 0, time.UTC)
	rawDocumentID := insertRawDocument(t, pool, canonicalURL, "raw validation revision", observedAt)
	t.Cleanup(func() {
		cleanupRawDocuments(t, pool, []string{rawDocumentID})
	})
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result := parseStructuredResult(t, "operational")
	_, err = store.RecordSuccess(context.Background(), parsing.RecordRequest{
		RawDocumentID: rawDocumentID,
		ObjectKey:     "normalized/other-source/" + fmt.Sprintf("%x", result.NormalizedSHA256) + ".txt",
		Result:        result,
		AttemptedAt:   observedAt,
		CompletedAt:   observedAt.Add(time.Second),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match expected key") {
		t.Fatalf("RecordSuccess() error = %v", err)
	}
	if err := store.RecordFailure(context.Background(), parsing.FailureRequest{
		RawDocumentID: rawDocumentID,
		ErrorCode:     parsing.ErrorInvalidDocument,
		AttemptedAt:   observedAt,
		CompletedAt:   observedAt.Add(time.Second),
	}); err == nil {
		t.Fatal("RecordFailure() accepted a missing parser name")
	}
	if _, err := pgstore.New(nil); err == nil {
		t.Fatal("New() accepted a nil database")
	}
}

func openIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for the parser integration test")
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), databaseConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertRawDocument(t *testing.T, pool *pgxpool.Pool, canonicalURL string, payload string, observedAt time.Time) string {
	t.Helper()
	digest := sha256.Sum256([]byte(payload))
	objectKey, err := storage.RawObjectKey("go-blog", observedAt, digest, "application/atom+xml")
	if err != nil {
		t.Fatalf("RawObjectKey() error = %v", err)
	}
	var rawDocumentID string
	err = pool.QueryRow(context.Background(), `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256,
			first_seen_at, first_fetched_at, content_policy
		) values ('go-blog', $1, $2, $3, $4, $4, 'link-and-excerpt')
		returning id::text`, canonicalURL, objectKey, digest[:], observedAt).Scan(&rawDocumentID)
	if err != nil {
		t.Fatalf("insert raw document: %v", err)
	}
	return rawDocumentID
}

func parseStructuredResult(t *testing.T, status string) parsing.Result {
	t.Helper()
	result, err := parsing.New().Parse(context.Background(), parsing.Request{
		Connector:   sources.ConnectorStructuredAPI,
		URL:         "https://fixtures.example.test/status",
		ContentType: "application/json; charset=utf-8",
		Body: strings.NewReader(fmt.Sprintf(
			`{"status":%q,"services":[{"id":"api","name":"Fixture API","status":%q}]}`,
			status,
			status,
		)),
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return result
}

func recordSuccess(t *testing.T, store *pgstore.Store, rawDocumentID string, result parsing.Result, attemptedAt time.Time) parsing.RecordResult {
	t.Helper()
	objectKey, err := storage.NormalizedObjectKey("go-blog", result.NormalizedSHA256)
	if err != nil {
		t.Fatalf("NormalizedObjectKey() error = %v", err)
	}
	recorded, err := store.RecordSuccess(context.Background(), parsing.RecordRequest{
		RawDocumentID: rawDocumentID,
		ObjectKey:     objectKey,
		Result:        result,
		AttemptedAt:   attemptedAt,
		CompletedAt:   attemptedAt.Add(25 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("RecordSuccess() error = %v", err)
	}
	return recorded
}

func cleanupRawDocuments(t *testing.T, pool *pgxpool.Pool, rawDocumentIDs []string) {
	t.Helper()
	ctx := context.Background()
	for index := len(rawDocumentIDs) - 1; index >= 0; index-- {
		rawDocumentID := rawDocumentIDs[index]
		if _, err := pool.Exec(ctx, `delete from app.source_parse_attempts where raw_document_id = $1::uuid`, rawDocumentID); err != nil {
			t.Errorf("delete parse attempts for %s: %v", rawDocumentID, err)
		}
		if _, err := pool.Exec(ctx, `delete from app.content_revisions where raw_document_id = $1::uuid`, rawDocumentID); err != nil {
			t.Errorf("delete content revisions for %s: %v", rawDocumentID, err)
		}
		if _, err := pool.Exec(ctx, `delete from app.raw_documents where id = $1::uuid`, rawDocumentID); err != nil {
			t.Errorf("delete raw document %s: %v", rawDocumentID, err)
		}
	}
}
