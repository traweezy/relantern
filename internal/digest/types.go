package digest

import (
	"errors"
	"time"
)

var (
	ErrConflict = errors.New("digest state conflict")
	ErrInvalid  = errors.New("invalid digest request")
	ErrNotFound = errors.New("digest not found")
)

const (
	ChannelDashboard = "dashboard"
	ChannelDiscord   = "discord"
	ChannelEmail     = "email"
)

type DigestCandidate struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	StoryID   string         `json:"storyId,omitempty"`
	ItemID    string         `json:"itemId,omitempty"`
	RadarID   string         `json:"radarId,omitempty"`
	Headline  string         `json:"headline"`
	Summary   string         `json:"summary"`
	Action    string         `json:"action"`
	Signal    string         `json:"signal"`
	Category  string         `json:"category"`
	Reason    string         `json:"reason"`
	AppPath   string         `json:"appPath"`
	SourceURL string         `json:"sourceUrl"`
	Score     float64        `json:"score"`
	Observed  time.Time      `json:"observedAt"`
	Evidence  map[string]any `json:"evidence"`
}

type StorySignals struct {
	Confidence     string
	LifecycleState string
	Signal         string
	SourceTier     string
	Status         string
	ObservedAt     time.Time
	WindowStart    time.Time
	WindowEnd      time.Time
}

type RadarSignals struct {
	CurrentState string
	Misleading   bool
	ObservedAt   time.Time
}

type RenderInput struct {
	Channel          string
	LocalDate        string
	WindowStart      time.Time
	WindowEnd        time.Time
	GeneratedAt      time.Time
	ExecutiveSummary string
	Items            []DigestCandidate
}

type DigestRenderedItem struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Headline  string  `json:"headline"`
	Summary   string  `json:"summary"`
	Action    string  `json:"action"`
	Signal    string  `json:"signal"`
	Reason    string  `json:"reason"`
	AppPath   string  `json:"appPath"`
	SourceURL string  `json:"sourceUrl"`
	Score     float64 `json:"score"`
}

type DigestPayload struct {
	Channel          string               `json:"channel"`
	Title            string               `json:"title"`
	ExecutiveSummary string               `json:"executiveSummary"`
	LocalDate        string               `json:"localDate"`
	WindowStart      time.Time            `json:"windowStart"`
	WindowEnd        time.Time            `json:"windowEnd"`
	GeneratedAt      time.Time            `json:"generatedAt"`
	Items            []DigestRenderedItem `json:"items"`
}

type DigestRecord struct {
	ID                     string            `json:"id"`
	OccurrenceID           string            `json:"occurrenceId"`
	LocalDate              string            `json:"localDate"`
	WindowStart            time.Time         `json:"windowStart"`
	WindowEnd              time.Time         `json:"windowEnd"`
	Channel                string            `json:"channel"`
	State                  string            `json:"state"`
	ItemLimit              int               `json:"itemLimit"`
	MinimumScore           float64           `json:"minimumScore"`
	EmptyBehavior          string            `json:"emptyBehavior"`
	ExecutiveSummary       string            `json:"executiveSummary"`
	Rendered               DigestPayload     `json:"rendered"`
	ProviderIdempotencyKey string            `json:"providerIdempotencyKey"`
	GeneratedAt            time.Time         `json:"generatedAt"`
	CompletedAt            *time.Time        `json:"completedAt,omitempty"`
	ErrorCode              string            `json:"errorCode,omitempty"`
	AttemptCount           int               `json:"attemptCount"`
	Items                  []DigestCandidate `json:"items"`
}

type DigestSnapshot struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Digests     []DigestRecord `json:"digests"`
}

type DigestPreview struct {
	ScheduleID     string        `json:"scheduleId"`
	LocalDate      string        `json:"localDate"`
	CandidateCount int           `json:"candidateCount"`
	MaximumItems   int           `json:"maximumItems"`
	Channels       []string      `json:"channels"`
	Rendered       DigestPayload `json:"rendered"`
}

type Delivery struct {
	DigestID       string
	OccurrenceID   string
	Channel        string
	Attempt        int
	IdempotencyKey string
	LocalDate      string
	ScheduledFor   time.Time
	Payload        DigestPayload
	PayloadSHA256  []byte
}

type Receipt struct {
	ProviderID string
}
