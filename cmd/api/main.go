package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/discovery"
	discoverystore "github.com/traweezy/relantern/internal/discovery/pgstore"
	"github.com/traweezy/relantern/internal/embedding"
	intelligencepgstore "github.com/traweezy/relantern/internal/intelligence/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/openaiwebhook"
	readingstatepgstore "github.com/traweezy/relantern/internal/readingstate/pgstore"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
	"github.com/traweezy/relantern/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	if len(arguments) > 0 {
		switch arguments[0] {
		case "openapi":
			return writeOpenAPI(arguments[1:], logger)
		case "healthcheck":
			return healthcheck()
		}
	}

	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTP(8080)
	if err != nil {
		return fmt.Errorf("load HTTP configuration: %w", err)
	}
	webhookConfig, err := config.LoadOpenAIWebhookInternal()
	if err != nil {
		return fmt.Errorf("load private OpenAI webhook configuration: %w", err)
	}
	embeddingSearchConfig, err := config.LoadEmbeddingSearch()
	if err != nil {
		return fmt.Errorf("load embedding and search configuration: %w", err)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(rootContext, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()

	inserter, err := jobqueue.NewInserter()
	if err != nil {
		return err
	}
	webhookStore, err := openaiwebhook.NewStore(pool, inserter)
	if err != nil {
		return err
	}
	webhookHandler, err := openaiwebhook.NewHandler(webhookStore, webhookConfig.ServiceToken, logger)
	if err != nil {
		return err
	}
	intelligenceStore, err := intelligencepgstore.New(pool)
	if err != nil {
		return err
	}
	readingStateStore, err := readingstatepgstore.New(pool)
	if err != nil {
		return err
	}
	embeddingClient, err := embedding.NewClient(
		embeddingSearchConfig.BaseURL,
		embeddingSearchConfig.ModelID,
		embeddingSearchConfig.Dimensions,
		embeddingSearchConfig.RequestTimeout,
	)
	if err != nil {
		return fmt.Errorf("create discovery embedding client: %w", err)
	}
	searchIndex, err := searchstore.New(pool, embeddingSearchConfig.Dimensions, embeddingSearchConfig.RRFK)
	if err != nil {
		return fmt.Errorf("create discovery search store: %w", err)
	}
	discoveryStore, err := discoverystore.New(
		pool,
		searchIndex,
		inserter,
		common.Environment == config.EnvironmentLocal || common.Environment == config.EnvironmentTest,
	)
	if err != nil {
		return fmt.Errorf("create discovery store: %w", err)
	}
	discoveryService, err := discovery.NewService(discoveryStore, embeddingClient)
	if err != nil {
		return fmt.Errorf("create discovery service: %w", err)
	}
	application := api.New(logger, api.Info{Version: common.Version, GitSHA: common.GitSHA}, func(ctx context.Context) error {
		pingContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return pool.Ping(pingContext)
	},
		api.WithIntelligence(intelligenceStore, webhookConfig.ServiceToken),
		api.WithReadingState(readingStateStore, webhookConfig.ServiceToken),
		api.WithDiscovery(discoveryService, webhookConfig.ServiceToken),
		api.WithOpenAIWebhook(webhookHandler),
	)

	return service.RunHTTP(rootContext, logger, service.HTTPServerConfig{
		Host:            "0.0.0.0",
		Port:            httpConfig.Port,
		Handler:         application.Handler,
		ShutdownTimeout: httpConfig.ShutdownTimeout,
	})
}

func writeOpenAPI(arguments []string, logger *slog.Logger) error {
	flags := flag.NewFlagSet("openapi", flag.ContinueOnError)
	output := flags.String("output", "contracts/openapi.yaml", "OpenAPI output path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	application := api.New(logger, api.Info{Version: "0.0.0"}, func(context.Context) error { return nil })
	document, err := application.API.OpenAPI().YAML()
	if err != nil {
		return fmt.Errorf("encode OpenAPI: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		return fmt.Errorf("create OpenAPI directory: %w", err)
	}
	if err := os.WriteFile(*output, document, 0o644); err != nil {
		return fmt.Errorf("write OpenAPI: %w", err)
	}
	return nil
}

func healthcheck() error {
	httpConfig, err := config.LoadHTTP(8080)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", httpConfig.Port))
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("health endpoint returned a non-200 response")
	}
	return nil
}
