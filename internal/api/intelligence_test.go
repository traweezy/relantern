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
	"github.com/traweezy/relantern/internal/intelligence"
)

const storyID = "01991234-5678-7abc-8def-0123456789ab"

type fixtureReader struct{}

func (fixtureReader) Today(_ context.Context, generatedAt time.Time) (intelligence.TodaySnapshot, error) {
	return intelligence.TodaySnapshot{
		CoverageEndAt: generatedAt, CoverageStartAt: generatedAt.Add(-24 * time.Hour),
		DeliveryState: "pending", GeneratedAt: generatedAt, Stories: []intelligence.StorySummary{},
		Stats: intelligence.TodayStats{EstimatedCostUSD: "0.00000000"},
	}, nil
}

func (fixtureReader) Live(_ context.Context, generatedAt time.Time) (intelligence.LiveSnapshot, error) {
	return intelligence.LiveSnapshot{Events: []intelligence.LiveEvent{}, GeneratedAt: generatedAt}, nil
}

func (fixtureReader) Story(_ context.Context, requestedID string) (intelligence.StoryDetail, error) {
	if requestedID != storyID {
		return intelligence.StoryDetail{}, intelligence.ErrStoryNotFound
	}
	return intelligence.StoryDetail{
		StorySummary: intelligence.StorySummary{
			Confidence: "high", FirstSeenAt: time.Unix(1, 0).UTC(), Headline: "Fixture story",
			ID: storyID, LastChangedAt: time.Unix(2, 0).UTC(), ReadTimeMinutes: 1,
			RecommendedAction: "Review the fixture.", Signal: "general", SourceTier: "T0",
			Status: "new", Summary: "A bounded fixture summary.", WhyItMatters: "It tests the API boundary.",
		},
		Assertions: []intelligence.ClaimEvidence{}, Related: []intelligence.StorySummary{},
		Sources: []intelligence.Source{}, Uncertainties: []string{},
	}, nil
}

func newIntelligenceApplication(token string) api.Application {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fixedNow := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	return api.New(
		logger,
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithClock(func() time.Time { return fixedNow }),
		api.WithIntelligence(fixtureReader{}, token),
	)
}

func TestIntelligenceEndpointsRequireInternalCredential(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newIntelligenceApplication(token)
	for name, authorization := range map[string]string{
		"missing": "",
		"wrong":   "Bearer " + strings.Repeat("w", 32),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/today", nil)
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			response := httptest.NewRecorder()
			application.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("response disclosed the private service token")
			}
		})
	}
}

func TestIntelligenceEndpointsReturnBoundedContracts(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newIntelligenceApplication(token)
	for _, path := range []string{"/api/v1/today", "/api/v1/live", "/api/v1/stories/" + storyID} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		application.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body = %s", path, response.Code, http.StatusOK, response.Body.String())
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("GET %s content type = %q", path, contentType)
		}
	}
}

func TestUnknownIntelligenceStoryReturnsProblem(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := newIntelligenceApplication(token)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/stories/01999999-9999-7999-8999-999999999999",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}
