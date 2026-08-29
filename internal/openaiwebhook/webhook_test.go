package openaiwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type acceptorStub struct {
	event  VerifiedEvent
	result AcceptResult
	err    error
	calls  int
}

func (stub *acceptorStub) Accept(_ context.Context, event VerifiedEvent, _ time.Time) (AcceptResult, error) {
	stub.calls++
	stub.event = event
	return stub.result, stub.err
}

func TestPrivateWebhookHandlerAuthenticatesAndAcceptsOneBoundedEvent(t *testing.T) {
	store := &acceptorStub{result: AcceptResult{Accepted: true}}
	handler, err := NewHandler(store, strings.Repeat("a", 32), slog.Default())
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	event := VerifiedEvent{
		WebhookID: "wh_123", EventID: "evt_123", EventType: "response.completed",
		ResponseID: "resp_123", EventCreatedAt: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
	}
	body, _ := json.Marshal(event)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/openai/events", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || store.calls != 1 || store.event.ResponseID != "resp_123" {
		t.Fatalf("response = %d %s, store = %+v", recorder.Code, recorder.Body.String(), store)
	}
}

func TestPrivateWebhookHandlerRejectsBadAuthMediaTypeAndBodies(t *testing.T) {
	store := &acceptorStub{result: AcceptResult{Accepted: true}}
	handler, err := NewHandler(store, strings.Repeat("b", 32), slog.Default())
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	tests := []struct {
		name          string
		authorization string
		contentType   string
		body          string
		wantStatus    int
	}{
		{name: "auth", contentType: "application/json", body: `{}`, wantStatus: http.StatusUnauthorized},
		{name: "media", authorization: "Bearer " + strings.Repeat("b", 32), contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown", authorization: "Bearer " + strings.Repeat("b", 32), contentType: "application/json", body: `{"unexpected":true}`, wantStatus: http.StatusBadRequest},
		{name: "oversize", authorization: "Bearer " + strings.Repeat("b", 32), contentType: "application/json", body: strings.Repeat("x", MaximumEventBytes+1), wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/internal/v1/openai/events", strings.NewReader(test.body))
			request.Header.Set("Authorization", test.authorization)
			request.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
	if store.calls != 0 {
		t.Fatalf("invalid requests reached store %d times", store.calls)
	}
}

func TestPrivateWebhookHandlerHidesStoreErrors(t *testing.T) {
	store := &acceptorStub{err: errors.New("database contains sensitive detail")}
	handler, err := NewHandler(store, strings.Repeat("c", 32), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/openai/events", strings.NewReader(`{
		"webhookId":"wh_123","eventId":"evt_123","eventType":"response.completed",
		"responseId":"resp_123","eventCreatedAt":"2026-08-29T12:00:00Z"
	}`))
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 32))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "sensitive") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}
