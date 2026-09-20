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
	"github.com/traweezy/relantern/internal/alert"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/delivery"
	"github.com/traweezy/relantern/internal/digest"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/ingestion"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/manualcapture"
	"github.com/traweezy/relantern/internal/openaiwebhook"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/radar"
	"github.com/traweezy/relantern/internal/reembedding"
	"github.com/traweezy/relantern/internal/research"
	researchstore "github.com/traweezy/relantern/internal/research/pgstore"
	"github.com/traweezy/relantern/internal/retention"
	"github.com/traweezy/relantern/internal/scheduler"
)

type reconcileSchedulesWorker struct {
	river.WorkerDefaults[jobqueue.ReconcileSchedulesArgs]
	health     *SchedulerHealth
	logger     *slog.Logger
	reconciler *scheduler.Reconciler
}

type reconcileSourcesWorker struct {
	river.WorkerDefaults[jobqueue.ReconcileSourcesArgs]
	poller *ingestion.Poller
	logger *slog.Logger
}

func (worker *reconcileSourcesWorker) Timeout(*river.Job[jobqueue.ReconcileSourcesArgs]) time.Duration {
	return 30 * time.Second
}

func (worker *reconcileSourcesWorker) Work(ctx context.Context, job *river.Job[jobqueue.ReconcileSourcesArgs]) error {
	count, err := worker.poller.Reconcile(ctx)
	if err != nil {
		return err
	}
	worker.logger.InfoContext(ctx, "source polling reconciled", "job_id", job.ID, "jobs_enqueued", count)
	return nil
}

type pollSourceEndpointWorker struct {
	river.WorkerDefaults[jobqueue.PollSourceEndpointArgs]
	poller *ingestion.Poller
}

