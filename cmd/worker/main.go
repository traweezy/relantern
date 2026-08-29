package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/extraction"
	extractionstore "github.com/traweezy/relantern/internal/extraction/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/openaiwebhook"
	"github.com/traweezy/relantern/internal/reembedding"
	"github.com/traweezy/relantern/internal/research"
	researchstore "github.com/traweezy/relantern/internal/research/pgstore"
	"github.com/traweezy/relantern/internal/scheduler"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
	"github.com/traweezy/relantern/internal/service"
	"github.com/traweezy/relantern/internal/storage/s3store"
	"github.com/traweezy/relantern/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		httpConfig, err := config.LoadHTTP(8081)
		if err != nil {
			return err
		}
		return service.CheckLocalHealth(httpConfig.Port, "/healthz")
	}
	if len(arguments) > 0 && (arguments[0] == "dead-letters" || arguments[0] == "retry-job") {
		return runJobOperation(arguments, logger)
	}
	common, err := config.LoadCommon()
	if err != nil {
		return fmt.Errorf("load common configuration: %w", err)
	}
	databaseConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	workerConfig, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker configuration: %w", err)
	}
	objectStorageConfig, err := config.LoadObjectStorage(common.Environment)
	if err != nil {
		return fmt.Errorf("load object-storage configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTP(8081)
	if err != nil {
		return fmt.Errorf("load HTTP configuration: %w", err)
	}
	embeddingSearchConfig, err := config.LoadEmbeddingSearch()
	if err != nil {
		return fmt.Errorf("load embedding and search configuration: %w", err)
	}
	openAIExtractionConfig, err := config.LoadOpenAIExtraction(common.Environment)
	if err != nil {
		return fmt.Errorf("load OpenAI extraction configuration: %w", err)
	}
	openAIResearchConfig, err := config.LoadOpenAIResearch(common.Environment)
	if err != nil {
		return fmt.Errorf("load OpenAI research configuration: %w", err)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(rootContext, databaseConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	rawStore, err := s3store.New(s3store.Config{
		Endpoint:  objectStorageConfig.Endpoint,
		Bucket:    objectStorageConfig.Bucket,
		Region:    objectStorageConfig.Region,
		AccessKey: objectStorageConfig.AccessKey,
		SecretKey: objectStorageConfig.SecretKey,
	})
	if err != nil {
		return fmt.Errorf("create raw object store: %w", err)
	}
	storageContext, cancelStorageCheck := context.WithTimeout(rootContext, 5*time.Second)
	defer cancelStorageCheck()
	if err := rawStore.Check(storageContext); err != nil {
		return fmt.Errorf("check raw object store: %w", err)
	}

	var reembedder *reembedding.Processor
	if embeddingSearchConfig.HybridEnabled {
		embeddingClient, clientError := embedding.NewClient(
			embeddingSearchConfig.BaseURL,
			embeddingSearchConfig.ModelID,
			embeddingSearchConfig.Dimensions,
			embeddingSearchConfig.RequestTimeout,
		)
		if clientError != nil {
			return fmt.Errorf("create embedding client: %w", clientError)
		}
		embeddingStore, storeError := embeddingstore.New(pool)
		if storeError != nil {
			return fmt.Errorf("create embedding store: %w", storeError)
		}
		searchStore, searchError := searchstore.New(
			pool,
			embeddingSearchConfig.Dimensions,
			embeddingSearchConfig.RRFK,
		)
		if searchError != nil {
			return fmt.Errorf("create search store: %w", searchError)
		}
		reembedder, err = reembedding.New(
			pool,
			rawStore,
			embeddingClient,
			embeddingStore,
			searchStore,
			embeddingSearchConfig.ModelID,
			embeddingSearchConfig.Dimensions,
		)
		if err != nil {
			return fmt.Errorf("create re-embedding processor: %w", err)
		}
	}

	var extractor *extraction.Processor
	if openAIExtractionConfig.Enabled {
		extractionClient, clientError := extraction.NewClient(extraction.ClientConfig{
			BaseURL:      openAIExtractionConfig.BaseURL,
			APIKey:       openAIExtractionConfig.APIKey,
			ProjectID:    openAIExtractionConfig.ProjectID,
			Organization: openAIExtractionConfig.OrganizationID,
			Timeout:      openAIExtractionConfig.RequestTimeout,
		})
		if clientError != nil {
			return fmt.Errorf("create structured extraction client: %w", clientError)
		}
		extractionStore, storeError := extractionstore.New(pool)
		if storeError != nil {
			return fmt.Errorf("create structured extraction store: %w", storeError)
		}
		extractor, err = extraction.NewProcessor(
			extractionStore,
			rawStore,
			extractionClient,
			common.Clock,
			extraction.Config{
				ExpectedModelID:         openAIExtractionConfig.ModelID,
				ExpectedReasoning:       openAIExtractionConfig.Reasoning,
				ExpectedVerbosity:       openAIExtractionConfig.Verbosity,
				ExpectedMaxOutputTokens: openAIExtractionConfig.MaxOutputTokens,
				MonthlySoftUSD:          openAIExtractionConfig.MonthlySoftUSD,
				MonthlyHardUSD:          openAIExtractionConfig.MonthlyHardUSD,
				MaximumAge:              openAIExtractionConfig.MaximumDocumentAge,
			},
		)
		if err != nil {
			return fmt.Errorf("create structured extraction processor: %w", err)
		}
	}

	inserter, err := jobqueue.NewInserter()
	if err != nil {
		return err
	}
	var researcher *research.Processor
	var openAIWebhookStore *openaiwebhook.Store
	if openAIResearchConfig.Enabled {
		researchClient, clientError := research.NewClient(research.ClientConfig{
			BaseURL:      openAIResearchConfig.BaseURL,
			APIKey:       openAIResearchConfig.APIKey,
			ProjectID:    openAIResearchConfig.ProjectID,
			Organization: openAIResearchConfig.OrganizationID,
			Timeout:      openAIResearchConfig.RequestTimeout,
		})
		if clientError != nil {
			return fmt.Errorf("create research client: %w", clientError)
		}
		researchStore, storeError := researchstore.New(pool)
		if storeError != nil {
			return fmt.Errorf("create research store: %w", storeError)
		}
		researcher, err = research.NewProcessor(researchStore, researchClient, common.Clock, research.Config{
			ExpectedModelID:         openAIResearchConfig.ModelID,
			ExpectedReasoning:       openAIResearchConfig.Reasoning,
			ExpectedVerbosity:       openAIResearchConfig.Verbosity,
			ExpectedMaxOutputTokens: openAIResearchConfig.MaxOutputTokens,
			MaximumToolCalls:        openAIResearchConfig.MaxToolCalls,
			AllowedDomains:          openAIResearchConfig.AllowedDomains,
			BlockedDomains:          openAIResearchConfig.BlockedDomains,
			Background:              openAIResearchConfig.Background,
			DailyWebSearchLimit:     openAIResearchConfig.DailyWebSearchLimit,
			MonthlySoftUSD:          openAIResearchConfig.MonthlySoftUSD,
			MonthlyHardUSD:          openAIResearchConfig.MonthlyHardUSD,
		})
		if err != nil {
			return fmt.Errorf("create research processor: %w", err)
		}
		openAIWebhookStore, err = openaiwebhook.NewStore(pool, inserter)
		if err != nil {
			return fmt.Errorf("create OpenAI webhook store: %w", err)
		}
	}
	reconciler := scheduler.NewReconciler(
		pool,
		common.Clock,
		"relantern:schedule-reconciler:v1",
		inserter,
	)
	processor := worker.NewProcessor(pool, workerConfig.DeliveryURL, workerConfig.RequestTimeout)
	health := worker.NewSchedulerHealth(pool, clock.System{}, workerConfig.ReconcileInterval)
	riverOptions := make([]worker.RiverOption, 0, 1)
	if researcher != nil {
		riverOptions = append(riverOptions, worker.WithResearchWorkers(
			researcher,
			openAIWebhookStore,
			openAIResearchConfig.RequestTimeout,
		))
	}
	riverClient, err := worker.NewRiverClient(
		pool,
		common.Clock,
		workerConfig.ReconcileInterval,
		httpConfig.ShutdownTimeout,
		!(len(arguments) > 0 && arguments[0] == "once"),
		logger,
		health,
		reconciler,
		processor,
		reembedder,
		extractor,
		openAIExtractionConfig.RequestTimeout,
		riverOptions...,
	)
	if err != nil {
		return err
	}
	runner := worker.NewRunner(riverClient, pool, health, httpConfig.ShutdownTimeout)
	if len(arguments) > 0 && arguments[0] == "once" {
		return runner.Once(rootContext, logger)
	}
	return runner.Run(rootContext, logger, httpConfig.Port)
}
