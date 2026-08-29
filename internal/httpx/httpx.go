package httpx

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const problemContentType = "application/problem+json"

type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
	TraceID  string `json:"traceId,omitempty"`
}

func WriteJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		slog.Error("encode JSON response", "error", err)
	}
}

func WriteProblem(response http.ResponseWriter, request *http.Request, status int, title string, detail string) {
	response.Header().Set("Content-Type", problemContentType)
	response.WriteHeader(status)
	problem := Problem{
		Type:     fmt.Sprintf("https://relantern.invalid/problems/http-%d", status),
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: request.URL.Path,
		TraceID:  middleware.GetReqID(request.Context()),
	}
	if err := json.NewEncoder(response).Encode(problem); err != nil {
		slog.Error("encode problem response", "error", err)
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.ErrorContext(
						request.Context(),
						"panic recovered",
						"panic",
						recovered,
						"stack",
						string(debug.Stack()),
					)
					WriteProblem(response, request, http.StatusInternalServerError, "Internal Server Error", "The request could not be completed.")
				}
			}()
			next.ServeHTTP(response, request)
		})
	}
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			startedAt := time.Now()
			wrapped := middleware.NewWrapResponseWriter(response, request.ProtoMajor)
			next.ServeHTTP(wrapped, request)
			logger.InfoContext(
				request.Context(),
				"http request",
				"method",
				request.Method,
				"path",
				request.URL.Path,
				"status",
				wrapped.Status(),
				"bytes",
				wrapped.BytesWritten(),
				"duration_ms",
				time.Since(startedAt).Milliseconds(),
				"request_id",
				middleware.GetReqID(request.Context()),
			)
		})
	}
}
