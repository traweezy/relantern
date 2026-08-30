package jobqueue

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const Schema = "river"

type Inserter struct {
	client *river.Client[pgx.Tx]
}

func NewInserter() (*Inserter, error) {
	client, err := river.NewClient(riverpgxv5.New(nil), &river.Config{Schema: Schema})
	if err != nil {
		return nil, fmt.Errorf("create transactional River inserter: %w", err)
	}
	return &Inserter{client: client}, nil
}

func (inserter *Inserter) EnqueueScheduleOccurrence(
	ctx context.Context,
	tx pgx.Tx,
	occurrenceID string,
	runID string,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(
		ctx,
		tx,
		ScheduleOccurrenceArgs{OccurrenceID: occurrenceID, RunID: runID},
		nil,
	)
	if err != nil {
		return 0, false, fmt.Errorf("enqueue schedule occurrence %s: %w", occurrenceID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}

func (inserter *Inserter) EnqueueReembedEntity(
	ctx context.Context,
	tx pgx.Tx,
	arguments ReembedEntityArgs,
) (int64, bool, error) {
	result, err := inserter.client.InsertTx(ctx, tx, arguments, nil)
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
	result, err := inserter.client.InsertTx(ctx, tx, arguments, nil)
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
	result, err := inserter.client.InsertTx(ctx, tx, arguments, nil)
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
	result, err := inserter.client.InsertTx(ctx, tx, arguments, nil)
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
	result, err := inserter.client.InsertTx(ctx, tx, arguments, nil)
	if err != nil {
		return 0, false, fmt.Errorf("enqueue OpenAI background poll for %s: %w", arguments.ResponseID, err)
	}
	return result.Job.ID, !result.UniqueSkippedAsDuplicate, nil
}
