package extraction

import (
	"context"
	"time"
)

const (
	Purpose                    = "structured_extraction"
	SchemaName                 = "relantern_structured_extraction"
	MaximumNormalizedBytes     = 100_000
	MaximumEvidenceSpanBytes   = 1_600
	MaximumEvidenceSpans       = 200
	MaximumClaims              = 50
	MaximumSchemaAttempts      = 2
	DefaultFastModelID         = "gpt-5.6-luna"
	DefaultFastReasoning       = "low"
	DefaultVerbosity           = "low"
	DefaultMaximumOutputTokens = 4_096
)

type Claim struct {
	ClaimType       string   `json:"claim_type"`
	ClaimText       string   `json:"claim_text"`
	NormalizedValue string   `json:"normalized_value"`
	Confidence      string   `json:"confidence"`
	Material        bool     `json:"material"`
	EvidenceSpanIDs []string `json:"evidence_span_ids"`
}

type Output struct {
	TopicIDs                []string `json:"topic_ids"`
	EventType               string   `json:"event_type"`
	LifecycleState          string   `json:"lifecycle_state"`
	DeepExtractionJustified bool     `json:"deep_extraction_justified"`
	Claims                  []Claim  `json:"claims"`
	Uncertainties           []string `json:"uncertainties"`
}

type EvidenceSpan struct {
	ID          string `json:"span_id"`
	SectionPath string `json:"section_path"`
	StartOffset int    `json:"start_offset"`
	EndOffset   int    `json:"end_offset"`
	Text        string `json:"text"`
}

type ProviderRequest struct {
	ModelID         string
	Reasoning       string
	Verbosity       string
	MaxOutputTokens int
	Prompt          string
	Input           string
}

type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

type ProviderResponse struct {
	ID     string
	Output string
	Usage  Usage
}

type Provider interface {
	Extract(context.Context, ProviderRequest) (ProviderResponse, error)
}

type ProcessRequest struct {
	ItemID     string
	RevisionID string
}

type ProcessResult struct {
	RunID            string
	ClaimCount       int
	AlreadyCompleted bool
	NeedsReview      bool
	Obsolete         bool
}

type ModelConfig struct {
	ID              string
	ModelID         string
	Reasoning       string
	Verbosity       string
	EnabledTools    []string
	MaxOutputTokens int
}

type PromptVersion struct {
	ID              string
	SemanticVersion string
	PromptSHA256    []byte
	SchemaVersion   string
	SchemaSHA256    []byte
}

type PreparedRun struct {
	RunID           string
	ItemID          string
	RevisionID      string
	SourceID        string
	SourceTier      string
	Title           string
	ObjectKey       string
	NormalizedHash  []byte
	NormalizedBytes int64
	Outline         []byte
	FirstSeenAt     time.Time
	Model           ModelConfig
	Prompt          PromptVersion
	State           string
	Obsolete        bool
}

type AttemptReservation struct {
	ID                 int64
	Number             int
	BudgetSoftExceeded bool
}

type Completion struct {
	Output         Output
	Spans          []EvidenceSpan
	ProviderID     string
	Usage          Usage
	NeedsReview    bool
	CompletedState string
}

type RunRepository interface {
	Prepare(context.Context, ProcessRequest, time.Time) (PreparedRun, error)
	ReserveAttempt(context.Context, PreparedRun, int64, USD, USD, time.Time) (AttemptReservation, error)
	RecordAttempt(context.Context, PreparedRun, AttemptReservation, ProviderResponse, string, time.Time) error
	Complete(context.Context, PreparedRun, Completion, time.Time) (ProcessResult, error)
	Fail(context.Context, PreparedRun, string, time.Time) error
}
