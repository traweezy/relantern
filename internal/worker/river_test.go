package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	alertstore "github.com/traweezy/relantern/internal/alert/pgstore"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/research"
)

type recordingResearchProcessor struct {
	request research.ProcessRequest
	calls   int
}

type recordingObservationSplitter struct {
	id  int64
	err error
}

func (splitter *recordingObservationSplitter) SplitAdvisoryObservation(_ context.Context, observationID int64) error {
	splitter.id = observationID
	return splitter.err
}

type recordingObservationAssessment struct {
	eventID    int64
	revisionID string
	err        error
}

type pendingBackfillWorkflow struct {
	criticalAlertWorkflow
	assessCalls  int
	catchUpCalls int
	assessError  error
	catchUpError error
}

func (workflow *pendingBackfillWorkflow) Assess(context.Context, string, string) error {
	workflow.assessCalls++
	return workflow.assessError
}

func (workflow *pendingBackfillWorkflow) CatchUp(context.Context, jobqueue.ReassessCurrentAdvisoriesArgs) error {
	workflow.catchUpCalls++
	return workflow.catchUpError
}

func TestAdvisoryBackfillWorkersSnoozePendingObservations(t *testing.T) {
	t.Parallel()
	workflow := &pendingBackfillWorkflow{
		assessError:  alertstore.ErrAdvisoryObservationBackfillPending,
		catchUpError: alertstore.ErrAdvisoryObservationBackfillPending,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	catchUp := &reassessCurrentAdvisoriesWorker{workflow: workflow, logger: logger}
	assess := &assessCriticalAdvisoryWorker{workflow: workflow, logger: logger}
	catchUpJob := &river.Job[jobqueue.ReassessCurrentAdvisoriesArgs]{
		JobRow: &rivertype.JobRow{},
	}
	assessJob := &river.Job[jobqueue.AssessCriticalAdvisoryArgs]{
		JobRow: &rivertype.JobRow{},
	}
	var snooze *river.JobSnoozeError
	if err := catchUp.Work(context.Background(), catchUpJob); !errors.As(err, &snooze) {
		t.Fatalf("pending catch-up returned %v, want River snooze", err)
	}
	snooze = nil
	if err := assess.Work(context.Background(), assessJob); !errors.As(err, &snooze) {
		t.Fatalf("pending assessment returned %v, want River snooze", err)
	}
	if workflow.catchUpCalls != 1 || workflow.assessCalls != 1 {
		t.Fatalf("pending workflow calls = catch-up %d, assessment %d",
			workflow.catchUpCalls, workflow.assessCalls)
	}
}

func TestAdvisoryEpisodeCutoverWorkersSnoozeWithoutAcknowledging(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backfill := &pendingBackfillWorkflow{assessError: alertstore.ErrEpisodeCutoverPending}
	backfillWorker := &assessCriticalAdvisoryWorker{workflow: backfill, logger: logger}
	backfillJob := &river.Job[jobqueue.AssessCriticalAdvisoryArgs]{JobRow: &rivertype.JobRow{}}
	var snooze *river.JobSnoozeError
	if err := backfillWorker.Work(context.Background(), backfillJob); !errors.As(err, &snooze) {
		t.Fatalf("episode cutover backfill returned %v, want River snooze", err)
	}
	observation := &recordingObservationAssessment{err: alertstore.ErrEpisodeCutoverPending}
	observationWorker := &assessAdvisoryObservationWorker{workflow: observation, logger: logger}
	observationJob := &river.Job[jobqueue.AssessAdvisoryObservationArgs]{JobRow: &rivertype.JobRow{}}
	snooze = nil
	if err := observationWorker.Work(context.Background(), observationJob); !errors.As(err, &snooze) {
		t.Fatalf("episode cutover observation returned %v, want River snooze", err)
	}
}

func (assessment *recordingObservationAssessment) AssessObservation(
	_ context.Context, eventID int64, revisionID string,
) error {
	assessment.eventID = eventID
	assessment.revisionID = revisionID
	return assessment.err
}

func TestAdvisoryObservationWorkersPreserveQueuedIdentityAndFailure(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	splitter := &recordingObservationSplitter{}
	splitWorker := &splitAdvisoryObservationWorker{parser: splitter, logger: logger}
	splitJob := &river.Job[jobqueue.SplitAdvisoryObservationArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   jobqueue.SplitAdvisoryObservationArgs{ObservationID: 37},
	}
	if splitWorker.Timeout(splitJob) != 2*time.Minute {
		t.Fatal("advisory split timeout is not bounded")
	}
	if err := splitWorker.Work(context.Background(), splitJob); err != nil || splitter.id != 37 {
		t.Fatalf("split work = (%d, %v), want (37, nil)", splitter.id, err)
	}

	assessment := &recordingObservationAssessment{err: errors.New("retry assessment")}
	assessWorker := &assessAdvisoryObservationWorker{workflow: assessment, logger: logger}
	assessJob := &river.Job[jobqueue.AssessAdvisoryObservationArgs]{
		JobRow: &rivertype.JobRow{},
		Args:   jobqueue.AssessAdvisoryObservationArgs{EventID: 53, RevisionID: "revision-53"},
	}
	if assessWorker.Timeout(assessJob) != 45*time.Second {
		t.Fatal("advisory assessment timeout is not bounded")
	}
	if err := assessWorker.Work(context.Background(), assessJob); !errors.Is(err, assessment.err) ||
		assessment.eventID != 53 || assessment.revisionID != "revision-53" {
		t.Fatalf("assessment work = (%d, %q, %v)", assessment.eventID,
			assessment.revisionID, err)
	}
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
