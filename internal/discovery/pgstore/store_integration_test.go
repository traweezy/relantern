package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/discovery"
	discoverystore "github.com/traweezy/relantern/internal/discovery/pgstore"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/search"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
)

func TestStoreKeepsManualCaptureAndOPMLImportsPending(t *testing.T) {
	pool := openDiscoveryDatabase(t)
	userID := insertDiscoveryUser(t, pool)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `
			delete from river.river_job
			where kind = 'process_manual_capture'
				and args->>'captureId' in (
					select id::text from app.manual_captures where user_id = $1::uuid
				)`, userID)
		_, _ = pool.Exec(ctx, `delete from app.users where id = $1::uuid`, userID)
		_, _ = pool.Exec(ctx, `
			delete from app.source_endpoints
			where source_id in (select id from app.sources where owner = $1)`, "owner:"+userID)
		_, _ = pool.Exec(ctx, `delete from app.sources where owner = $1`, "owner:"+userID)
	})
	searchIndex, err := searchstore.New(pool, embedding.DefaultDimensions, search.DefaultRRFK)
	if err != nil {
		t.Fatalf("searchstore.New() error = %v", err)
	}
	jobs, err := jobqueue.NewIsolatedTestInserter("test_discovery")
	if err != nil {
		t.Fatalf("jobqueue.NewIsolatedTestInserter() error = %v", err)
	}
	store, err := discoverystore.New(pool, searchIndex, jobs, true)
	if err != nil {
		t.Fatalf("discoverystore.New() error = %v", err)
	}

	capture, err := store.ImportURL(context.Background(), discovery.ManualCaptureRequest{
		UserID: userID, URL: "http://fake-source:8090/article.html",
		IdempotencyKey: "discovery-integration-capture",
	})
	if err != nil {
		t.Fatalf("ImportURL() error = %v", err)
	}
	replayed, err := store.ImportURL(context.Background(), discovery.ManualCaptureRequest{
		UserID: userID, URL: "http://fake-source:8090/article.html",
		IdempotencyKey: "discovery-integration-capture",
	})
	if err != nil || replayed.ID != capture.ID {
		t.Fatalf("idempotent ImportURL() = %+v, error %v", replayed, err)
	}
	var sourceID string
	var sourceEnabled bool
	var validationState string
	if err := pool.QueryRow(context.Background(), `
		select capture.source_id, capture.endpoint_id::text,
			source.enabled, source.validation_state
		from app.manual_captures capture
		join app.sources source on source.id = capture.source_id
		where capture.id = $1::uuid`, capture.ID).Scan(
		&sourceID, new(string), &sourceEnabled, &validationState,
	); err != nil {
		t.Fatalf("inspect manual capture: %v", err)
	}
	if sourceEnabled || validationState != "pending" || capture.State != "queued" {
		t.Fatalf("manual capture state = source enabled %t, validation %q, capture %q", sourceEnabled, validationState, capture.State)
	}
	var queuedJobs int
	if err := pool.QueryRow(context.Background(), `
		select count(*) from river.river_job
		where kind = 'process_manual_capture' and args->>'captureId' = $1`, capture.ID).Scan(&queuedJobs); err != nil {
		t.Fatalf("count manual-capture jobs: %v", err)
	}
	if queuedJobs != 1 {
		t.Fatalf("manual-capture jobs = %d, want 1", queuedJobs)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	feedURL := "https://feeds-" + suffix + ".example.test/releases.xml"
	preview, err := store.PreviewOPML(
		context.Background(),
		userID,
		[]byte(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Fixture releases" type="rss" xmlUrl="`+feedURL+`" /></body></opml>`),
		time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("PreviewOPML() error = %v", err)
	}
	if len(preview.Candidates) != 1 || !preview.Candidates[0].Valid || preview.Candidates[0].Duplicate {
		t.Fatalf("OPML preview = %+v", preview)
	}
	committed, err := store.CommitOPML(context.Background(), discovery.ImportCommitRequest{
		UserID: userID, PreviewID: preview.ID, ApprovedIDs: []string{preview.Candidates[0].ID},
	}, time.Date(2026, time.August, 29, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CommitOPML() error = %v", err)
	}
	if committed.ImportedCount != 1 || len(committed.PendingSourceIDs) != 1 {
		t.Fatalf("OPML commit = %+v", committed)
	}
	if _, err := store.CommitOPML(context.Background(), discovery.ImportCommitRequest{
		UserID: userID, PreviewID: preview.ID, ApprovedIDs: []string{preview.Candidates[0].ID},
	}, time.Date(2026, time.August, 29, 12, 2, 0, 0, time.UTC)); !errors.Is(err, discovery.ErrConflict) {
		t.Fatalf("replayed CommitOPML() error = %v, want conflict", err)
	}
	var importedEnabled bool
	var importedState string
	if err := pool.QueryRow(context.Background(), `
		select enabled, validation_state from app.sources where id = $1`, committed.PendingSourceIDs[0]).Scan(
		&importedEnabled, &importedState,
	); err != nil {
		t.Fatalf("inspect imported source: %v", err)
	}
	if importedEnabled || importedState != "pending" {
		t.Fatalf("imported source = enabled %t, state %q", importedEnabled, importedState)
	}
	exported, err := store.ExportOPML(context.Background(), userID)
	if err != nil || !strings.Contains(string(exported.Payload), feedURL) {
		t.Fatalf("ExportOPML() = %q, error %v", exported.Payload, err)
	}

	saved, err := store.SaveSearch(context.Background(), discovery.SaveSearchRequest{
		UserID: userID, Name: "Database releases", Query: "postgres replication",
		Filters: discovery.SearchFilters{SourceTier: "T0", LifecycleState: "stable"},
	}, time.Date(2026, time.August, 29, 12, 3, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SaveSearch() error = %v", err)
	}
	listed, err := store.ListSavedSearches(context.Background(), userID)
	if err != nil || len(listed) != 1 || listed[0].ID != saved.ID || listed[0].Filters.SourceTier != "T0" {
		t.Fatalf("ListSavedSearches() = %+v, error %v", listed, err)
	}
	if err := store.DeleteSavedSearch(context.Background(), userID, saved.ID); err != nil {
		t.Fatalf("DeleteSavedSearch() error = %v", err)
	}
	if err := store.DeleteSavedSearch(context.Background(), userID, saved.ID); !errors.Is(err, discovery.ErrNotFound) {
		t.Fatalf("replayed DeleteSavedSearch() error = %v, want not found", err)
	}
}

func openDiscoveryDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for discovery integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("config.LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertDiscoveryUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	suffix := time.Now().UnixNano()
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		) values ($1, $2, 'Discovery integration', 'America/New_York', $3, true)
		returning id::text`,
		suffix,
		fmt.Sprintf("discovery-integration-%d", suffix),
		fmt.Sprintf("discovery-integration-%d@tests.relantern.local", suffix),
	).Scan(&userID); err != nil {
		t.Fatalf("insert discovery user: %v", err)
	}
	return userID
}
