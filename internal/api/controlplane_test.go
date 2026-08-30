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
	"github.com/traweezy/relantern/internal/controlplane"
)

const (
	controlPlaneUserID     = "01991234-5678-7abc-8def-0123456789ae"
	controlPlaneScheduleID = "01991234-5678-7abc-8def-0123456789af"
)

var controlPlaneNow = time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)

type fixtureControlPlaneStore struct {
	err             error
	scheduleRequest controlplane.UpdateScheduleRequest
}

func (store *fixtureControlPlaneStore) Sources(
	context.Context,
	string,
	time.Time,
) (controlplane.SourcesSnapshot, error) {
	return controlplane.SourcesSnapshot{GeneratedAt: controlPlaneNow, Sources: []controlplane.ManagedSource{}}, store.err
}

func (store *fixtureControlPlaneStore) UpdateSourcePreference(
	context.Context,
	controlplane.UpdateSourcePreferenceRequest,
	time.Time,
) (controlplane.SourcePreference, error) {
	return controlplane.SourcePreference{}, store.err
}

func (store *fixtureControlPlaneStore) ActOnSource(
	context.Context,
	controlplane.SourceActionRequest,
	time.Time,
) (controlplane.ManagedSource, error) {
	return controlplane.ManagedSource{}, store.err
}

func (store *fixtureControlPlaneStore) Settings(
	context.Context,
	string,
	time.Time,
) (controlplane.SettingsSnapshot, error) {
	return controlplane.SettingsSnapshot{}, store.err
}

func (store *fixtureControlPlaneStore) UpdateSettings(
	context.Context,
	controlplane.UpdateSettingsRequest,
	time.Time,
) (controlplane.SettingsSnapshot, error) {
	return controlplane.SettingsSnapshot{}, store.err
}

func (store *fixtureControlPlaneStore) Schedule(
	context.Context,
	string,
	string,
) (controlplane.ScheduleDefinition, error) {
	return controlplane.ScheduleDefinition{
		ID: controlPlaneScheduleID, ScheduleType: "daily_digest",
		Timezone: "America/New_York", LocalTime: "08:30",
		DaysOfWeek: []int16{1, 2, 3, 4, 5},
	}, store.err
}

func (store *fixtureControlPlaneStore) UpdateSchedule(
	_ context.Context,
	request controlplane.UpdateScheduleRequest,
	_ time.Time,
) (controlplane.ScheduleDefinition, error) {
	store.scheduleRequest = request
	return controlplane.ScheduleDefinition{
		ID: request.ScheduleID, ScheduleType: "daily_digest",
		Timezone: request.Timezone, LocalTime: request.LocalTime,
		DaysOfWeek: request.DaysOfWeek, NextDueAt: request.NextDueAt,
	}, store.err
}

func (store *fixtureControlPlaneStore) ActOnSchedule(
	context.Context,
	controlplane.ScheduleActionRequest,
	time.Time,
) (controlplane.ScheduleActionResult, error) {
	return controlplane.ScheduleActionResult{}, store.err
}

func (store *fixtureControlPlaneStore) PreviewSchedule(
	context.Context,
	string,
	string,
	time.Time,
) (controlplane.SchedulePreview, error) {
	return controlplane.SchedulePreview{}, store.err
}

func (store *fixtureControlPlaneStore) Operations(
	context.Context,
	string,
	time.Time,
	controlplane.DeploymentMetadata,
) (controlplane.OperationsSnapshot, error) {
	return controlplane.OperationsSnapshot{}, store.err
}

func newControlPlaneApplication(t *testing.T, store *fixtureControlPlaneStore, token string) api.Application {
	t.Helper()
	service, err := controlplane.NewService(store, controlplane.DeploymentMetadata{
		Environment: "test", Version: "test", GitSHA: "test-sha",
	})
	if err != nil {
		t.Fatalf("controlplane.NewService() error = %v", err)
	}
	return api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithClock(func() time.Time { return controlPlaneNow }),
		api.WithControlPlane(service, token),
	)
}

func authorizeControlPlaneRequest(request *http.Request, token string) {
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Relantern-User-ID", controlPlaneUserID)
}

func TestControlPlaneEndpointsRequireServiceCredential(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("c", 32)
	application := newControlPlaneApplication(t, &fixtureControlPlaneStore{}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	request.Header.Set("X-Relantern-User-ID", controlPlaneUserID)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestControlPlaneScheduleUpdatePropagatesValidatedPolicy(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("c", 32)
	store := &fixtureControlPlaneStore{}
	application := newControlPlaneApplication(t, store, token)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/schedules/"+controlPlaneScheduleID,
		strings.NewReader(`{
			"expectedVersion":4,
			"timezone":"America/New_York",
			"localTime":"08:30",
			"daysOfWeek":[1,2,3,4,5],
			"enabled":true,
			"catchupPolicy":"catch_up",
			"catchupGraceMinutes":360,
			"weekendMode":"off",
			"maximumItems":10,
			"minimumScore":0.65,
			"includeComingSoon":true,
			"includeRadarCandidates":false,
			"includeLaterReminders":true,
			"emptyBehavior":"all_clear",
			"channels":["dashboard"]
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	authorizeControlPlaneRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if store.scheduleRequest.UserID != controlPlaneUserID ||
		store.scheduleRequest.ScheduleID != controlPlaneScheduleID ||
		store.scheduleRequest.ExpectedVersion != 4 ||
		store.scheduleRequest.WeekendMode != "off" ||
		store.scheduleRequest.MaximumItems != 10 ||
		store.scheduleRequest.EmptyBehavior != "all_clear" ||
		len(store.scheduleRequest.Channels) != 1 || store.scheduleRequest.Channels[0] != "dashboard" ||
		store.scheduleRequest.NextDueAt.IsZero() {
		t.Fatalf("propagated schedule request = %+v", store.scheduleRequest)
	}
}

func TestControlPlaneUnexpectedErrorDoesNotLeakDetails(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("c", 32)
	secretError := errors.New("database password is secret")
	application := newControlPlaneApplication(t, &fixtureControlPlaneStore{err: secretError}, token)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	authorizeControlPlaneRequest(request, token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), secretError.Error()) {
		t.Fatal("problem response leaked an internal error")
	}
}
