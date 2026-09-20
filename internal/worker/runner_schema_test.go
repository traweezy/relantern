package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/clock"
)

func TestWorkerReadinessRejectsSchemaDriftAfterStartup(t *testing.T) {
	now := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	health := NewSchedulerHealth(nil, clock.NewFixed(now), time.Minute)
	health.RecordSuccess()
	runner := &Runner{
		health: health,
		schemaReady: func(context.Context) error {
			return errors.New("schema drift")
		},
	}
	response := httptest.NewRecorder()
	runner.healthHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want 503", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("readiness Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}