func (worker *pollSourceEndpointWorker) Timeout(*river.Job[jobqueue.PollSourceEndpointArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *pollSourceEndpointWorker) Work(ctx context.Context, job *river.Job[jobqueue.PollSourceEndpointArgs]) error {
	return worker.poller.Poll(ctx, job.Args.RegistryID)
}

type parseRawDocumentWorker struct {
	river.WorkerDefaults[jobqueue.ParseRawDocumentArgs]
	parser *ingestion.ParseWorker
}

type criticalAlertWorkflow interface {
	Assess(context.Context, string, string) error
	CatchUp(context.Context, jobqueue.ReassessCurrentAdvisoriesArgs) error
	ReconcileDeliveries(context.Context, time.Time) (alert.ReconcileResult, error)
	BeginDelivery(context.Context, string, time.Time) (*alert.Delivery, error)
	CompleteDelivery(context.Context, string, alert.Receipt, time.Time) error
	FailDelivery(context.Context, string, string, bool, time.Time) error
}

type reassessCurrentAdvisoriesWorker struct {
	river.WorkerDefaults[jobqueue.ReassessCurrentAdvisoriesArgs]
	workflow criticalAlertWorkflow
	logger   *slog.Logger
}

func (worker *reassessCurrentAdvisoriesWorker) Timeout(*river.Job[jobqueue.ReassessCurrentAdvisoriesArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *reassessCurrentAdvisoriesWorker) Work(
	ctx context.Context, job *river.Job[jobqueue.ReassessCurrentAdvisoriesArgs],
) error {
	err := worker.workflow.CatchUp(ctx, job.Args)
	if err != nil {
		worker.logger.WarnContext(ctx, "watched advisory catch-up page failed",
			"job_id", job.ID,
			"settings_version", job.Args.SettingsVersion, "attempt", job.Attempt)
		return err
	}
	worker.logger.InfoContext(ctx, "watched advisory catch-up page completed",
		"job_id", job.ID,
		"settings_version", job.Args.SettingsVersion)
	return nil
}

type criticalAlertSender interface {
	SendAlert(context.Context, alert.Delivery) (alert.Receipt, error)
}

type assessCriticalAdvisoryWorker struct {
	river.WorkerDefaults[jobqueue.AssessCriticalAdvisoryArgs]
	workflow criticalAlertWorkflow
	logger   *slog.Logger
}

func (worker *assessCriticalAdvisoryWorker) Timeout(*river.Job[jobqueue.AssessCriticalAdvisoryArgs]) time.Duration {
	return 45 * time.Second
}

func (worker *assessCriticalAdvisoryWorker) Work(
	ctx context.Context, job *river.Job[jobqueue.AssessCriticalAdvisoryArgs],
) error {
	err := worker.workflow.Assess(ctx, job.Args.RawDocumentID, job.Args.RevisionID)
	if err != nil {
		worker.logger.WarnContext(ctx, "critical advisory assessment failed",
			"job_id", job.ID, "revision_id", job.Args.RevisionID, "attempt", job.Attempt)
		return err
	}
	worker.logger.InfoContext(ctx, "critical advisory assessment completed",
		"job_id", job.ID, "revision_id", job.Args.RevisionID)
	return nil
}

type deliverCriticalAlertWorker struct {
	river.WorkerDefaults[jobqueue.DeliverCriticalAlertArgs]
	clock    clock.Clock
	workflow criticalAlertWorkflow
	sender   criticalAlertSender
	logger   *slog.Logger
}

type reconcileCriticalAlertsWorker struct {
	river.WorkerDefaults[jobqueue.ReconcileCriticalAlertsArgs]
	clock    clock.Clock
	workflow criticalAlertWorkflow
	logger   *slog.Logger
}

func (worker *reconcileCriticalAlertsWorker) Timeout(*river.Job[jobqueue.ReconcileCriticalAlertsArgs]) time.Duration {
	return 30 * time.Second
}

func (worker *reconcileCriticalAlertsWorker) Work(
	ctx context.Context, job *river.Job[jobqueue.ReconcileCriticalAlertsArgs],
) error {
	result, err := worker.workflow.ReconcileDeliveries(ctx, worker.clock.Now().UTC())
	if err != nil {
		return err
	}
	if result.Overdue > 0 {
		worker.logger.ErrorContext(ctx, "confirmed critical advisory delivery overdue",
			"job_id", job.ID, "undelivered_count", result.Overdue,
			"recoveries_enqueued", result.Requeued)
	} else {
		worker.logger.InfoContext(ctx, "critical alert deliveries reconciled",
			"job_id", job.ID, "recoveries_enqueued", result.Requeued)
	}
	return nil
}

func (worker *deliverCriticalAlertWorker) Timeout(*river.Job[jobqueue.DeliverCriticalAlertArgs]) time.Duration {
	return 45 * time.Second
}

func (worker *deliverCriticalAlertWorker) Work(
	ctx context.Context, job *river.Job[jobqueue.DeliverCriticalAlertArgs],
) error {
	request, err := worker.workflow.BeginDelivery(ctx, job.Args.DeliveryID, worker.clock.Now().UTC())
	if errors.Is(err, alert.ErrPayloadIntegrity) {
		return river.JobCancel(err)
	}
	if err != nil || request == nil {
		if err != nil {
			worker.logger.WarnContext(ctx, "critical alert delivery claim failed",
				"job_id", job.ID, "delivery_id", job.Args.DeliveryID, "attempt", job.Attempt)
		}
		return err
	}
	receipt, err := worker.sender.SendAlert(ctx, *request)
	if err != nil {
		worker.logger.WarnContext(ctx, "critical alert provider send failed",
			"job_id", job.ID, "delivery_id", job.Args.DeliveryID,
			"attempt", request.Attempt, "permanent", delivery.Permanent(err))
		permanent := delivery.Permanent(err)
		code := "provider_retryable"
		if permanent {
			code = "provider_permanent"
		} else if job.Attempt >= job.MaxAttempts {
			code = "provider_retry_exhausted"
		}
		if recordErr := worker.workflow.FailDelivery(ctx, job.Args.DeliveryID, code, permanent, worker.clock.Now().UTC()); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		if permanent || job.Attempt >= job.MaxAttempts {
			return river.JobCancel(err)
		}
		return err
	}
	err = worker.workflow.CompleteDelivery(ctx, job.Args.DeliveryID, receipt, worker.clock.Now().UTC())
	if err != nil {
		worker.logger.WarnContext(ctx, "critical alert receipt recording failed",
			"job_id", job.ID, "delivery_id", job.Args.DeliveryID, "attempt", request.Attempt)
		return err
	}
	worker.logger.InfoContext(ctx, "critical alert delivery completed",
		"job_id", job.ID, "delivery_id", job.Args.DeliveryID, "attempt", request.Attempt)
	return nil
}

func (worker *parseRawDocumentWorker) Timeout(*river.Job[jobqueue.ParseRawDocumentArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *parseRawDocumentWorker) Work(ctx context.Context, job *river.Job[jobqueue.ParseRawDocumentArgs]) error {
	err := worker.parser.Parse(ctx, job.Args.RegistryID, job.Args.RawDocumentID)
	var parseErr *parsing.ParseError
	if errors.As(err, &parseErr) && parseErr.Code != parsing.ErrorObjectStorage && parseErr.Code != parsing.ErrorCanceled {
		return river.JobCancel(err)
	}
	return err
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

type digestWorkflow interface {
	Preflight(context.Context, string, time.Time) error
	Prepare(context.Context, string, time.Time) error
	Finalize(context.Context, string, time.Time) error
	BeginDelivery(context.Context, string, time.Time) (*digest.Delivery, error)
	CompleteDelivery(context.Context, string, digest.Receipt, time.Time) error
	FailDelivery(context.Context, string, string, time.Time) error
}

type digestSender interface {
	Send(context.Context, digest.Delivery) (digest.Receipt, error)
}

type preflightDigestSourcesWorker struct {
	river.WorkerDefaults[jobqueue.PreflightDigestSourcesArgs]
	clock     clock.Clock
	processor digestWorkflow
}

func (worker *preflightDigestSourcesWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.PreflightDigestSourcesArgs],
) error {
	return mapDigestWorkflowError(worker.processor.Preflight(ctx, job.Args.OccurrenceID, worker.clock.Now().UTC()))
}

type prepareDailyDigestWorker struct {
	river.WorkerDefaults[jobqueue.PrepareDailyDigestArgs]
	clock     clock.Clock
	processor digestWorkflow
}

func (worker *prepareDailyDigestWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.PrepareDailyDigestArgs],
) error {
	return mapDigestWorkflowError(worker.processor.Prepare(ctx, job.Args.OccurrenceID, worker.clock.Now().UTC()))
}

type finalizeDailyDigestWorker struct {
	river.WorkerDefaults[jobqueue.FinalizeDailyDigestArgs]
	clock     clock.Clock
	processor digestWorkflow
}

func (worker *finalizeDailyDigestWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.FinalizeDailyDigestArgs],
) error {
	return mapDigestWorkflowError(worker.processor.Finalize(ctx, job.Args.OccurrenceID, worker.clock.Now().UTC()))
}

type deliverDigestWorker struct {
	river.WorkerDefaults[jobqueue.DeliverDigestArgs]
	clock     clock.Clock
	processor digestWorkflow
	sender    digestSender
}

func (worker *deliverDigestWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.DeliverDigestArgs],
) error {
	now := worker.clock.Now().UTC()
	request, err := worker.processor.BeginDelivery(ctx, job.Args.DigestID, now)
	if err != nil {
		return mapDigestWorkflowError(err)
	}
	if request == nil {
		return nil
	}
	receipt, err := worker.sender.Send(ctx, *request)
	if err != nil {
		errorCode := "delivery_provider_unavailable"
		if delivery.Permanent(err) {
			errorCode = "delivery_provider_rejected"
		}
		if recordErr := worker.processor.FailDelivery(ctx, request.DigestID, errorCode, worker.clock.Now().UTC()); recordErr != nil {
			return fmt.Errorf("send digest: %w; record failure: %v", err, recordErr)
		}
		if delivery.Permanent(err) {
			return river.JobCancel(err)
		}
		return err
	}
	return worker.processor.CompleteDelivery(ctx, request.DigestID, receipt, worker.clock.Now().UTC())
}

