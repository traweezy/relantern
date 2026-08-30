package controlplane

import "time"

type SourcePreference struct {
	Muted               bool    `json:"muted"`
	ExcludeFromDigest   bool    `json:"excludeFromDigest"`
	RelevanceAdjustment float64 `json:"relevanceAdjustment"`
	Version             int64   `json:"version"`
}

type SourceEndpoint struct {
	ID                  string     `json:"id"`
	Connector           string     `json:"connector"`
	URL                 string     `json:"url"`
	PollIntervalSeconds int64      `json:"pollIntervalSeconds"`
	Priority            string     `json:"priority"`
	HealthState         string     `json:"healthState"`
	NextPollAt          *time.Time `json:"nextPollAt,omitempty"`
	LastAttemptAt       *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt,omitempty"`
	LatestStatusCode    *int       `json:"latestStatusCode,omitempty"`
	LatestErrorCode     string     `json:"latestErrorCode,omitempty"`
}

type SourceValidation struct {
	ID               string    `json:"id"`
	State            string    `json:"state"`
	CheckCount       int       `json:"checkCount"`
	FailedCheckCount int       `json:"failedCheckCount"`
	Explanation      string    `json:"explanation"`
	CompletedAt      time.Time `json:"completedAt"`
}

type ManagedSource struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	TrustTier        string            `json:"trustTier"`
	Owner            string            `json:"owner"`
	Origin           string            `json:"origin"`
	ValidationState  string            `json:"validationState"`
	HomepageURL      string            `json:"homepageUrl"`
	ContentPolicy    string            `json:"contentPolicy"`
	Enabled          bool              `json:"enabled"`
	PollingEnabled   bool              `json:"pollingEnabled"`
	Topics           []string          `json:"topics"`
	ContentCount     int64             `json:"contentCount"`
	DuplicateRate    float64           `json:"duplicateRate"`
	SuccessCount24h  int64             `json:"successCount24h"`
	FailureCount24h  int64             `json:"failureCount24h"`
	ErrorBudgetState string            `json:"errorBudgetState"`
	Preference       SourcePreference  `json:"preference"`
	Endpoints        []SourceEndpoint  `json:"endpoints"`
	LatestValidation *SourceValidation `json:"latestValidation,omitempty"`
	ReviewedAt       *time.Time        `json:"reviewedAt,omitempty"`
}

type SourcesSnapshot struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Sources     []ManagedSource `json:"sources"`
}

type UpdateSourcePreferenceRequest struct {
	UserID              string
	SourceID            string
	Muted               bool
	ExcludeFromDigest   bool
	RelevanceAdjustment float64
	ExpectedVersion     int64
}

type SourceActionRequest struct {
	UserID   string
	SourceID string
	Action   string
	Reason   string
}

type InterestTopic struct {
	TopicID    string   `json:"topicId"`
	Priority   int16    `json:"priority"`
	Weight     float64  `json:"weight"`
	Keywords   []string `json:"keywords"`
	Exclusions []string `json:"exclusions"`
}

type InterestProfile struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	ProfileSummary string          `json:"profileSummary"`
	Version        int64           `json:"version"`
	Topics         []InterestTopic `json:"topics"`
}

type WatchedTechnology struct {
	ID                string     `json:"id,omitempty"`
	Technology        string     `json:"technology"`
	PackageName       string     `json:"packageName"`
	CurrentVersion    string     `json:"currentVersion"`
	VersionConstraint string     `json:"versionConstraint"`
	Status            string     `json:"status"`
	Source            string     `json:"source"`
	LastVerifiedAt    *time.Time `json:"lastVerifiedAt,omitempty"`
}

type OwnerSettings struct {
	Timezone             string `json:"timezone"`
	QuietHoursStart      string `json:"quietHoursStart"`
	QuietHoursEnd        string `json:"quietHoursEnd"`
	CriticalAlertsBypass bool   `json:"criticalAlertsBypass"`
	MonthlySoftBudgetUSD string `json:"monthlySoftBudgetUsd"`
	MonthlyHardBudgetUSD string `json:"monthlyHardBudgetUsd"`
	RawRetentionDays     int    `json:"rawRetentionDays"`
	AuditRetentionDays   int    `json:"auditRetentionDays"`
	Version              int64  `json:"version"`
}

type ScheduleDefinition struct {
	ID                     string     `json:"id"`
	ScheduleType           string     `json:"scheduleType"`
	Timezone               string     `json:"timezone"`
	LocalTime              string     `json:"localTime"`
	DaysOfWeek             []int16    `json:"daysOfWeek"`
	Enabled                bool       `json:"enabled"`
	CatchupPolicy          string     `json:"catchupPolicy"`
	CatchupGraceMinutes    int        `json:"catchupGraceMinutes"`
	NextDueAt              time.Time  `json:"nextDueAt"`
	SkipNextAt             *time.Time `json:"skipNextAt,omitempty"`
	PausedAt               *time.Time `json:"pausedAt,omitempty"`
	PausedUntil            *time.Time `json:"pausedUntil,omitempty"`
	WeekendMode            string     `json:"weekendMode"`
	MaximumItems           int        `json:"maximumItems"`
	MinimumScore           float64    `json:"minimumScore"`
	IncludeComingSoon      bool       `json:"includeComingSoon"`
	IncludeRadarCandidates bool       `json:"includeRadarCandidates"`
	IncludeLaterReminders  bool       `json:"includeLaterReminders"`
	EmptyBehavior          string     `json:"emptyBehavior"`
	Channels               []string   `json:"channels"`
	Version                int64      `json:"version"`
}

