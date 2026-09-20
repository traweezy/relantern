package fakeprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAlertCaptureIsIdempotentAndKeepsDigestCapturesSeparate(t *testing.T) {
	t.Parallel()
	handler, err := New(KindDelivery, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	alertPayload := Capture{
		Kind: "critical_alert", AlertID: "alert-1", Attempt: 1,
		Channel: "discord", IdempotencyKey: "shared-key",
		Payload: json.RawMessage(`{"title":"Critical advisory","sourceUrl":"https://example.test/advisory","packageName":"package","ecosystem":"npm","currentVersion":"1.2.0","versionRange":"< 1.3.0"}`),
	}
	digestPayload := Capture{
		Attempt: 1, Channel: "discord", DigestID: "digest-1",
		IdempotencyKey: "shared-key", LocalDate: "2026-08-29",
		Payload:       json.RawMessage(`{"title":"Digest"}`),
		PayloadSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ScheduledFor:  time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	}
	for _, fixture := range []struct {
		payload Capture
		wantID  string
	}{
		{alertPayload, "capture:alert-1"},
		{alertPayload, "capture:alert-1"},
		{digestPayload, "capture:digest-1"},
	} {
		encoded, err := json.Marshal(fixture.payload)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(http.MethodPost, server.URL+"/capture", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", fixture.payload.IdempotencyKey)
		request.Header.Set("Authorization", "Bearer never-store-this")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted || result.ID != fixture.wantID {
			t.Fatalf("capture status/id = %d/%q, want 202/%q", response.StatusCode, result.ID, fixture.wantID)
		}
	}
	response, err := http.Get(server.URL + "/captures")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Count    int       `json:"count"`
		Captures []Capture `json:"captures"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || len(result.Captures) != 2 || result.Captures[0].Kind != "critical_alert" || result.Captures[0].AlertID != "alert-1" || result.Captures[1].DigestID != "digest-1" {
		t.Fatalf("captures = %+v", result)
	}
	if _, exists := result.Captures[0].Headers["Authorization"]; exists {
		t.Fatal("authorization header was captured")
	}
	viewer, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Body.Close()
	page, err := io.ReadAll(viewer.Body)
	if err != nil {
		t.Fatal(err)
	}
	if viewer.StatusCode != http.StatusOK || !strings.Contains(string(page), "Critical alert") ||
		!strings.Contains(string(page), "alert-1") || !strings.Contains(string(page), "digest-1") ||
		!strings.Contains(viewer.Header.Get("Content-Security-Policy"), "style-src 'sha256-") ||
		strings.Contains(viewer.Header.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatalf("capture viewer omitted alert or digest: %s", page)
	}
}

func TestAlertCaptureRejectsIncompletePayload(t *testing.T) {
	t.Parallel()
	handler, err := New(KindDelivery, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{
		`{"kind":"critical_alert","alertId":"alert-1","channel":"discord","attempt":1,"idempotencyKey":"key","payload":{}}`,
		`{"kind":"critical_alert","alertId":"alert-1","channel":"discord","attempt":0,"idempotencyKey":"key","payload":{"title":"critical"}}`,
		`{"kind":"unknown","alertId":"alert-1","channel":"discord","attempt":1,"idempotencyKey":"key","payload":{"title":"critical"}}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(payload))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("capture status = %d for %s, want 400", response.Code, payload)
		}
	}
	valid := `{"kind":"critical_alert","alertId":"alert-1","channel":"discord","attempt":1,"idempotencyKey":"key","payload":{"title":"Critical advisory","sourceUrl":"https://example.test/advisory","packageName":"package","ecosystem":"npm","currentVersion":"1.2.0","versionRange":"< 1.3.0"}}`
	request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(valid))
	request.Header.Set("Idempotency-Key", "different-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("capture status = %d for mismatched idempotency, want 400", response.Code)
	}
}