func mapDigestWorkflowError(err error) error {
	if errors.Is(err, digest.ErrInvalid) || errors.Is(err, digest.ErrNotFound) || errors.Is(err, digest.ErrConflict) {
		return river.JobCancel(err)
	}
	return err
}

type reembedEntityWorker struct {
	river.WorkerDefaults[jobqueue.ReembedEntityArgs]
	processor *reembedding.Processor
}

type extractItemWorker struct {
	river.WorkerDefaults[jobqueue.ExtractItemArgs]
	processor *extraction.Processor
	pool      *pgxpool.Pool
	jobs      *jobqueue.Inserter
	clock     clock.Clock
	timeout   time.Duration
}

type manualCaptureWorker struct {
	river.WorkerDefaults[jobqueue.ProcessManualCaptureArgs]
	processor *manualcapture.Processor
}

type radarProcessor interface {
	ProcessDiscovery(context.Context, string, string, time.Time) (radar.DiscoveryResult, error)
	RefreshCandidate(context.Context, string, string, time.Time) error
}

type runWeeklyRadarDiscoveryWorker struct {
	river.WorkerDefaults[jobqueue.RunWeeklyRadarDiscoveryArgs]
	clock     clock.Clock
	logger    *slog.Logger
	processor radarProcessor
}

func (worker *runWeeklyRadarDiscoveryWorker) Timeout(*river.Job[jobqueue.RunWeeklyRadarDiscoveryArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *runWeeklyRadarDiscoveryWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.RunWeeklyRadarDiscoveryArgs],
) error {
	result, err := worker.processor.ProcessDiscovery(
		ctx,
		job.Args.RunID,
		job.Args.UserID,
		worker.clock.Now().UTC(),
	)
	if errors.Is(err, radar.ErrNotFound) || errors.Is(err, radar.ErrInvalid) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	worker.logger.InfoContext(ctx, "weekly Radar discovery complete",
		"job_id", job.ID,
		"run_id", job.Args.RunID,
		"evidence_count", result.EvidenceCount,
		"candidate_count", result.CandidateCount,
		"misleading_count", result.MisleadingCount,
	)
	return nil
}