type SettingsSnapshot struct {
	GeneratedAt  time.Time            `json:"generatedAt"`
	Owner        OwnerSettings        `json:"owner"`
	Profile      InterestProfile      `json:"profile"`
	Technologies []WatchedTechnology  `json:"technologies"`
	Schedules    []ScheduleDefinition `json:"schedules"`
}

type UpdateSettingsRequest struct {
	UserID               string
	ExpectedVersion      int64
	ProfileName          string
	ProfileSummary       string
	Topics               []InterestTopic
	Technologies         []WatchedTechnology
	Timezone             string
	QuietHoursStart      string
	QuietHoursEnd        string
	CriticalAlertsBypass bool
	MonthlySoftBudgetUSD string
	MonthlyHardBudgetUSD string
	RawRetentionDays     int
	AuditRetentionDays   int
}

type UpdateScheduleRequest struct {
	UserID                 string
	ScheduleID             string
	ExpectedVersion        int64
	Timezone               string
	LocalTime              string
	DaysOfWeek             []int16
	Enabled                bool
	CatchupPolicy          string
	CatchupGraceMinutes    int
	WeekendMode            string
	MaximumItems           int
	MinimumScore           float64
	IncludeComingSoon      bool
	IncludeRadarCandidates bool
	IncludeLaterReminders  bool
	EmptyBehavior          string
	Channels               []string
	NextDueAt              time.Time
}

type ScheduleActionRequest struct {
	UserID         string
	ScheduleID     string
	Action         string
	Reason         string
	IdempotencyKey string
	Deliver        bool
	PausedUntil    *time.Time
	NextDueAt      *time.Time
}

type ScheduleActionResult struct {
	Message      string             `json:"message"`
	OccurrenceID string             `json:"occurrenceId,omitempty"`
	Schedule     ScheduleDefinition `json:"schedule"`
}

type SchedulePreview struct {
	ScheduleID       string    `json:"scheduleId"`
	ScheduleType     string    `json:"scheduleType"`
	LocalDate        string    `json:"localDate"`
	NextRunAt        time.Time `json:"nextRunAt"`
	CandidateCount   int       `json:"candidateCount"`
	MaximumItems     int       `json:"maximumItems"`
	ExternalDelivery bool      `json:"externalDelivery"`
	Explanation      string    `json:"explanation"`
}

type QueueDepth struct {
	Queue     string `json:"queue"`
	Available int64  `json:"available"`
	Running   int64  `json:"running"`
	Retryable int64  `json:"retryable"`
	Scheduled int64  `json:"scheduled"`
	Completed int64  `json:"completed"`
	Discarded int64  `json:"discarded"`
	Cancelled int64  `json:"cancelled"`
}

type StuckJob struct {
	ID          int64      `json:"id"`
	Queue       string     `json:"queue"`
	Kind        string     `json:"kind"`
	Attempt     int        `json:"attempt"`
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
}

type SourceErrorBudget struct {
	SourceID    string  `json:"sourceId"`
	SourceName  string  `json:"sourceName"`
	Attempts    int64   `json:"attempts"`
	Failures    int64   `json:"failures"`
	FailureRate float64 `json:"failureRate"`
	State       string  `json:"state"`
}

type DeploymentMetadata struct {
	Environment string `json:"environment"`
	Version     string `json:"version"`
	GitSHA      string `json:"gitSha"`
}

type RestoreStatus struct {
	State       string `json:"state"`
	Explanation string `json:"explanation"`
}

type OperationsSnapshot struct {
	GeneratedAt             time.Time            `json:"generatedAt"`
	Queues                  []QueueDepth         `json:"queues"`
	StuckJobs               []StuckJob           `json:"stuckJobs"`
	OpenAIBackgroundPending int64                `json:"openaiBackgroundPending"`
	DeliveryAttempts        int64                `json:"deliveryAttempts"`
	SourceErrorBudgets      []SourceErrorBudget  `json:"sourceErrorBudgets"`
	Deployment              DeploymentMetadata   `json:"deployment"`
	Restore                 RestoreStatus        `json:"restore"`
	SchedulerLastSuccess    *time.Time           `json:"schedulerLastSuccess,omitempty"`
	OldestOverdueOccurrence *time.Time           `json:"oldestOverdueOccurrence,omitempty"`
	Schedules               []ScheduleDefinition `json:"schedules"`
	Occurrences             []ScheduleOccurrence `json:"occurrences"`
}

type ScheduleOccurrence struct {
	ID             string     `json:"id"`
	ScheduleID     string     `json:"scheduleId"`
	ScheduleType   string     `json:"scheduleType"`
	ScheduledFor   time.Time  `json:"scheduledFor"`
	LocalDate      string     `json:"localDate"`
	State          string     `json:"state"`
	TriggerType    string     `json:"triggerType"`
	IdempotencyKey string     `json:"idempotencyKey"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	ErrorCode      string     `json:"errorCode,omitempty"`
}
