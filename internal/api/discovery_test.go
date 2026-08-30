package api_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/discovery"
	"github.com/traweezy/relantern/internal/embedding"
)

const discoveryUserID = "01991234-5678-7abc-8def-0123456789ad"

type fixtureDiscoveryEmbedder struct{}

func (fixtureDiscoveryEmbedder) Embed(context.Context, string) (embedding.Vector, error) {
	return embedding.Vector{1}, nil
}

type fixtureDiscoveryStore struct {
	searchRequest discovery.SearchRequest
	err           error
}

func (store *fixtureDiscoveryStore) Search(
	_ context.Context,
	request discovery.SearchRequest,
	_ embedding.Vector,
) (discovery.SearchResponse, error) {
	store.searchRequest = request
	return discovery.SearchResponse{
		Query: request.Query, Results: []discovery.SearchResult{}, ResultCount: 0,
		Explanation: "Fixture hybrid search.",
	}, store.err
}

func (store *fixtureDiscoveryStore) ListSavedSearches(
	context.Context,
	string,
) ([]discovery.SavedSearch, error) {
	return []discovery.SavedSearch{}, store.err
}

func (store *fixtureDiscoveryStore) SaveSearch(
	_ context.Context,
	request discovery.SaveSearchRequest,
	now time.Time,
) (discovery.SavedSearch, error) {
	return discovery.SavedSearch{
		ID: "01991234-5678-7abc-8def-0123456789aa", Name: request.Name,
		Query: request.Query, Filters: request.Filters, CreatedAt: now, UpdatedAt: now,
	}, store.err
}

func (store *fixtureDiscoveryStore) DeleteSavedSearch(context.Context, string, string) error {
	return store.err
}

func (store *fixtureDiscoveryStore) Releases(
	context.Context,
	string,
	time.Time,
) (discovery.ReleaseCatalog, error) {
	return discovery.ReleaseCatalog{
		GeneratedAt:  time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
		Technologies: []discovery.TechnologyRelease{},
		ComingSoon:   []discovery.ReleaseEntry{},
	}, store.err
}

func (store *fixtureDiscoveryStore) ImportURL(
	_ context.Context,
	request discovery.ManualCaptureRequest,
) (discovery.ManualCapture, error) {
	return discovery.ManualCapture{
		ID:        "01991234-5678-7abc-8def-0123456789ae",
		URL:       request.URL,
		State:     "queued",
		CreatedAt: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
	}, store.err
}

func (store *fixtureDiscoveryStore) PreviewOPML(
	context.Context,
	string,
	[]byte,
	time.Time,
) (discovery.ImportPreview, error) {
	return discovery.ImportPreview{
		ID:         "01991234-5678-7abc-8def-0123456789af",
		Candidates: []discovery.ImportCandidate{},
		ExpiresAt:  time.Date(2026, time.August, 29, 12, 30, 0, 0, time.UTC),
	}, store.err
}

func (store *fixtureDiscoveryStore) CommitOPML(
	context.Context,
	discovery.ImportCommitRequest,
	time.Time,
) (discovery.ImportCommitResult, error) {
	return discovery.ImportCommitResult{
		ImportedCount: 1, PendingSourceIDs: []string{"owner-opml-fixture"},
	}, store.err
}

func (store *fixtureDiscoveryStore) ExportOPML(context.Context, string) (discovery.Export, error) {
	return discovery.Export{
		ContentType: "text/x-opml; charset=utf-8",
		Filename:    "relantern-sources.opml",
		Payload:     []byte("<opml version=\"2.0\"></opml>"),
	}, store.err
}

func (store *fixtureDiscoveryStore) ExportMetadata(
	context.Context,
	discovery.MetadataExportRequest,
) (discovery.Export, error) {
	return discovery.Export{
		ContentType: "application/json",
		Filename:    "relantern-metadata.json",
		Payload:     []byte("{\"stories\":[]}"),
	}, store.err
}

func (store *fixtureDiscoveryStore) ExportMarkdown(
	context.Context,
	discovery.MarkdownExportRequest,
) (discovery.Export, error) {
	return discovery.Export{
		ContentType: "text/markdown; charset=utf-8",
		Filename:    "relantern-stories.md",
		Payload:     []byte("# Relantern export\n"),
	}, store.err
}

func newDiscoveryApplication(t *testing.T, store *fixtureDiscoveryStore, token string) api.Application {
	t.Helper()
	service, err := discovery.NewService(store, fixtureDiscoveryEmbedder{})
	if err != nil {
		t.Fatalf("discovery.NewService() error = %v", err)
	}
	return api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithClock(func() time.Time {
			return time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
		}),
		api.WithDiscovery(service, token),
	)
}

func authorizeDiscoveryRequest(request *http.Request, token string) {
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Relantern-User-ID", discoveryUserID)
}

func TestDiscoveryEndpointsRequireServiceCredential(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newDiscoveryApplication(t, &fixtureDiscoveryStore{}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/releases", nil)
	request.Header.Set("X-Relantern-User-ID", discoveryUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestDiscoverySearchPropagatesExactFilters(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	store := &fixtureDiscoveryStore{}
	application := newDiscoveryApplication(t, store, token)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/search?q=postgres&topic=database&sourceTier=T0&lifecycle=stable&action=upgrade&saved=true&after=2026-08-01&before=2026-08-29",
		nil,
	)
	authorizeDiscoveryRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if store.searchRequest.UserID != discoveryUserID ||
		store.searchRequest.Query != "postgres" ||
		store.searchRequest.Topic != "database" ||
		store.searchRequest.SourceTier != "T0" ||
		store.searchRequest.LifecycleState != "stable" ||
		store.searchRequest.Action != "upgrade" ||
		store.searchRequest.Saved == nil || !*store.searchRequest.Saved ||
		store.searchRequest.After == nil || !store.searchRequest.After.Equal(time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)) ||
		store.searchRequest.Before == nil || !store.searchRequest.Before.Equal(time.Date(2026, time.August, 29, 23, 59, 59, 999_999_999, time.UTC)) {
		t.Fatalf("propagated search request = %+v", store.searchRequest)
	}
}

func TestDiscoverySearchRejectsReversedDateRange(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	store := &fixtureDiscoveryStore{}
	application := newDiscoveryApplication(t, store, token)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/search?q=postgres&after=2026-08-30&before=2026-08-29",
		nil,
	)
	authorizeDiscoveryRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestDiscoveryURLCaptureReturnsQueuedReceipt(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newDiscoveryApplication(t, &fixtureDiscoveryStore{}, token)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/inbox/import-url",
		strings.NewReader(`{"url":"https://go.dev/blog/go1.27","idempotencyKey":"0123456789abcdef"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	authorizeDiscoveryRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"queued"`) {
		t.Fatalf("capture response = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestDiscoveryExportsPreserveSafeDownloadHeaders(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newDiscoveryApplication(t, &fixtureDiscoveryStore{}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/exports/opml", nil)
	authorizeDiscoveryRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		response.Header().Get("Content-Disposition") != `attachment; filename="relantern-sources.opml"` ||
		!strings.HasPrefix(response.Header().Get("Content-Type"), "text/x-opml") {
		t.Fatalf("export response = status %d, headers %+v", response.Code, response.Header())
	}
}

func TestDiscoveryUnexpectedErrorDoesNotLeakDetails(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	secretError := errors.New("database password is secret")
	application := newDiscoveryApplication(t, &fixtureDiscoveryStore{err: secretError}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/releases", nil)
	authorizeDiscoveryRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), secretError.Error()) {
		t.Fatal("problem response leaked an internal error")
	}
}