type refreshPackageMetricsWorker struct {
	river.WorkerDefaults[jobqueue.RefreshPackageMetricsArgs]
	clock     clock.Clock
	processor radarProcessor
}

func (worker *refreshPackageMetricsWorker) Timeout(*river.Job[jobqueue.RefreshPackageMetricsArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *refreshPackageMetricsWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.RefreshPackageMetricsArgs],
) error {
	err := worker.processor.RefreshCandidate(
		ctx,
		job.Args.CandidateID,
		job.Args.UserID,
		worker.clock.Now().UTC(),
	)
	if errors.Is(err, radar.ErrNotFound) || errors.Is(err, radar.ErrInvalid) {
		return river.JobCancel(err)
	}
	return err
}

func (worker *manualCaptureWorker) Timeout(*river.Job[jobqueue.ProcessManualCaptureArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *manualCaptureWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ProcessManualCaptureArgs],
) error {
	err := worker.processor.Process(ctx, job.Args.CaptureID)
	if errors.Is(err, manualcapture.ErrInvalidCapture) || errors.Is(err, manualcapture.ErrPermanent) {
		return river.JobCancel(err)
	}
	return err
}

type researchStoryWorker struct {
	river.WorkerDefaults[jobqueue.ResearchStoryArgs]
	processor researchProcessor
	logger    *slog.Logger
	timeout   time.Duration
}

type researchProcessor interface {
	Process(context.Context, research.ProcessRequest) (research.ProcessResult, error)
}

func (worker *researchStoryWorker) Timeout(*river.Job[jobqueue.ResearchStoryArgs]) time.Duration {
	return worker.timeout
}

