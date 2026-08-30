package main

import (
	"context"
	"errors"
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
	"github.com/traweezy/relantern/internal/dedupe"
	dedupestore "github.com/traweezy/relantern/internal/dedupe/pgstore"
	deliveryclient "github.com/traweezy/relantern/internal/delivery"
	digeststore "github.com/traweezy/relantern/internal/digest/pgstore"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/extraction"
	extractionstore "github.com/traweezy/relantern/internal/extraction/pgstore"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/manualcapture"
	"github.com/traweezy/relantern/internal/openaiwebhook"
	"github.com/traweezy/relantern/internal/parsing"
	parsingstore "github.com/traweezy/relantern/internal/parsing/pgstore"
	radarstore "github.com/traweezy/relantern/internal/radar/pgstore"
	readingstatestore "github.com/traweezy/relantern/internal/readingstate/pgstore"
	"github.com/traweezy/relantern/internal/reembedding"
	"github.com/traweezy/relantern/internal/research"
	researchstore "github.com/traweezy/relantern/internal/research/pgstore"
	"github.com/traweezy/relantern/internal/retention"
	retentionstore "github.com/traweezy/relantern/internal/retention/pgstore"
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
	if len(arguments) > 0 && (arguments[0] == "digest-preview" || arguments[0] == "digest-run") {
		return runDigestOperation(arguments)
	}
	if len(arguments) > 0 && arguments[0] == "retention-run" {
		return runRetentionOperation(arguments)
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
	deliveryConfig, err := config.LoadDelivery(common.Environment)
	if err != nil {
		return fmt.Errorf("load delivery configuration: %w", err)
	}
	objectStorageConfig, err := config.LoadObjectStorage(common.Environment)
	if err != nil {
		return fmt.Errorf("load object-storage configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTP(8081)
	if err != nil {
		return fmt.Errorf("load HTTP configuration: %w", err)
	}
	embeddingSearchConfig, err := config.LoadEmbeddingSearch(common.Environment)
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
	dedupeConfig, err := config.LoadDedupe()
	if err != nil {
		return fmt.Errorf("load dedupe configuration: %w", err)
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
	var embeddingClient *embedding.Client
	if embeddingSearchConfig.HybridEnabled {
		var clientError error
		embeddingClient, clientError = embedding.NewClient(embedding.ClientConfig{
			BaseURL:      embeddingSearchConfig.BaseURL,
			APIKey:       embeddingSearchConfig.APIKey,
			ProjectID:    embeddingSearchConfig.ProjectID,
			Organization: embeddingSearchConfig.OrganizationID,
			ModelID:      embeddingSearchConfig.ModelID,
			Dimensions:   embeddingSearchConfig.Dimensions,
			Timeout:      embeddingSearchConfig.RequestTimeout,
			Hosted:       embeddingSearchConfig.Hosted,
		})
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
	digestStore, err := digeststore.New(pool, inserter)
	if err != nil {
		return fmt.Errorf("create digest store: %w", err)
	}
	digestSender, err := deliveryclient.New(deliveryclient.Config{
		Mode: deliveryConfig.Mode, CaptureURL: deliveryConfig.CaptureURL,
		DiscordURL: deliveryConfig.DiscordWebhookURL, ResendURL: deliveryConfig.ResendAPIURL,
		ResendAPIKey: deliveryConfig.ResendAPIKey, EmailFrom: deliveryConfig.EmailFrom,
		EmailTo: deliveryConfig.EmailTo, PublicBaseURL: deliveryConfig.PublicBaseURL,
		RequestTimeout: deliveryConfig.RequestTimeout,
	})
	if err != nil {
		return fmt.Errorf("create digest delivery client: %w", err)
	}
	var manualCaptureProcessor *manualcapture.Processor
	if embeddingClient != nil {
		dedupeStore, storeError := dedupestore.New(pool, common.Clock, dedupe.Config{
			SimHashDistance:     dedupeConfig.SimHashDistance,
			EmbeddingSimilarity: dedupeConfig.EmbeddingSimilarity,
			ClusterMaxAge:       dedupeConfig.ClusterMaxAge,
		})
		if storeError != nil {
			return fmt.Errorf("create manual-capture dedupe store: %w", storeError)
		}
		revisionStore, storeError := parsingstore.New(pool)
		if storeError != nil {
			return fmt.Errorf("create manual-capture parsing store: %w", storeError)
		}
		parsingProcessor, processorError := parsing.NewProcessor(rawStore, revisionStore, common.Clock)
		if processorError != nil {
			return fmt.Errorf("create manual-capture parser: %w", processorError)
		}
		limiter, limiterError := fetcher.NewHostLimiter(12, 2, time.Second)
		if limiterError != nil {
			return fmt.Errorf("create manual-capture host limiter: %w", limiterError)
		}
		manualCaptureProcessor, err = manualcapture.New(
			pool,
			common.Clock,
			rawStore,
			embeddingClient,
			dedupeStore,
			parsingProcessor,
			inserter,
			limiter,
			embeddingSearchConfig.ModelID,
			common.Environment == config.EnvironmentLocal || common.Environment == config.EnvironmentTest,
		)
		if err != nil {
			return fmt.Errorf("create manual-capture processor: %w", err)
		}
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
	health := worker.NewSchedulerHealth(pool, clock.System{}, workerConfig.ReconcileInterval)
	readingStateStore, err := readingstatestore.New(
		pool,
		readingstatestore.WithClock(common.Clock.Now),
	)
	if err != nil {
		return fmt.Errorf("create reading-state worker store: %w", err)
	}
	radarProcessor, err := radarstore.New(pool, inserter)
	if err != nil {
		return fmt.Errorf("create Radar worker store: %w", err)
	}
	retentionRepository, err := retentionstore.New(pool)
	if err != nil {
		return fmt.Errorf("create retention store: %w", err)
	}
	retentionRunner, err := retention.NewRunner(retentionRepository, rawStore, retention.DefaultPolicy())
	if err != nil {
		return fmt.Errorf("create retention runner: %w", err)
	}
	processor := worker.NewProcessor(
		pool,
		deliveryConfig.CaptureURL,
		workerConfig.RequestTimeout,
		worker.WithDigestScheduleHandoff(digestStore),
		worker.WithRadarScheduleHandoff(radarProcessor),
	)
	riverOptions := []worker.RiverOption{
		worker.WithSnoozeReturner(readingStateStore),
		worker.WithRadarProcessor(radarProcessor),
		worker.WithDigestProcessor(digestStore, digestSender),
		worker.WithRetentionRunner(retentionRunner),
	}
	if manualCaptureProcessor != nil {
		riverOptions = append(riverOptions, worker.WithManualCaptureProcessor(manualCaptureProcessor))
	}
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
		if len(arguments) == 3 && arguments[1] == "--occurrence-id" {
			return runner.OnceOccurrence(rootContext, logger, arguments[2])
		}
		if len(arguments) != 1 {
			return errors.New("usage: worker once [--occurrence-id UUID]")
		}
		return runner.Once(rootContext, logger)
	}
	return runner.Run(rootContext, logger, httpConfig.Port)
}
