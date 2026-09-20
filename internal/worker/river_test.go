package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/research"
)

type recordingResearchProcessor struct {
	request research.ProcessRequest
	calls   int
}

func (processor *recordingResearchProcessor) Process(_ context.Context, request research.ProcessRequest) (research.ProcessResult, error) {
	processor.calls++
	processor.request = request
	return research.ProcessResult{}, nil
}

func TestExtractionWorkerUsesConfiguredTimeout(t *testing.T) {
	t.Parallel()

	worker := &extractItemWorker{timeout: 7 * time.Minute}
	if got := worker.Timeout(&river.Job[jobqueue.ExtractItemArgs]{}); got != 7*time.Minute {
		t.Fatalf("Timeout() = %s", got)
	}
	if _, err := NewRiverClient(
		nil,
		clock.System{},
		time.Minute,
		time.Second,
		false,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		0,
	); err == nil {
		t.Fatal("NewRiverClient() accepted a zero extraction timeout")
	}
}

func TestExtractionResearchHandoffRequiresPublishableCompletion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		result          extraction.ProcessResult
		processError    error
		researchEnabled bool
		want            bool
	}{
		{name: "completed", researchEnabled: true, want: true},
		{name: "completed retry", result: extraction.ProcessResult{AlreadyCompleted: true}, researchEnabled: true, want: true},
		{name: "needs review", result: extraction.ProcessResult{NeedsReview: true}, researchEnabled: true},
		{name: "obsolete", result: extraction.ProcessResult{Obsolete: true}, researchEnabled: true},
		{name: "retryable failure", processError: errors.New("temporary provider failure"), researchEnabled: true},
		{name: "research disabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := shouldEnqueueResearch(test.result, test.processError, test.researchEnabled); actual != test.want {
				t.Fatalf("shouldEnqueueResearch() = %t, want %t", actual, test.want)
			}
		})
	}
}

func TestResearchWorkerPassesQueuedRevisionToProcessor(t *testing.T) {
	t.Parallel()
	processor := &recordingResearchProcessor{}
	worker := &researchStoryWorker{processor: processor, timeout: 15 * time.Minute}
	args := jobqueue.ResearchStoryArgs{
		ClusterID:   "01994f45-6770-7b45-9f2c-7ce5aca41111",
		RevisionID:  "01994f45-6770-7b45-9f2c-7ce5aca43333",
		InputSHA256: strings.Repeat("a", 64),
	}
	if err := worker.Work(context.Background(), &river.Job[jobqueue.ResearchStoryArgs]{Args: args}); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if processor.calls != 1 || processor.request.ClusterID != args.ClusterID || processor.request.RevisionID != args.RevisionID ||
		processor.request.InputSHA256 != args.InputSHA256 {
		t.Fatalf("Process() request = %+v, want %+v", processor.request, args)
	}
}

func TestResearchWorkerAcknowledgesLegacyJobWithoutProviderWork(t *testing.T) {
	t.Parallel()
	processor := &recordingResearchProcessor{}
	worker := &researchStoryWorker{processor: processor, timeout: 15 * time.Minute}
	legacy := jobqueue.ResearchStoryArgs{ClusterID: "01994f45-6770-7b45-9f2c-7ce5aca41111"}
	if err := worker.Work(context.Background(), &river.Job[jobqueue.ResearchStoryArgs]{Args: legacy}); err != nil {
		t.Fatalf("legacy Work() error = %v", err)
	}
	if processor.calls != 0 {
		t.Fatalf("legacy job called research processor %d times", processor.calls)
	}
}
