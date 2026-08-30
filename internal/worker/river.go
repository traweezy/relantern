package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/openaiwebhook"
	"github.com/traweezy/relantern/internal/reembedding"
	"github.com/traweezy/relantern/internal/research"
	"github.com/traweezy/relantern/internal/scheduler"
)

type reconcileSchedulesWorker struct {
	river.WorkerDefaults[jobqueue.ReconcileSchedulesArgs]
	health     *SchedulerHealth
	logger     *slog.Logger
	reconciler *scheduler.Reconciler
}

func (worker *reconcileSchedulesWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ReconcileSchedulesArgs],
) error {
	result, err := worker.reconciler.Reconcile(ctx, job.Args.RunID)
	if err != nil {
		return err
	}
	worker.health.RecordSuccess()
	worker.logger.InfoContext(
		ctx,
		"schedule reconciliation complete",
		"job_id", job.ID,
		"run_id", job.Args.RunID,
		"lock_acquired", result.LockAcquired,
		"occurrences_created", result.OccurrencesCreated,
		"jobs_enqueued", result.JobsEnqueued,
	)
	return nil
}

type scheduleOccurrenceWorker struct {
	river.WorkerDefaults[jobqueue.ScheduleOccurrenceArgs]
	processor *Processor
}

type reembedEntityWorker struct {
	river.WorkerDefaults[jobqueue.ReembedEntityArgs]
	processor *reembedding.Processor
}

type extractItemWorker struct {
	river.WorkerDefaults[jobqueue.ExtractItemArgs]
	processor *extraction.Processor
	timeout   time.Duration
}

type researchStoryWorker struct {
	river.WorkerDefaults[jobqueue.ResearchStoryArgs]
	processor *research.Processor
	timeout   time.Duration
}

func (worker *researchStoryWorker) Timeout(*river.Job[jobqueue.ResearchStoryArgs]) time.Duration {
	return worker.timeout
}

func (worker *researchStoryWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ResearchStoryArgs],
) error {
	_, err := worker.processor.Process(ctx, research.ProcessRequest{ClusterID: job.Args.ClusterID})
	if research.Permanent(err) {
		return river.JobCancel(err)
	}
	return err
}

type pollOpenAIBackgroundWorker struct {
	river.WorkerDefaults[jobqueue.PollOpenAIBackgroundArgs]
	clock     clock.Clock
	processor *research.Processor
	store     *openaiwebhook.Store
	timeout   time.Duration
}

func (worker *pollOpenAIBackgroundWorker) Timeout(*river.Job[jobqueue.PollOpenAIBackgroundArgs]) time.Duration {
	return worker.timeout
}

func (worker *pollOpenAIBackgroundWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.PollOpenAIBackgroundArgs],
) error {
	result, err := worker.processor.Poll(ctx, research.PollRequest{
		RunID: job.Args.RunID, ResponseID: job.Args.ResponseID,
	})
	if err != nil {
		if research.Permanent(err) {
			if markError := worker.store.MarkProcessed(
				ctx,
				job.Args.WebhookID,
				job.Args.ResponseID,
				err.Error(),
				worker.clock.Now().UTC(),
			); markError != nil {
				err = errors.Join(err, markError)
			}
			return river.JobCancel(err)
		}
		return err
	}
	if result.Pending {
		if result.ProviderID != job.Args.ResponseID {
			return worker.store.MarkProcessed(
				ctx,
				job.Args.WebhookID,
				job.Args.ResponseID,
				"",
				worker.clock.Now().UTC(),
			)
		}
		return river.JobSnooze(time.Minute)
	}
	return worker.store.MarkProcessed(
		ctx,
		job.Args.WebhookID,
		job.Args.ResponseID,
		"",
		worker.clock.Now().UTC(),
	)
}

type reconcileOpenAIBackgroundWorker struct {
	river.WorkerDefaults[jobqueue.ReconcileOpenAIBackgroundArgs]
	logger *slog.Logger
	store  *openaiwebhook.Store
}

func (worker *reconcileOpenAIBackgroundWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ReconcileOpenAIBackgroundArgs],
) error {
	count, err := worker.store.ReconcilePending(ctx, 500)
	if err != nil {
		return err
	}
	worker.logger.InfoContext(ctx, "OpenAI background reconciliation complete", "job_id", job.ID, "jobs_enqueued", count)
	return nil
}

func (worker *extractItemWorker) Timeout(*river.Job[jobqueue.ExtractItemArgs]) time.Duration {
	return worker.timeout
}

func (worker *extractItemWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ExtractItemArgs],
) error {
	_, err := worker.processor.Process(ctx, extraction.ProcessRequest{
		ItemID:     job.Args.ItemID,
		RevisionID: job.Args.RevisionID,
	})
	if extraction.Permanent(err) {
		return river.JobCancel(err)
	}
	return err
}

func (worker *reembedEntityWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ReembedEntityArgs],
) error {
	_, err := worker.processor.Process(ctx, reembedding.Request{
		EntityType: job.Args.EntityType,
		EntityID:   job.Args.EntityID,
		RevisionID: job.Args.RevisionID,
		ModelID:    job.Args.ModelID,
	})
	if errors.Is(err, reembedding.ErrInvalidTarget) ||
		errors.Is(err, reembedding.ErrModelMismatch) ||
		errors.Is(err, reembedding.ErrRevisionIntegrity) {
		return river.JobCancel(err)
	}
	return err
}

func (worker *scheduleOccurrenceWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ScheduleOccurrenceArgs],
) error {
	err := worker.processor.Process(ctx, job.Args.OccurrenceID)
	if jobqueue.IsPermanent(err) {
		return river.JobCancel(err)
	}
	return err
}

