package jobqueue

import (
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	QueueCritical    = "critical"
	QueueFetch       = "fetch"
	QueueParse       = "parse"
	QueueAIFast      = "ai_fast"
	QueueAIResearch  = "ai_research"
	QueueDelivery    = "delivery"
	QueueMaintenance = "maintenance"

	ReconcileSchedulesKind        = "reconcile_schedules"
	ScheduleOccurrenceKind        = "schedule_occurrence"
	ReembedEntityKind             = "reembed_entity"
	ExtractItemKind               = "extract_item"
	ResearchStoryKind             = "research_story"
	PollOpenAIBackgroundKind      = "poll_openai_background"
	ReconcileOpenAIBackgroundKind = "reconcile_openai_background"
	ReturnSnoozedItemsKind        = "return_snoozed_items"
	ProcessManualCaptureKind      = "process_manual_capture"
	RunWeeklyRadarDiscoveryKind   = "run_weekly_radar_discovery"
	RefreshPackageMetricsKind     = "refresh_package_metrics"
)

const reconcileSchedulesPeriodicID = "reconcile-schedules-v1"
const reconcileOpenAIBackgroundPeriodicID = "reconcile-openai-background-v1"
const returnSnoozedItemsPeriodicID = "return-snoozed-items-v1"

type ReconcileSchedulesArgs struct {
	RunID string `json:"runId,omitempty" river:"unique"`
}

func (ReconcileSchedulesArgs) Kind() string {
	return ReconcileSchedulesKind
}

func (ReconcileSchedulesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Priority:    1,
		Queue:       QueueMaintenance,
		Tags:        []string{"schedule", "reconcile"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: time.Minute,
			ByQueue:  true,
			ByState:  rivertype.JobStates(),
		},
	}
}

type ScheduleOccurrenceArgs struct {
	OccurrenceID string `json:"occurrenceId" river:"unique"`
	RunID        string `json:"runId,omitempty"`
}

type ReembedEntityArgs struct {
	EntityType string `json:"entityType" river:"unique"`
	EntityID   string `json:"entityId" river:"unique"`
	RevisionID string `json:"revisionId" river:"unique"`
	ModelID    string `json:"modelId" river:"unique"`
}

type ExtractItemArgs struct {
	ItemID     string `json:"itemId" river:"unique"`
	RevisionID string `json:"revisionId" river:"unique"`
}

type ResearchStoryArgs struct {
	ClusterID string `json:"clusterId" river:"unique"`
}

func (ResearchStoryArgs) Kind() string {
	return ResearchStoryKind
}

