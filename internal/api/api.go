package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/traweezy/relantern/internal/discovery"
	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/intelligence"
	"github.com/traweezy/relantern/internal/readingstate"
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

type TodayOutput struct {
	Body intelligence.TodaySnapshot
}

type LiveOutput struct {
	Body intelligence.LiveSnapshot
}

type StoryInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
}

type InternalInput struct {
	Authorization string `header:"Authorization"`
}

type StoryOutput struct {
	Body intelligence.StoryDetail
}

type Application struct {
	Handler http.Handler
	API     huma.API
}

type options struct {
	clock         func() time.Time
	intelligence  intelligence.Reader
	readingState  readingstate.Repository
	discovery     *discovery.Service
	serviceToken  string
	openAIWebhook http.Handler
}

type Option func(*options)

func WithOpenAIWebhook(handler http.Handler) Option {
	return func(configuration *options) {
		configuration.openAIWebhook = handler
	}
}

func WithIntelligence(reader intelligence.Reader, serviceToken string) Option {
	return func(configuration *options) {
		configuration.intelligence = reader
		configuration.serviceToken = serviceToken
	}
}

func WithReadingState(repository readingstate.Repository, serviceToken string) Option {
	return func(configuration *options) {
		configuration.readingState = repository
		configuration.serviceToken = serviceToken
	}
}

func WithDiscovery(service *discovery.Service, serviceToken string) Option {
	return func(configuration *options) {
		configuration.discovery = service
		configuration.serviceToken = serviceToken
	}
}

func WithClock(clock func() time.Time) Option {
	return func(configuration *options) {
		configuration.clock = clock
	}
}

func New(logger *slog.Logger, info Info, ready ReadyCheck, configuredOptions ...Option) Application {
	configuration := options{clock: time.Now}
	for _, configure := range configuredOptions {
		configure(&configuration)
	}
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
	if configuration.openAIWebhook != nil {
		router.Method(http.MethodPost, "/internal/v1/openai/events", configuration.openAIWebhook)
	}

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
	registerIntelligence(api, configuration, logger)
	registerReadingState(api, configuration, logger)
	registerDiscovery(api, configuration, logger)

	return Application{Handler: router, API: api}
}

func registerIntelligence(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "get-today",
		Method:      http.MethodGet,
		Path:        "/api/v1/today",
		Summary:     "Get the owner daily intelligence snapshot",
		Tags:        []string{"intelligence"},
	}, func(ctx context.Context, input *InternalInput) (*TodayOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.intelligence != nil); err != nil {
			return nil, err
		}
		snapshot, err := configuration.intelligence.Today(ctx, configuration.clock().UTC())
		if err != nil {
			logger.ErrorContext(ctx, "Today intelligence read failed", "error", err)
			return nil, huma.Error500InternalServerError("The private intelligence snapshot is unavailable.")
		}
		return &TodayOutput{Body: snapshot}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-live-intelligence",
		Method:      http.MethodGet,
		Path:        "/api/v1/live",
		Summary:     "Get the current owner intelligence stream snapshot",
		Tags:        []string{"intelligence"},
	}, func(ctx context.Context, input *InternalInput) (*LiveOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.intelligence != nil); err != nil {
			return nil, err
		}
		snapshot, err := configuration.intelligence.Live(ctx, configuration.clock().UTC())
		if err != nil {
			logger.ErrorContext(ctx, "live intelligence read failed", "error", err)
			return nil, huma.Error500InternalServerError("The private intelligence stream is unavailable.")
		}
		return &LiveOutput{Body: snapshot}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-story",
		Method:      http.MethodGet,
		Path:        "/api/v1/stories/{storyId}",
		Summary:     "Get an evidence-backed owner intelligence story",
		Tags:        []string{"intelligence"},
	}, func(ctx context.Context, input *StoryInput) (*StoryOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.intelligence != nil); err != nil {
			return nil, err
		}
		story, err := configuration.intelligence.Story(ctx, input.StoryID)
		if errors.Is(err, intelligence.ErrStoryNotFound) {
			return nil, huma.Error404NotFound("The requested intelligence story does not exist.")
		}
		if err != nil {
			logger.ErrorContext(ctx, "intelligence story read failed", "error", err)
			return nil, huma.Error500InternalServerError("The private intelligence story is unavailable.")
		}
		return &StoryOutput{Body: story}, nil
	})
}

func authorizeInternal(authorization string, configuration options, available bool) error {
	if !available || len(configuration.serviceToken) < 32 {
		return huma.Error503ServiceUnavailable("The private intelligence service is not configured.")
	}
	expected := "Bearer " + configuration.serviceToken
	provided := strings.TrimSpace(authorization)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return huma.Error401Unauthorized("A valid private service credential is required.")
	}
	return nil
}
