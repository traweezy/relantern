package research

import (
	"context"
	"time"

	"github.com/traweezy/relantern/internal/extraction"
)

const (
	Purpose                    = "research_synthesis"
	SchemaName                 = "relantern_research_synthesis"
	DefaultModelID             = "gpt-5.6-terra"
	DefaultReasoning           = "medium"
	DefaultVerbosity           = "low"
	DefaultMaximumOutputTokens = 8_192
	DefaultMaximumToolCalls    = 4
	MaximumProviderAttempts    = 5
	MaximumSchemaAttempts      = 2
	MaximumClaims              = 100
	MaximumSources             = 100
	MaximumInputBytes          = 200_000
)

type ClaimFact struct {
	ID                string `json:"claim_id"`
	ItemID            string `json:"item_id"`
	RevisionID        string `json:"revision_id"`
	ClaimType         string `json:"claim_type"`
	ClaimText         string `json:"claim_text"`
	NormalizedValue   string `json:"normalized_value"`
	Confidence        string `json:"confidence"`
	Material          bool   `json:"material"`
	VerificationState string `json:"verification_state"`
	SourceURL         string `json:"source_url"`
	SourceTier        string `json:"source_tier"`
}

type Assertion struct {
	Text       string   `json:"text"`
	Material   bool     `json:"material"`
	ClaimIDs   []string `json:"claim_ids"`
	SourceURLs []string `json:"source_urls"`
}

type Output struct {
	Headline          string      `json:"headline"`
	Summary           string      `json:"summary"`
	WhyItMatters      string      `json:"why_it_matters"`
	RecommendedAction string      `json:"recommended_action"`
	Confidence        string      `json:"confidence"`
	Assertions        []Assertion `json:"assertions"`
	Uncertainties     []string    `json:"uncertainties"`
}

type Source struct {
	URL    string
	Domain string
}

type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ToolCalls         int
}

type ProviderRequest struct {
	ModelID         string
	Reasoning       string
	Verbosity       string
	MaxOutputTokens int
	MaxToolCalls    int
	Prompt          string
	Input           string
	AllowedDomains  []string
	BlockedDomains  []string
	Background      bool
}

type ProviderResponse struct {
	ID      string
	Status  string
	Output  string
	Sources []Source
	Usage   Usage
}

type Provider interface {
	Start(context.Context, ProviderRequest) (ProviderResponse, error)
	Get(context.Context, string) (ProviderResponse, error)
}

type ProcessRequest struct {
	ClusterID string
}

type PollRequest struct {
	RunID      string
	ResponseID string
}

type ProcessResult struct {
	RunID            string
	ProviderID       string
	AssertionCount   int
	AlreadyCompleted bool
	Pending          bool
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
	RunID              string
	ClusterID          string
	ItemID             string
	RevisionID         string
	Title              string
	InputHash          []byte
	Claims             []ClaimFact
	Model              ModelConfig
	Prompt             PromptVersion
	State              string
	ErrorCode          string
	ProviderResponseID string
	Reservation        AttemptReservation
	SchemaFailures     int
	Obsolete           bool
}

type AttemptReservation struct {
	ID                 int64
	Number             int
	MaximumToolCalls   int
	BudgetSoftExceeded bool
}

type Completion struct {
	Output     Output
	ProviderID string
	Sources    []Source
	Usage      Usage
}

type Config struct {
	ExpectedModelID         string
	ExpectedReasoning       string
	ExpectedVerbosity       string
	ExpectedMaxOutputTokens int
	MaximumToolCalls        int
	AllowedDomains          []string
	BlockedDomains          []string
	Background              bool
	DailyWebSearchLimit     int
	MonthlySoftUSD          extraction.USD
	MonthlyHardUSD          extraction.USD
}

type RunRepository interface {
	Prepare(context.Context, ProcessRequest, time.Time) (PreparedRun, error)
	LoadPending(context.Context, PollRequest) (PreparedRun, error)
	ReserveAttempt(context.Context, PreparedRun, int64, Config, time.Time) (AttemptReservation, error)
	AttachProvider(context.Context, PreparedRun, AttemptReservation, ProviderResponse, time.Time) error
	RecordAttempt(context.Context, PreparedRun, AttemptReservation, ProviderResponse, string, time.Time) error
	Complete(context.Context, PreparedRun, Completion, time.Time) (ProcessResult, error)
	Fail(context.Context, PreparedRun, string, time.Time) error
}