func (worker *researchStoryWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ResearchStoryArgs],
) error {
	if job.Args.RevisionID == "" || job.Args.InputSHA256 == "" {
		// Pre-snapshot jobs cannot prove which revision or facts they target.
		// Reconciliation may enqueue a current, admitted snapshot separately.
		if worker.logger != nil {
			worker.logger.InfoContext(ctx, "obsolete legacy research job acknowledged",
				"job_id", job.ID, "cluster_id", job.Args.ClusterID)
		}
		return nil
	}
	_, err := worker.processor.Process(ctx, research.ProcessRequest{
		ClusterID: job.Args.ClusterID, RevisionID: job.Args.RevisionID,
		InputSHA256: job.Args.InputSHA256,
	})
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

type snoozeReturner interface {
	ReturnDueSnoozes(context.Context, int) (int, error)
}

type returnSnoozedItemsWorker struct {
	river.WorkerDefaults[jobqueue.ReturnSnoozedItemsArgs]
	logger   *slog.Logger
	returner snoozeReturner
}

type retentionRunner interface {
	Run(context.Context, time.Time) (retention.Counts, error)
}

type runRetentionWorker struct {
	river.WorkerDefaults[jobqueue.RunRetentionArgs]
	clock  clock.Clock
	logger *slog.Logger
	runner retentionRunner
}

func (worker *runRetentionWorker) Timeout(*river.Job[jobqueue.RunRetentionArgs]) time.Duration {
	return 10 * time.Minute
}

func (worker *runRetentionWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.RunRetentionArgs],
) error {
	counts, err := worker.runner.Run(ctx, worker.clock.Now().UTC())
	if errors.Is(err, retention.ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	worker.logger.InfoContext(ctx, "retention run complete", "job_id", job.ID, "counts", counts)
	return nil
}

func (worker *returnSnoozedItemsWorker) Work(
	ctx context.Context,
	job *river.Job[jobqueue.ReturnSnoozedItemsArgs],
) error {
	count, err := worker.returner.ReturnDueSnoozes(ctx, 200)
	if err != nil {
		return err
	}
	worker.logger.InfoContext(
		ctx,
		"due snoozes returned",
		"job_id", job.ID,
		"returned_count", count,
	)
	return nil
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
	result, err := worker.processor.Process(ctx, extraction.ProcessRequest{
		ItemID:     job.Args.ItemID,
		RevisionID: job.Args.RevisionID,
	})
	if extraction.Permanent(err) {
		return river.JobCancel(err)
	}
	if !shouldEnqueueResearch(result, err, worker.jobs != nil) {
		return err
	}
	return worker.enqueueResearch(ctx, job.Args.ItemID, job.Args.RevisionID)
}

func shouldEnqueueResearch(result extraction.ProcessResult, processError error, researchEnabled bool) bool {
	return processError == nil && !result.Obsolete && !result.NeedsReview && researchEnabled
}

func (worker *extractItemWorker) enqueueResearch(ctx context.Context, itemID, revisionID string) error {
	transaction, err := worker.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin extraction research handoff: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var clusterID, primaryRevisionID string
	if err := transaction.QueryRow(ctx, `
		select cluster.id::text, primary_item.current_revision_id::text
		from app.cluster_members member
		join app.items extracted_item on extracted_item.id = member.item_id
		join app.story_clusters cluster on cluster.id = member.cluster_id
		join app.items primary_item on primary_item.id = cluster.primary_item_id
		where member.item_id = $1::uuid
			and extracted_item.current_revision_id = $2::uuid
		for share of cluster, extracted_item, primary_item`, itemID, revisionID).Scan(&clusterID, &primaryRevisionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("select extraction research cluster: %w", err)
	}
	admitted, err := researchstore.ResearchAdmissions(ctx, transaction, worker.clock.Now().UTC(), nil)
	if err != nil {
		return fmt.Errorf("select extraction research admission: %w", err)
	}
	if _, eligible := admitted[clusterID]; !eligible {
		return nil
	}
	currentRevisionID, inputSHA256, err := researchstore.CurrentInputSHA256(ctx, transaction, clusterID)
	if err != nil {
		return fmt.Errorf("read extraction research snapshot: %w", err)
	}
	if currentRevisionID != primaryRevisionID {
		return nil
	}
	var attempted bool
	if err := transaction.QueryRow(ctx, `
		select exists (
			select 1 from app.ai_runs run
			where run.cluster_id = $1::uuid and run.revision_id = $2::uuid
				and run.purpose = $3 and run.input_sha256 = decode($4, 'hex')
		)`, clusterID, primaryRevisionID, research.Purpose, inputSHA256).Scan(&attempted); err != nil {
		return fmt.Errorf("inspect extraction research attempt: %w", err)
	}
	if attempted {
		return nil
	}
	if _, _, err := worker.jobs.EnqueueResearchStory(
		ctx,
		transaction,
		jobqueue.ResearchStoryArgs{ClusterID: clusterID, RevisionID: primaryRevisionID, InputSHA256: inputSHA256},
	); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit extraction research handoff: %w", err)
	}
	return nil
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
	var researchJobs *jobqueue.Inserter
	if configuration.researcher != nil {
		var err error
		researchJobs, err = jobqueue.NewInserter()
		if err != nil {
			return nil, fmt.Errorf("create extraction research inserter: %w", err)
		}
	}
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
			&extractItemWorker{
				processor: extractor,
				pool:      pool,
				jobs:      researchJobs,
				clock:     configuredClock,
				timeout:   extractionTimeout,
			},
		); err != nil {
			return nil, fmt.Errorf("register structured extraction worker: %w", err)
		}
	}
	if configuration.researcher != nil {
		if err := river.AddWorkerSafely(workers, &researchStoryWorker{
			processor: configuration.researcher,
			logger:    logger,
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
	if configuration.snoozeReturner != nil {
		if err := river.AddWorkerSafely(workers, &returnSnoozedItemsWorker{
			logger: logger, returner: configuration.snoozeReturner,
		}); err != nil {
			return nil, fmt.Errorf("register returned-snooze worker: %w", err)
		}
	}
	if configuration.manualCaptureProcessor != nil {
		if err := river.AddWorkerSafely(workers, &manualCaptureWorker{
			processor: configuration.manualCaptureProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register manual-capture worker: %w", err)
		}
	}
	if configuration.sourcePoller != nil {
		if configuration.sourceParser == nil {
			return nil, errors.New("source poller requires a parser")
		}
		if err := river.AddWorkerSafely(workers, &reconcileSourcesWorker{poller: configuration.sourcePoller, logger: logger}); err != nil {
			return nil, fmt.Errorf("register source reconciliation worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &pollSourceEndpointWorker{poller: configuration.sourcePoller}); err != nil {
			return nil, fmt.Errorf("register source poll worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &parseRawDocumentWorker{parser: configuration.sourceParser}); err != nil {
			return nil, fmt.Errorf("register source parse worker: %w", err)
		}
	}
	if configuration.criticalAlerts != nil {
		if configuration.criticalAlertSender == nil {
			return nil, errors.New("critical alert workers require a delivery sender")
		}
		if err := river.AddWorkerSafely(workers, &assessCriticalAdvisoryWorker{
			workflow: configuration.criticalAlerts, logger: logger,
		}); err != nil {
			return nil, fmt.Errorf("register critical advisory assessment worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &reassessCurrentAdvisoriesWorker{
			workflow: configuration.criticalAlerts, logger: logger,
		}); err != nil {
			return nil, fmt.Errorf("register watched advisory catch-up worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &deliverCriticalAlertWorker{
			clock: configuredClock, workflow: configuration.criticalAlerts,
			sender: configuration.criticalAlertSender, logger: logger,
		}); err != nil {
			return nil, fmt.Errorf("register critical alert delivery worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &reconcileCriticalAlertsWorker{
			clock: configuredClock, workflow: configuration.criticalAlerts, logger: logger,
		}); err != nil {
			return nil, fmt.Errorf("register critical alert reconciliation worker: %w", err)
		}
	}
	if configuration.radarProcessor != nil {
		if err := river.AddWorkerSafely(workers, &runWeeklyRadarDiscoveryWorker{
			clock: configuredClock, logger: logger, processor: configuration.radarProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register Radar discovery worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &refreshPackageMetricsWorker{
			clock: configuredClock, processor: configuration.radarProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register Radar metrics worker: %w", err)
		}
	}
	if configuration.digestProcessor != nil {
		if configuration.digestSender == nil {
			return nil, errors.New("digest workers require a delivery sender")
		}
		if err := river.AddWorkerSafely(workers, &preflightDigestSourcesWorker{
			clock: configuredClock, processor: configuration.digestProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register digest preflight worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &prepareDailyDigestWorker{
			clock: configuredClock, processor: configuration.digestProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register digest preparation worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &finalizeDailyDigestWorker{
			clock: configuredClock, processor: configuration.digestProcessor,
		}); err != nil {
			return nil, fmt.Errorf("register digest finalization worker: %w", err)
		}
		if err := river.AddWorkerSafely(workers, &deliverDigestWorker{
			clock: configuredClock, processor: configuration.digestProcessor,
			sender: configuration.digestSender,
		}); err != nil {
			return nil, fmt.Errorf("register digest delivery worker: %w", err)
		}
	}
	if configuration.retentionRunner != nil {
		if err := river.AddWorkerSafely(workers, &runRetentionWorker{
			clock: configuredClock, logger: logger, runner: configuration.retentionRunner,
		}); err != nil {
			return nil, fmt.Errorf("register retention worker: %w", err)
		}
	}

	var periodicJobs []*river.PeriodicJob
	if enablePeriodicJobs {
		periodicJobs = jobqueue.PeriodicJobs(
			interval,
			configuration.researcher != nil,
			configuration.snoozeReturner != nil,
			configuration.retentionRunner != nil,
			configuration.sourcePoller != nil,
			configuration.criticalAlerts != nil,
		)
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
	researcher             *research.Processor
	openAIWebhookStore     *openaiwebhook.Store
	researchTimeout        time.Duration
	snoozeReturner         snoozeReturner
	queueConfigs           map[string]river.QueueConfig
	manualCaptureProcessor *manualcapture.Processor
	radarProcessor         radarProcessor
	digestProcessor        digestWorkflow
	digestSender           digestSender
	retentionRunner        retentionRunner
	sourcePoller           *ingestion.Poller
	sourceParser           *ingestion.ParseWorker
	criticalAlerts         criticalAlertWorkflow
	criticalAlertSender    criticalAlertSender
}

func WithSourceIngestion(poller *ingestion.Poller, parser *ingestion.ParseWorker) RiverOption {
	return func(configuration *riverOptions) {
		configuration.sourcePoller = poller
		configuration.sourceParser = parser
	}
}

func WithCriticalAlerts(workflow criticalAlertWorkflow, sender criticalAlertSender) RiverOption {
	return func(configuration *riverOptions) {
		configuration.criticalAlerts = workflow
		configuration.criticalAlertSender = sender
	}
}

func WithSnoozeReturner(returner snoozeReturner) RiverOption {
	return func(configuration *riverOptions) {
		configuration.snoozeReturner = returner
	}
}

func WithManualCaptureProcessor(processor *manualcapture.Processor) RiverOption {
	return func(configuration *riverOptions) {
		configuration.manualCaptureProcessor = processor
	}
}

func WithRadarProcessor(processor radarProcessor) RiverOption {
	return func(configuration *riverOptions) {
		configuration.radarProcessor = processor
	}
}

func WithDigestProcessor(processor digestWorkflow, sender digestSender) RiverOption {
	return func(configuration *riverOptions) {
		configuration.digestProcessor = processor
		configuration.digestSender = sender
	}
}

func WithRetentionRunner(runner retentionRunner) RiverOption {
	return func(configuration *riverOptions) {
		configuration.retentionRunner = runner
	}
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