func (ResearchStoryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueAIResearch,
		Tags:        []string{"openai", "research", "web-search"},
		UniqueOpts: river.UniqueOpts{
			ByArgs: true, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

type PollOpenAIBackgroundArgs struct {
	RunID      string `json:"runId,omitempty"`
	ResponseID string `json:"responseId" river:"unique"`
	WebhookID  string `json:"webhookId,omitempty"`
}

func (PollOpenAIBackgroundArgs) Kind() string {
	return PollOpenAIBackgroundKind
}

func (PollOpenAIBackgroundArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 20,
		Priority:    1,
		Queue:       QueueAIResearch,
		Tags:        []string{"openai", "background", "poll"},
		UniqueOpts: river.UniqueOpts{
			ByArgs: true, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

type ReconcileOpenAIBackgroundArgs struct{}

func (ReconcileOpenAIBackgroundArgs) Kind() string {
	return ReconcileOpenAIBackgroundKind
}

type ReturnSnoozedItemsArgs struct{}

type ProcessManualCaptureArgs struct {
	CaptureID string `json:"captureId" river:"unique"`
}

type RunWeeklyRadarDiscoveryArgs struct {
	RunID  string `json:"runId" river:"unique"`
	UserID string `json:"userId" river:"unique"`
}

func (RunWeeklyRadarDiscoveryArgs) Kind() string {
	return RunWeeklyRadarDiscoveryKind
}

func (RunWeeklyRadarDiscoveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueMaintenance,
		Tags:        []string{"radar", "discovery", "evidence"},
		UniqueOpts: river.UniqueOpts{
			ByArgs: true, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

type RefreshPackageMetricsArgs struct {
	CandidateID string `json:"candidateId" river:"unique"`
	UserID      string `json:"userId" river:"unique"`
}

func (RefreshPackageMetricsArgs) Kind() string {
	return RefreshPackageMetricsKind
}

func (RefreshPackageMetricsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    3,
		Queue:       QueueMaintenance,
		Tags:        []string{"radar", "metrics", "evidence"},
		UniqueOpts: river.UniqueOpts{
			ByArgs: true, ByPeriod: time.Hour, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

func (ProcessManualCaptureArgs) Kind() string {
	return ProcessManualCaptureKind
}

func (ProcessManualCaptureArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueFetch,
		Tags:        []string{"manual-capture", "fetch", "parse", "dedupe"},
		UniqueOpts: river.UniqueOpts{
			ByArgs: true, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

func (ReturnSnoozedItemsArgs) Kind() string {
	return ReturnSnoozedItemsKind
}

func (ReturnSnoozedItemsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueMaintenance,
		Tags:        []string{"reading-state", "snooze", "return"},
		UniqueOpts: river.UniqueOpts{
			ByPeriod: time.Minute, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

func (ReconcileOpenAIBackgroundArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Priority:    1,
		Queue:       QueueMaintenance,
		Tags:        []string{"openai", "background", "reconcile"},
		UniqueOpts: river.UniqueOpts{
			ByPeriod: 15 * time.Minute, ByQueue: true, ByState: rivertype.JobStates(),
		},
	}
}

func (ExtractItemArgs) Kind() string {
	return ExtractItemKind
}

func (ExtractItemArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueAIFast,
		Tags:        []string{"openai", "extraction", "evidence"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
			ByState: rivertype.JobStates(),
		},
	}
}

func (ReembedEntityArgs) Kind() string {
	return ReembedEntityKind
}

func (ReembedEntityArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    3,
		Queue:       QueueMaintenance,
		Tags:        []string{"embedding", "search"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
			ByState: rivertype.JobStates(),
		},
	}
}

func (ScheduleOccurrenceArgs) Kind() string {
	return ScheduleOccurrenceKind
}

func (ScheduleOccurrenceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    2,
		Queue:       QueueDelivery,
		Tags:        []string{"schedule", "occurrence"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
			ByState: rivertype.JobStates(),
		},
	}
}

func QueueConfigs() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueCritical:    {MaxWorkers: 4},
		QueueFetch:       {MaxWorkers: 12},
		QueueParse:       {MaxWorkers: 8},
		QueueAIFast:      {MaxWorkers: 4},
		QueueAIResearch:  {MaxWorkers: 2},
		QueueDelivery:    {MaxWorkers: 2},
		QueueMaintenance: {MaxWorkers: 1},
	}
}

func PeriodicJobs(interval time.Duration, includeOpenAIReconciliation ...bool) []*river.PeriodicJob {
	jobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(interval),
			func() (river.JobArgs, *river.InsertOpts) {
				return ReconcileSchedulesArgs{}, nil
			},
			&river.PeriodicJobOpts{ID: reconcileSchedulesPeriodicID, RunOnStart: true},
		),
	}
	if len(includeOpenAIReconciliation) > 0 && includeOpenAIReconciliation[0] {
		jobs = append(jobs, river.NewPeriodicJob(
			river.PeriodicInterval(15*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) {
				return ReconcileOpenAIBackgroundArgs{}, nil
			},
			&river.PeriodicJobOpts{ID: reconcileOpenAIBackgroundPeriodicID, RunOnStart: true},
		))
	}
	if len(includeOpenAIReconciliation) > 1 && includeOpenAIReconciliation[1] {
		jobs = append(jobs, river.NewPeriodicJob(
			river.PeriodicInterval(time.Minute),
			func() (river.JobArgs, *river.InsertOpts) {
				return ReturnSnoozedItemsArgs{}, nil
			},
			&river.PeriodicJobOpts{ID: returnSnoozedItemsPeriodicID, RunOnStart: true},
		))
	}
	return jobs
}
