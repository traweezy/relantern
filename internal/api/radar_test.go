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
	"github.com/traweezy/relantern/internal/radar"
)

const radarCandidateID = "01991234-5678-7abc-8def-0123456789bd"

type fixtureRadarStore struct {
	decision radar.DecisionRequest
	queued   radar.QueueDiscoveryRequest
}

func (store *fixtureRadarStore) Snapshot(context.Context, string, time.Time) (radar.Snapshot, error) {
	return radar.Snapshot{GeneratedAt: controlPlaneNow, States: []radar.State{radar.StateAssess}, Candidates: []radar.Candidate{}, Runs: []radar.DiscoveryRun{}}, nil
}

func (store *fixtureRadarStore) QueueDiscovery(_ context.Context, request radar.QueueDiscoveryRequest, _ time.Time) (radar.DiscoveryRun, error) {
	store.queued = request
	return radar.DiscoveryRun{ID: controlPlaneScheduleID, TriggerType: "owner", State: "queued", RequestedAt: controlPlaneNow}, nil
}

func (store *fixtureRadarStore) Decide(_ context.Context, request radar.DecisionRequest, _ time.Time) (radar.Candidate, error) {
	store.decision = request
	return radar.Candidate{ID: request.CandidateID, CurrentState: request.State}, nil
}

func newRadarApplication(t *testing.T, store *fixtureRadarStore, token string) api.Application {
	t.Helper()
	service, err := radar.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithClock(func() time.Time { return controlPlaneNow }),
		api.WithRadar(service, token),
	)
}

func TestRadarDecisionRequiresInternalCredential(t *testing.T) {
	t.Parallel()
	application := newRadarApplication(t, &fixtureRadarStore{}, strings.Repeat("r", 32))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radar", nil)
	request.Header.Set("X-Relantern-User-ID", controlPlaneUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestRadarOwnerDecisionPropagatesCompleteContext(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("r", 32)
	store := &fixtureRadarStore{}
	application := newRadarApplication(t, store, token)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/radar/candidates/"+radarCandidateID+"/decisions", strings.NewReader(`{
		"expectedVersion":2,
		"state":"trial",
		"rationale":"Run a bounded trial against the incumbent.",
		"evidence":{"url":"https://example.com/evidence"},
		"reviewAt":"2026-09-28T12:00:00Z",
		"applicableProjectTypes":["web"],
		"compatibilityRequirements":["Node 26"],
		"exitConditions":["Rollback on bundle regression"]
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Relantern-User-ID", controlPlaneUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.decision.UserID != controlPlaneUserID || store.decision.CandidateID != radarCandidateID ||
		store.decision.State != radar.StateTrial || len(store.decision.ExitConditions) != 1 {
		t.Fatalf("propagated decision = %+v", store.decision)
	}
}
