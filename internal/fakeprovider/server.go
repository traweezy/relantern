package fakeprovider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/traweezy/relantern/internal/httpx"
)

type Kind string

const (
	KindDelivery Kind = "delivery"
	KindOpenAI   Kind = "openai"
	KindSource   Kind = "source"
)

type captureStore struct {
	mu       sync.RWMutex
	keys     map[string]struct{}
	captures []Capture
}

func New(kind Kind, logger *slog.Logger) (http.Handler, error) {
	if kind != KindDelivery && kind != KindOpenAI && kind != KindSource {
		return nil, fmt.Errorf("unsupported fake provider kind %q", kind)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"provider": string(kind), "status": "ok"})
	})

	switch kind {
	case KindDelivery:
		registerDelivery(mux)
	case KindOpenAI:
		registerOpenAI(mux)
	case KindSource:
		registerSource(mux)
	}

	return httpx.SecurityHeaders(httpx.Recovery(logger)(httpx.AccessLog(logger)(mux))), nil
}

func registerDelivery(mux *http.ServeMux) {
	store := &captureStore{keys: make(map[string]struct{})}
	mux.HandleFunc("POST /capture", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload Capture
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload.IdempotencyKey == "" || payload.LocalDate == "" || payload.Message == "" || payload.ScheduledFor.IsZero() {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid capture", "The capture body must be valid JSON.")
			return
		}
		store.mu.Lock()
		if _, exists := store.keys[payload.IdempotencyKey]; !exists {
			store.keys[payload.IdempotencyKey] = struct{}{}
			store.captures = append(store.captures, payload)
		}
		store.mu.Unlock()
		httpx.WriteJSON(response, http.StatusAccepted, map[string]string{"status": "captured"})
	})
	mux.HandleFunc("GET /captures", func(response http.ResponseWriter, _ *http.Request) {
		store.mu.RLock()
		captures := append([]Capture{}, store.captures...)
		store.mu.RUnlock()
		httpx.WriteJSON(response, http.StatusOK, map[string]any{"count": len(captures), "captures": captures})
	})
}

func registerOpenAI(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/responses", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
		var ignored map[string]any
		if err := decoder.Decode(&ignored); err != nil {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid request", "The request body must be valid JSON.")
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]any{
			"id":     "resp_fake_pr0",
			"object": "response",
			"status": "completed",
			"output": []any{},
		})
	})
}

func registerSource(mux *http.ServeMux) {
	mux.HandleFunc("GET /feed.xml", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		response.Header().Set("ETag", `"pr0-source-v1"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Relantern fake source</title><id>urn:relantern:fake-source</id><updated>2026-08-29T00:00:00Z</updated><entry><title>Foundation fixture</title><id>urn:relantern:fixture:foundation</id><updated>2026-08-29T00:00:00Z</updated><content>Static evidence fixture. Ingestion is disabled in PR 0.</content></entry></feed>`))
	})
	mux.HandleFunc("GET /robots.txt", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("User-agent: *\nAllow: /\nCrawl-delay: 1\n"))
	})
}

type Capture struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	LocalDate      string    `json:"localDate"`
	Message        string    `json:"message"`
	ScheduledFor   time.Time `json:"scheduledFor"`
}
