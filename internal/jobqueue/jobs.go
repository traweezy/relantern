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
)

const reconcileSchedulesPeriodicID = "reconcile-schedules-v1"
const reconcileOpenAIBackgroundPeriodicID = "reconcile-openai-background-v1"

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
	return jobs
}
