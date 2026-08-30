package fakeprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeliveryCaptureIsIdempotentAndRedactsCredentials(t *testing.T) {
	t.Parallel()
	handler, err := New(KindDelivery, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create delivery provider: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	payload, err := json.Marshal(Capture{
		Attempt:        1,
		Channel:        "discord",
		DigestID:       "01900000-0000-7000-8000-000000000001",
		IdempotencyKey: "digest:01900000-0000-7000-8000-000000000001:discord",
		LocalDate:      "2026-08-29",
		Payload:        json.RawMessage(`{"title":"Daily digest"}`),
		PayloadSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ScheduledFor:   time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("encode capture: %v", err)
	}
	for range 2 {
		request, requestErr := http.NewRequest(http.MethodPost, server.URL+"/capture", bytes.NewReader(payload))
		if requestErr != nil {
			t.Fatalf("create capture request: %v", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "digest:01900000-0000-7000-8000-000000000001:discord")
		request.Header.Set("Authorization", "Bearer must-not-be-captured")
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatalf("capture delivery: %v", requestErr)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("capture status = %d, want %d", response.StatusCode, http.StatusAccepted)
		}
	}

	response, err := http.Get(server.URL + "/captures")
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	defer response.Body.Close()
	var result struct {
		Count    int       `json:"count"`
		Captures []Capture `json:"captures"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode captures: %v", err)
	}
	if result.Count != 1 || len(result.Captures) != 1 {
		t.Fatalf("captures = %#v, want one idempotent capture", result)
	}
	if _, exists := result.Captures[0].Headers["Authorization"]; exists {
		t.Fatal("authorization header was captured")
	}
}

func TestDeliveryCaptureRejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	handler, err := New(KindDelivery, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create delivery provider: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(`{"attempt":0}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("capture status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
