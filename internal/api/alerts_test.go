package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/intelligence"
)

type alertFixtureReader struct {
	fixtureReader
	err    error
	limit  int
	userID string
}

func (reader *alertFixtureReader) AlertHistory(_ context.Context, userID, _ string, limit int) (intelligence.AlertHistoryPage, error) {
	reader.userID = userID
	reader.limit = limit
	if reader.err != nil {
		return intelligence.AlertHistoryPage{}, reader.err
	}
	cursor := "next-page"
	return intelligence.AlertHistoryPage{
		Alerts: []intelligence.AlertHistoryItem{}, NextCursor: &cursor,
	}, nil
}

func newAlertHistoryApplication(reader intelligence.Reader, token string) api.Application {
	return api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithClock(func() time.Time { return time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC) }),
		api.WithIntelligence(reader, token),
	)
}

func alertHistoryRequest(application api.Application, token, userID, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if userID != "" {
		request.Header.Set("X-Relantern-User-ID", userID)
	}
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	return response
}

func TestAlertHistoryRequiresPrivateCredentialAndOwnerHeader(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	reader := &alertFixtureReader{}
	application := newAlertHistoryApplication(reader, token)
	for _, test := range []struct {
		name   string
		token  string
		userID string
		status int
	}{
		{"missing credential", "", storyID, http.StatusUnauthorized},
		{"wrong credential", strings.Repeat("w", 32), storyID, http.StatusUnauthorized},
		{"missing owner", token, "", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := alertHistoryRequest(application, test.token, test.userID, "/api/v1/alerts")
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("private service token leaked")
			}
		})
	}
	if reader.userID != "" {
		t.Fatal("unauthorized alert history request reached the reader")
	}
}

func TestAlertHistoryBoundsPageAndLinksToNextPage(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	reader := &alertFixtureReader{}
	application := newAlertHistoryApplication(reader, token)
	response := alertHistoryRequest(application, token, storyID, "/api/v1/alerts")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if reader.userID != storyID || reader.limit != 20 {
		t.Fatalf("reader received user %q and limit %d", reader.userID, reader.limit)
	}
	if got := response.Header().Get("Link"); got != `</api/v1/alerts?cursor=next-page&limit=20>; rel="next"` {
		t.Fatalf("Link = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var page intelligence.AlertHistoryPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Alerts == nil || page.NextCursor == nil || *page.NextCursor != "next-page" {
		t.Fatalf("page = %+v", page)
	}
	response = alertHistoryRequest(application, token, storyID, "/api/v1/alerts?limit=101")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized limit status = %d: %s", response.Code, response.Body.String())
	}
	response = alertHistoryRequest(application, token, storyID, "/api/v1/alerts?cursor=")
	if response.Code != http.StatusOK || reader.userID != storyID || reader.limit != 20 {
		t.Fatalf("empty cursor first-page alias = %d, %q, %d", response.Code, reader.userID, reader.limit)
	}
}

func TestAlertHistoryInvalidCursorReturnsProblem(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newAlertHistoryApplication(&alertFixtureReader{err: intelligence.ErrInvalidAlertHistory}, token)
	for _, path := range []string{"/api/v1/alerts?cursor=invalid"} {
		response := alertHistoryRequest(application, token, storyID, path)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d: %s", path, response.Code, response.Body.String())
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/problem+json") {
			t.Fatalf("GET %s Content-Type = %q", path, contentType)
		}
	}
}
