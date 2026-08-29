package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/traweezy/relantern/internal/httpx"
)

type ReadyCheck func(context.Context) error

type Info struct {
	Version string
	GitSHA  string
}

type HealthBody struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
	GitSHA  string `json:"gitSha"`
}

type HealthOutput struct {
	Body HealthBody
}

type Application struct {
	Handler http.Handler
	API     huma.API
}

func New(logger *slog.Logger, info Info, ready ReadyCheck) Application {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(httpx.SecurityHeaders)
	router.Use(httpx.Recovery(logger))
	router.Use(httpx.AccessLog(logger))
	router.Use(middleware.Timeout(15 * time.Second))

	router.Get("/healthz", func(response http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(response, http.StatusOK, HealthBody{
			Service: "api",
			Status:  "ok",
			Version: info.Version,
			GitSHA:  info.GitSHA,
		})
	})
	router.Get("/readyz", func(response http.ResponseWriter, request *http.Request) {
		if err := ready(request.Context()); err != nil {
			logger.WarnContext(request.Context(), "readiness check failed", "error", err)
			httpx.WriteProblem(
				response,
				request,
				http.StatusServiceUnavailable,
				"Service Unavailable",
				"A required private dependency is unavailable.",
			)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"status": "ready"})
	})

	humaConfig := huma.DefaultConfig("Relantern API", info.Version)
	humaConfig.DocsPath = ""
	humaConfig.OpenAPIPath = ""
	humaConfig.SchemasPath = ""
	api := humachi.New(router, humaConfig)
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/api/v1/healthz",
		Summary:     "Get API process health",
		Tags:        []string{"operations"},
	}, func(_ context.Context, _ *struct{}) (*HealthOutput, error) {
		return &HealthOutput{Body: HealthBody{
			Service: "api",
			Status:  "ok",
			Version: info.Version,
			GitSHA:  info.GitSHA,
		}}, nil
	})

	return Application{Handler: router, API: api}
}
