package jobqueue_test

import (
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestQueueConfigsDeclareEverySpecificationQueue(t *testing.T) {
	t.Parallel()

	queues := jobqueue.QueueConfigs()
	want := map[string]int{
		jobqueue.QueueCritical:    4,
		jobqueue.QueueFetch:       12,
		jobqueue.QueueParse:       8,
		jobqueue.QueueAIFast:      4,
		jobqueue.QueueAIResearch:  2,
		jobqueue.QueueDelivery:    2,
		jobqueue.QueueMaintenance: 1,
	}
	for name, workers := range want {
		configuration, ok := queues[name]
		if !ok {
			t.Fatalf("queue %q is missing", name)
		}
		if configuration.MaxWorkers != workers {
			t.Fatalf("queue %q MaxWorkers = %d, want %d", name, configuration.MaxWorkers, workers)
		}
	}
	if len(queues) != len(want) {
		t.Fatalf("QueueConfigs() has %d queues, want %d", len(queues), len(want))
	}
}

func TestScheduleJobOptionsAreBoundedAndUnique(t *testing.T) {
	t.Parallel()

	reconcileOptions := (jobqueue.ReconcileSchedulesArgs{}).InsertOpts()
	if reconcileOptions.MaxAttempts != 3 ||
		reconcileOptions.UniqueOpts.ByPeriod != time.Minute ||
		!reconcileOptions.UniqueOpts.ByArgs {
		t.Fatalf("reconcile options = %+v", reconcileOptions)
	}
	if (jobqueue.ReconcileSchedulesArgs{}).Kind() != jobqueue.ReconcileSchedulesKind {
		t.Fatal("reconcile kind is not stable")
	}
	occurrenceOptions := (jobqueue.ScheduleOccurrenceArgs{}).InsertOpts()
	if occurrenceOptions.MaxAttempts != 5 || !occurrenceOptions.UniqueOpts.ByArgs {
		t.Fatalf("occurrence options = %+v", occurrenceOptions)
	}
}

func TestReembedJobIsBoundedUniqueMaintenanceWork(t *testing.T) {
	t.Parallel()
	options := (jobqueue.ReembedEntityArgs{}).InsertOpts()
	if (jobqueue.ReembedEntityArgs{}).Kind() != jobqueue.ReembedEntityKind ||
		options.Queue != jobqueue.QueueMaintenance || options.MaxAttempts != 5 ||
		!options.UniqueOpts.ByArgs || !options.UniqueOpts.ByQueue {
		t.Fatalf("re-embedding options = %+v", options)
	}
}

func TestExtractionJobIsBoundedUniqueFastAIWork(t *testing.T) {
	t.Parallel()
	options := (jobqueue.ExtractItemArgs{}).InsertOpts()
	if (jobqueue.ExtractItemArgs{}).Kind() != jobqueue.ExtractItemKind ||
		options.Queue != jobqueue.QueueAIFast || options.MaxAttempts != 5 ||
		!options.UniqueOpts.ByArgs || !options.UniqueOpts.ByQueue ||
		len(options.Tags) != 3 {
		t.Fatalf("extraction options = %+v", options)
	}
}

func TestPeriodicJobsDeclareRunOnStartSchedule(t *testing.T) {
	t.Parallel()

	periodicJobs := jobqueue.PeriodicJobs(time.Minute, true)
	if len(periodicJobs) != 2 || periodicJobs[0] == nil || periodicJobs[1] == nil {
		t.Fatalf("PeriodicJobs() = %+v", periodicJobs)
	}
	if defaultJobs := jobqueue.PeriodicJobs(time.Minute); len(defaultJobs) != 1 {
		t.Fatalf("default PeriodicJobs() = %+v", defaultJobs)
	}
}

func TestResearchJobsAreBoundedAndUseResearchQueue(t *testing.T) {
	t.Parallel()

	researchOptions := (jobqueue.ResearchStoryArgs{}).InsertOpts()
	if (jobqueue.ResearchStoryArgs{}).Kind() != jobqueue.ResearchStoryKind ||
		researchOptions.Queue != jobqueue.QueueAIResearch || researchOptions.MaxAttempts != 5 ||
		!researchOptions.UniqueOpts.ByArgs || !researchOptions.UniqueOpts.ByQueue {
		t.Fatalf("research options = %+v", researchOptions)
	}
	pollOptions := (jobqueue.PollOpenAIBackgroundArgs{}).InsertOpts()
	if (jobqueue.PollOpenAIBackgroundArgs{}).Kind() != jobqueue.PollOpenAIBackgroundKind ||
		pollOptions.Queue != jobqueue.QueueAIResearch || pollOptions.MaxAttempts != 20 ||
		!pollOptions.UniqueOpts.ByArgs || !pollOptions.UniqueOpts.ByQueue {
		t.Fatalf("poll options = %+v", pollOptions)
	}
	reconcileOptions := (jobqueue.ReconcileOpenAIBackgroundArgs{}).InsertOpts()
	if (jobqueue.ReconcileOpenAIBackgroundArgs{}).Kind() != jobqueue.ReconcileOpenAIBackgroundKind ||
		reconcileOptions.Queue != jobqueue.QueueMaintenance ||
		reconcileOptions.UniqueOpts.ByPeriod != 15*time.Minute {
		t.Fatalf("background reconciliation options = %+v", reconcileOptions)
	}
}

func TestExponentialRetryPolicyCapsDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	policy := jobqueue.NewExponentialRetryPolicy(clock.NewFixed(now), 5*time.Second, 20*time.Second)
	tests := []struct {
		attempt int
		delay   time.Duration
	}{
		{attempt: 1, delay: 5 * time.Second},
		{attempt: 2, delay: 10 * time.Second},
		{attempt: 3, delay: 20 * time.Second},
		{attempt: 20, delay: 20 * time.Second},
	}
	for _, test := range tests {
		got := policy.NextRetry(&rivertype.JobRow{Attempt: test.attempt})
		want := now.Add(test.delay)
		if !got.Equal(want) {
			t.Fatalf("attempt %d retry = %s, want %s", test.attempt, got, want)
		}
	}
}

func TestPermanentErrorPreservesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("invalid provider request")
	wrapped := jobqueue.Permanent(cause)
	if !jobqueue.IsPermanent(wrapped) || !errors.Is(wrapped, cause) || wrapped.Error() != cause.Error() {
		t.Fatalf("Permanent() did not preserve classification and cause: %v", wrapped)
	}
	if jobqueue.Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must remain nil")
	}
}