type errorHandler struct {
	logger *slog.Logger
}

func (handler *errorHandler) HandleError(
	ctx context.Context,
	job *rivertype.JobRow,
	err error,
) *river.ErrorHandlerResult {
	handler.logger.ErrorContext(
		ctx,
		"River job failed",
		"job_id", job.ID,
		"kind", job.Kind,
		"queue", job.Queue,
		"attempt", job.Attempt,
		"max_attempts", job.MaxAttempts,
		"error", err,
	)
	return nil
}

func (handler *errorHandler) HandlePanic(
	ctx context.Context,
	job *rivertype.JobRow,
	panicValue any,
	trace string,
) *river.ErrorHandlerResult {
	handler.logger.ErrorContext(
		ctx,
		"River job panicked",
		"job_id", job.ID,
		"kind", job.Kind,
		"queue", job.Queue,
		"panic", fmt.Sprint(panicValue),
		"trace", trace,
	)
	return nil
}

func NewRiverClient(
	pool *pgxpool.Pool,
	configuredClock clock.Clock,
	interval time.Duration,
	shutdownTimeout time.Duration,
	enablePeriodicJobs bool,
	logger *slog.Logger,
	health *SchedulerHealth,
	reconciler *scheduler.Reconciler,
	processor *Processor,
	reembedder *reembedding.Processor,
	extractor *extraction.Processor,
	extractionTimeout time.Duration,
	configuredOptions ...RiverOption,
) (*river.Client[pgx.Tx], error) {
	if extractionTimeout <= 0 || extractionTimeout > 10*time.Minute {
		return nil, errors.New("structured extraction timeout must be positive and at most 10 minutes")
	}
	configuration := riverOptions{}
	for _, configure := range configuredOptions {
		configure(&configuration)
	}
	if configuration.researcher != nil &&
		(configuration.openAIWebhookStore == nil || configuration.researchTimeout <= 0 || configuration.researchTimeout > 30*time.Minute) {
		return nil, errors.New("research workers require a webhook store and timeout of at most 30 minutes")
	}
	workers := river.NewWorkers()
	if err := river.AddWorkerSafely(
		workers,
		&reconcileSchedulesWorker{health: health, logger: logger, reconciler: reconciler},
	); err != nil {
		return nil, fmt.Errorf("register schedule reconciliation worker: %w", err)
	}
	if err := river.AddWorkerSafely(
		workers,
		&scheduleOccurrenceWorker{processor: processor},
	); err != nil {
		return nil, fmt.Errorf("register schedule occurrence worker: %w", err)
	}
	if reembedder != nil {
		if err := river.AddWorkerSafely(
			workers,
			&reembedEntityWorker{processor: reembedder},
		); err != nil {
			return nil, fmt.Errorf("register re-embedding worker: %w", err)
		}
	}
	if extractor != nil {
		if err := river.AddWorkerSafely(
			workers,
			&extractItemWorker{processor: extractor, timeout: extractionTimeout},
		); err != nil {
			return nil, fmt.Errorf("register structured extraction worker: %w", err)
		}
	}
	if configuration.researcher != nil {
		if err := river.AddWorkerSafely(workers, &researchStoryWorker{
			processor: configuration.researcher,
			timeout:   configuration.researchTimeout,
		}); err != nil {
			return nil, fmt.Errorf("register research worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &pollOpenAIBackgroundWorker{
			clock: configuredClock, processor: configuration.researcher,
			store: configuration.openAIWebhookStore, timeout: configuration.researchTimeout,
		}); err != nil {
			return nil, fmt.Errorf("register OpenAI background polling worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &reconcileOpenAIBackgroundWorker{
			logger: logger, store: configuration.openAIWebhookStore,
		}); err != nil {
			return nil, fmt.Errorf("register OpenAI background reconciliation worker: %w", err)
		}
	}

	var periodicJobs []*river.PeriodicJob
	if enablePeriodicJobs {
		periodicJobs = jobqueue.PeriodicJobs(interval, configuration.researcher != nil)
	}
	queues := jobqueue.QueueConfigs()
	if configuration.queueConfigs != nil {
		queues = configuration.queueConfigs
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		CancelledJobRetentionPeriod: 7 * 24 * time.Hour,
		CompletedJobRetentionPeriod: 24 * time.Hour,
		DiscardedJobRetentionPeriod: -1,
		ErrorHandler:                &errorHandler{logger: logger},
		JobTimeout:                  30 * time.Second,
		Logger:                      logger,
		MaxAttempts:                 5,
		PeriodicJobs:                periodicJobs,
		Queues:                      queues,
		RescueStuckJobsAfter:        2 * time.Minute,
		RetryPolicy: jobqueue.NewExponentialRetryPolicy(
			configuredClock,
			5*time.Second,
			5*time.Minute,
		),
		Schema:          jobqueue.Schema,
		SoftStopTimeout: shutdownTimeout,
		Workers:         workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create River client: %w", err)
	}
	return client, nil
}

type riverOptions struct {
	researcher         *research.Processor
	openAIWebhookStore *openaiwebhook.Store
	researchTimeout    time.Duration
	queueConfigs       map[string]river.QueueConfig
}

type RiverOption func(*riverOptions)

func WithResearchWorkers(
	processor *research.Processor,
	store *openaiwebhook.Store,
	timeout time.Duration,
) RiverOption {
	return func(configuration *riverOptions) {
		configuration.researcher = processor
		configuration.openAIWebhookStore = store
		configuration.researchTimeout = timeout
	}
}
