package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/intelligence"
)

const (
	liveHeartbeatInterval = 15 * time.Second
	liveStreamLifetime    = 5 * time.Minute
	liveReplayLimit       = 100
	liveStreamLimit       = 4
)

func liveStreamHandler(configuration options, logger *slog.Logger) http.HandlerFunc {
	streamSlots := make(chan struct{}, liveStreamLimit)
	return func(response http.ResponseWriter, request *http.Request) {
		if configuration.intelligence == nil || len(configuration.serviceToken) < 32 {
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is not configured.")
			return
		}
		expected := "Bearer " + configuration.serviceToken
		provided := strings.TrimSpace(request.Header.Get("Authorization"))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			httpx.WriteProblem(response, request, http.StatusUnauthorized, "Unauthorized", "A valid private service credential is required.")
			return
		}
		replay, ok := configuration.intelligence.(intelligence.LiveReplay)
		if !ok {
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is not configured.")
			return
		}
		cursor, supplied, err := parseLiveCursor(request)
		if err != nil {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Bad Request", "The live event cursor is invalid.")
			return
		}
		select {
		case streamSlots <- struct{}{}:
			defer func() { <-streamSlots }()
		default:
			response.Header().Set("Retry-After", "5")
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is at capacity. Retry shortly.")
			return
		}
		streamContext, stop := context.WithTimeout(request.Context(), liveStreamLifetime)
		defer stop()
		subscription, err := replay.Subscribe(streamContext)
		if err != nil {
			logger.ErrorContext(streamContext, "live notification subscription failed", "error", err)
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is unavailable.")
			return
		}
		defer subscription.Close()
		if !supplied {
			cursor, err = replay.LatestCursor(streamContext, configuration.clock().UTC())
			if err != nil {
				logger.ErrorContext(streamContext, "live cursor read failed", "error", err)
				httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is unavailable.")
				return
			}
		}
		batch, err := replay.Replay(streamContext, cursor, configuration.clock().UTC(), liveReplayLimit)
		if err != nil {
			logger.ErrorContext(streamContext, "live replay read failed", "error", err)
			httpx.WriteProblem(response, request, http.StatusServiceUnavailable, "Service Unavailable", "The private live stream is unavailable.")
			return
		}
		response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		response.Header().Set("Cache-Control", "no-cache, no-transform")
		response.Header().Set("Connection", "keep-alive")
		response.Header().Set("X-Accel-Buffering", "no")
		response.WriteHeader(http.StatusOK)
		if _, err := response.Write([]byte(": connected\n\n")); err != nil {
			return
		}
		if err := http.NewResponseController(response).Flush(); err != nil {
			return
		}
		for {
			if batch.ResetRequired {
				_, _ = response.Write([]byte("event: reset_required\ndata: {\"reason\":\"cursor_expired\"}\n\n"))
				_ = http.NewResponseController(response).Flush()
				return
			}
			for _, event := range batch.Events {
				if err := writeLiveEvent(response, event); err != nil {
					return
				}
				cursor = event.Cursor
			}
			if len(batch.Events) > 0 {
				if err := http.NewResponseController(response).Flush(); err != nil {
					return
				}
			}
			if batch.HasMore {
				batch, err = replay.Replay(streamContext, cursor, configuration.clock().UTC(), liveReplayLimit)
			} else {
				waitContext, cancel := context.WithTimeout(streamContext, liveHeartbeatInterval)
				waitErr := subscription.Wait(waitContext)
				cancel()
				if streamContext.Err() != nil {
					return
				}
				if errors.Is(waitErr, context.DeadlineExceeded) {
					if _, writeErr := response.Write([]byte(": heartbeat\n\n")); writeErr != nil {
						return
					}
					if flushErr := http.NewResponseController(response).Flush(); flushErr != nil {
						return
					}
				} else if waitErr != nil {
					logger.WarnContext(streamContext, "live notification wait failed", "error", waitErr)
					return
				}
				batch, err = replay.Replay(streamContext, cursor, configuration.clock().UTC(), liveReplayLimit)
			}
			if err != nil {
				logger.WarnContext(streamContext, "live replay refresh failed", "error", err)
				return
			}
		}
	}
}

func parseLiveCursor(request *http.Request) (int64, bool, error) {
	raw := request.URL.Query().Get("after")
	if raw == "" {
		raw = request.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return 0, false, nil
	}
	if len(raw) > 19 || (raw != "0" && strings.HasPrefix(raw, "0")) {
		return 0, false, errors.New("cursor is not canonical decimal")
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, false, errors.New("cursor contains a non-decimal digit")
		}
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	return cursor, true, err
}

func writeLiveEvent(response http.ResponseWriter, event intelligence.ReplayEvent) error {
	encoded, err := json.Marshal(event.Event)
	if err != nil {
		return fmt.Errorf("encode live event: %w", err)
	}
	_, err = fmt.Fprintf(response, "id: %d\nevent: %s\ndata: %s\n\n", event.Cursor, event.Event.Type, encoded)
	return err
}
