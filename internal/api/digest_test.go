package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/digest"
)

const digestID = "01990000-0000-7000-8000-000000000002"
const digestUserID = "01990000-0000-7000-8000-000000000001"

type digestRepositoryFixture struct{}

func (digestRepositoryFixture) Snapshot(_ context.Context, _ string, now time.Time) (digest.DigestSnapshot, error) {
	return digest.DigestSnapshot{GeneratedAt: now, Digests: []digest.DigestRecord{}}, nil
}

func (digestRepositoryFixture) Get(_ context.Context, _, requestedID string) (digest.DigestRecord, error) {
	if requestedID != digestID {
		return digest.DigestRecord{}, digest.ErrNotFound
	}
	return digestFixture("failed"), nil
}

func (digestRepositoryFixture) Retry(_ context.Context, _, requestedID string, _ time.Time) (digest.DigestRecord, error) {
	if requestedID != digestID {
		return digest.DigestRecord{}, digest.ErrNotFound
	}
	return digestFixture("ready"), nil
}

func digestFixture(state string) digest.DigestRecord {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	return digest.DigestRecord{
		ID: digestID, OccurrenceID: "01990000-0000-7000-8000-000000000003",
		LocalDate: "2026-08-29", WindowStart: now.Add(-24 * time.Hour), WindowEnd: now,
		Channel: digest.ChannelDiscord, State: state, ItemLimit: 10, MinimumScore: 0.5,
		EmptyBehavior: "all_clear", ExecutiveSummary: "No material changes met the threshold.",
		Rendered: digest.DigestPayload{
			Channel: digest.ChannelDiscord, Title: "Relantern morning brief", ExecutiveSummary: "No material changes met the threshold.",
			LocalDate: "2026-08-29", WindowStart: now.Add(-24 * time.Hour), WindowEnd: now,
			GeneratedAt: now, Items: []digest.DigestRenderedItem{},
		},
		ProviderIdempotencyKey: "digest:fixture:discord", GeneratedAt: now,
		Items: []digest.DigestCandidate{},
	}
}

func TestDigestEndpointsRequireOwnerScopeAndPreserveRetryContract(t *testing.T) {
	t.Parallel()
	service, err := digest.NewService(digestRepositoryFixture{})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("d", 32)
	application := api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)), api.Info{Version: "test"},
		func(context.Context) error { return nil }, api.WithDigest(service, token),
	)
	for _, requestCase := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/digests"},
		{method: http.MethodGet, path: "/api/v1/digests/" + digestID},
		{method: http.MethodPost, path: "/api/v1/digests/" + digestID + "/retry-delivery"},
	} {
		request := httptest.NewRequest(requestCase.method, requestCase.path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Relantern-User-ID", digestUserID)
		response := httptest.NewRecorder()
		application.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d; body = %s", requestCase.method, requestCase.path, response.Code, response.Body.String())
		}
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/digests", nil)
	unauthorized.Header.Set("X-Relantern-User-ID", digestUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, unauthorized)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
