package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/traweezy/relantern/internal/api"
)

func TestReadinessIncludesReleaseSHAOnlyWhenSchemaReady(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	checkError := error(nil)
	application := api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test", GitSHA: sha},
		func(context.Context) error { return checkError },
	)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ready Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ready" || payload["gitSha"] != sha {
		t.Fatalf("ready payload = %v", payload)
	}
	checkError = errors.New("schema drift")
	response = httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unready Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}
