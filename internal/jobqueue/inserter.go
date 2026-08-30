package jobqueue

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const Schema = "river"

var queueNamePattern = regexp.MustCompile(`^(?:[a-z0-9])+(?:[_-]?[a-z0-9]+)*$`)

type Inserter struct {
	client        *river.Client[pgx.Tx]
	queueOverride string
}

func NewInserter() (*Inserter, error) {
	return newInserter("")
}

// NewIsolatedTestInserter creates an inserter whose jobs cannot be claimed by
// the application's production queue configuration. It is intended only for
// integration tests that manually drive queued workflows.
func NewIsolatedTestInserter(queue string) (*Inserter, error) {
	if err := validateIsolatedTestQueue(queue); err != nil {
		return nil, err
	}
	return newInserter(queue)
}

func newInserter(queueOverride string) (*Inserter, error) {
	client, err := river.NewClient(riverpgxv5.New(nil), &river.Config{Schema: Schema})
	if err != nil {
		return nil, fmt.Errorf("create transactional River inserter: %w", err)
	}
	return &Inserter{client: client, queueOverride: queueOverride}, nil
}

func validateIsolatedTestQueue(queue string) error {
	if _, production := QueueConfigs()[queue]; production {
		return fmt.Errorf("isolated test queue %q conflicts with a production queue", queue)
	}
	if !strings.HasPrefix(queue, "test_") {
		return fmt.Errorf("isolated test queue %q must start with test_", queue)
	}
	if len(queue) > 64 {
		return fmt.Errorf("isolated test queue %q exceeds 64 characters", queue)
	}
	if !queueNamePattern.MatchString(queue) {
		return fmt.Errorf("isolated test queue %q contains unsupported characters", queue)
	}
	return nil
}

func (inserter *Inserter) insertOptions(arguments river.JobArgs) *river.InsertOpts {
	if inserter.queueOverride == "" {
		return nil
	}
	options := river.InsertOpts{}
	if provider, ok := arguments.(river.JobArgsWithInsertOpts); ok {
		options = provider.InsertOpts()
	}
	options.Queue = inserter.queueOverride
	return &options
}

func (inserter *Inserter) EnqueueScheduleOccurrence(
	ctx context.Context,
	tx pgx.Tx,
	occurrenceID string,
	runID string,
) (int64, bool, error) {
	arguments := ScheduleOccurrenceArgs{OccurrenceID: occurrenceID, RunID: runID}
	result, err := inserter.client.InsertTx(
		ctx,
		tx,
		arguments,
		inserter.insertOptions(arguments),
	)
	if err != nil {
		return 0, false, fmt.Errorf("enqueue schedule occurrence %s: %w", occurrenceID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueuePreflightDigestSources(
	ctx context.Context,
	tx pgx.Tx,
	arguments PreflightDigestSourcesArgs,
) (int64, bool, error) {
	return inserter.enqueueDigestJob(ctx, tx, arguments, arguments.OccurrenceID)
}

func (inserter *Inserter) EnqueuePrepareDailyDigest(
	ctx context.Context,
	tx pgx.Tx,
	arguments PrepareDailyDigestArgs,
) (int64, bool, error) {
	return inserter.enqueueDigestJob(ctx, tx, arguments, arguments.OccurrenceID)
}

func (inserter *Inserter) EnqueueFinalizeDailyDigest(
	ctx context.Context,
	tx pgx.Tx,
	arguments FinalizeDailyDigestArgs,
) (int64, bool, error) {
	return inserter.enqueueDigestJob(ctx, tx, arguments, arguments.OccurrenceID)
}

func (inserter *Inserter) EnqueueDeliverDigest(
	ctx context.Context,
	tx pgx.Tx,
	arguments DeliverDigestArgs,
) (int64, bool, error) {
	return inserter.enqueueDigestJob(ctx, tx, arguments, arguments.DigestID)
}

func (inserter *Inserter) enqueueDigestJob(
	ctx context.Context,
	tx pgx.Tx,
	arguments river.JobArgs,
	identifier string,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue digest work for %s: %w", identifier, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueReembedEntity(
	ctx context.Context,
	tx pgx.Tx,
	arguments ReembedEntityArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue re-embedding for %s: %w", arguments.EntityID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueExtractItem(
	ctx context.Context,
	tx pgx.Tx,
	arguments ExtractItemArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue structured extraction for %s: %w", arguments.ItemID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueResearchStory(
	ctx context.Context,
	tx pgx.Tx,
	arguments ResearchStoryArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue research for cluster %s: %w", arguments.ClusterID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueManualCapture(
	ctx context.Context,
	tx pgx.Tx,
	arguments ProcessManualCaptureArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue manual capture %s: %w", arguments.CaptureID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueuePollOpenAIBackground(
	ctx context.Context,
	tx pgx.Tx,
	arguments PollOpenAIBackgroundArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue OpenAI background poll for %s: %w", arguments.ResponseID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueRadarDiscovery(
	ctx context.Context,
	tx pgx.Tx,
	arguments RunWeeklyRadarDiscoveryArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue Radar discovery %s: %w", arguments.RunID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueRefreshPackageMetrics(
	ctx context.Context,
	tx pgx.Tx,
	arguments RefreshPackageMetricsArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, inserter.insertOptions(arguments))
	if err != nil {
		return 0, false, fmt.Errorf("enqueue Radar metrics refresh %s: %w", arguments.CandidateID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}
