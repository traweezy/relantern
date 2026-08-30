package worker

import (
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/jobqueue"
)

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
