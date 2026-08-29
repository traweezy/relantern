package research

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/traweezy/relantern/contracts/schemas"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/prompts"
)

type processorRepository struct {
	prepared       PreparedRun
	reservation    AttemptReservation
	completed      Completion
	recorded       ProviderResponse
	recordedCode   string
	failedCode     string
	attached       bool
	completeCalled bool
	loadState      string
}

func (repository *processorRepository) Prepare(context.Context, ProcessRequest, time.Time) (PreparedRun, error) {
	return repository.prepared, nil
}

func (repository *processorRepository) LoadPending(context.Context, PollRequest) (PreparedRun, error) {
	prepared := repository.prepared
	prepared.ProviderResponseID = "resp_test"
	prepared.State = "running"
	if repository.loadState != "" {
		prepared.State = repository.loadState
	}
	prepared.Reservation = repository.reservation
	return prepared, nil
}

func (repository *processorRepository) ReserveAttempt(
	context.Context,
	PreparedRun,
	int64,
	Config,
	time.Time,
) (AttemptReservation, error) {
	return repository.reservation, nil
}

func (repository *processorRepository) AttachProvider(
	_ context.Context,
	_ PreparedRun,
	_ AttemptReservation,
	_ ProviderResponse,
	_ time.Time,
) error {
	repository.attached = true
	return nil
}

func (repository *processorRepository) RecordAttempt(
	_ context.Context,
	_ PreparedRun,
	_ AttemptReservation,
	response ProviderResponse,
	errorCode string,
	_ time.Time,
) error {
	repository.recorded = response
	repository.recordedCode = errorCode
	return nil
}

func (repository *processorRepository) Complete(
	_ context.Context,
	prepared PreparedRun,
	completion Completion,
	_ time.Time,
) (ProcessResult, error) {
	repository.completed = completion
	repository.completeCalled = true
	return ProcessResult{RunID: prepared.RunID, ProviderID: completion.ProviderID, AssertionCount: len(completion.Output.Assertions)}, nil
}

func (repository *processorRepository) Fail(_ context.Context, _ PreparedRun, errorCode string, _ time.Time) error {
	repository.failedCode = errorCode
	return nil
}

type processorProvider struct {
	started ProviderRequest
	start   ProviderResponse
	get     ProviderResponse
	err     error
}

func (provider *processorProvider) Start(_ context.Context, request ProviderRequest) (ProviderResponse, error) {
	provider.started = request
	return provider.start, provider.err
}

func (provider *processorProvider) Get(_ context.Context, _ string) (ProviderResponse, error) {
	return provider.get, provider.err
}

func TestProcessorStartsAndPollsBoundedBackgroundResearch(t *testing.T) {
	prepared := validPreparedRun(t)
	repository := &processorRepository{
		prepared:    prepared,
		reservation: AttemptReservation{ID: 7, Number: 1, MaximumToolCalls: 4},
	}
	provider := &processorProvider{
		start: ProviderResponse{ID: "resp_test", Status: "queued"},
		get:   validCompletedProviderResponse(t),
	}
	processor, err := NewProcessor(repository, provider, clock.NewFixed(time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)), testConfig())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	started, err := processor.Process(context.Background(), ProcessRequest{ClusterID: prepared.ClusterID})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if !started.Pending || !repository.attached || !provider.started.Background ||
		provider.started.MaxToolCalls != 4 || len(provider.started.AllowedDomains) != 2 {
		t.Fatalf("started = %+v, request = %+v, repository = %+v", started, provider.started, repository)
	}
	completed, err := processor.Poll(context.Background(), PollRequest{RunID: prepared.RunID, ResponseID: "resp_test"})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if completed.AssertionCount != 1 || !repository.completeCalled || repository.recordedCode != "" ||
		repository.completed.Usage.ToolCalls != 1 {
		t.Fatalf("completed = %+v, repository = %+v", completed, repository)
	}
}

func TestProcessorRetriesInvalidSchemaOnceThenRequiresReview(t *testing.T) {
	prepared := validPreparedRun(t)
	repository := &processorRepository{
		prepared:    prepared,
		reservation: AttemptReservation{ID: 7, Number: 1, MaximumToolCalls: 4},
	}
	provider := &processorProvider{
		start: ProviderResponse{
			ID: "resp_test", Status: "completed", Output: `{"invented":true}`,
			Usage:   Usage{InputTokens: 10, OutputTokens: 10, ToolCalls: 1},
			Sources: []Source{{URL: "https://go.dev/doc/devel/release", Domain: "go.dev"}},
		},
	}
	processor, err := NewProcessor(repository, provider, clock.NewFixed(time.Now()), testConfig())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), ProcessRequest{ClusterID: prepared.ClusterID}); !errors.Is(err, ErrSchemaRetry) {
		t.Fatalf("Process() error = %v, want ErrSchemaRetry", err)
	}
	if repository.recordedCode != "schema_invalid" || repository.failedCode != "schema_invalid_retryable" {
		t.Fatalf("first invalid attempt = %+v", repository)
	}

	repository.failedCode = ""
	repository.reservation.Number = 2
	repository.prepared.SchemaFailures = 1
	if _, err := processor.Process(context.Background(), ProcessRequest{ClusterID: prepared.ClusterID}); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("second Process() error = %v, want ErrSchemaInvalid", err)
	}
	if repository.failedCode != "schema_invalid" || !Permanent(ErrSchemaInvalid) {
		t.Fatalf("second invalid attempt = %+v", repository)
	}
}

func TestProcessorRestartsFailedBackgroundRunWithNewResponse(t *testing.T) {
	prepared := validPreparedRun(t)
	repository := &processorRepository{
		prepared: prepared, loadState: "failed_retryable",
		reservation: AttemptReservation{ID: 8, Number: 2, MaximumToolCalls: 4},
	}
	provider := &processorProvider{start: ProviderResponse{ID: "resp_replacement", Status: "queued"}}
	processor, err := NewProcessor(repository, provider, clock.NewFixed(time.Now()), testConfig())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	result, err := processor.Poll(context.Background(), PollRequest{
		RunID: prepared.RunID, ResponseID: "resp_test",
	})
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if !result.Pending || result.ProviderID != "resp_replacement" || !repository.attached {
		t.Fatalf("replacement result = %+v, repository = %+v", result, repository)
	}
}

func TestProcessorFailsClosedOnRegistryDrift(t *testing.T) {
	prepared := validPreparedRun(t)
	prepared.Model.EnabledTools = []string{"web_search", "shell"}
	repository := &processorRepository{prepared: prepared, reservation: AttemptReservation{MaximumToolCalls: 4}}
	processor, err := NewProcessor(repository, &processorProvider{}, clock.NewFixed(time.Now()), testConfig())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), ProcessRequest{ClusterID: prepared.ClusterID}); !errors.Is(err, ErrConfigurationDrift) {
		t.Fatalf("Process() error = %v, want configuration drift", err)
	}
	if repository.failedCode != "configuration_drift" {
		t.Fatalf("failed code = %q", repository.failedCode)
	}
}

func validPreparedRun(t *testing.T) PreparedRun {
	t.Helper()
	claims := []ClaimFact{{
		ID:         "01994f45-6770-7b45-9f2c-7ce5aca44444",
		ItemID:     "01994f45-6770-7b45-9f2c-7ce5aca42222",
		RevisionID: "01994f45-6770-7b45-9f2c-7ce5aca43333",
		ClaimType:  "release", ClaimText: "Go 1.27 is released.", NormalizedValue: "1.27",
		Confidence: "high", Material: true, VerificationState: "verified_span",
		SourceURL: "https://go.dev/doc/devel/release", SourceTier: "T0",
	}}
	_, inputHash, err := EncodeValidatedFacts("Go release", "01994f45-6770-7b45-9f2c-7ce5aca41111", claims)
	if err != nil {
		t.Fatalf("EncodeValidatedFacts() error = %v", err)
	}
	promptHash := sha256.Sum256([]byte(prompts.ResearchSynthesisV1()))
	schemaHash := SchemaDigest()
	return PreparedRun{
		RunID:      "01994f45-6770-7b45-9f2c-7ce5aca45555",
		ClusterID:  "01994f45-6770-7b45-9f2c-7ce5aca41111",
		ItemID:     "01994f45-6770-7b45-9f2c-7ce5aca42222",
		RevisionID: "01994f45-6770-7b45-9f2c-7ce5aca43333",
		Title:      "Go release", InputHash: inputHash, Claims: claims, State: "pending",
		Model: ModelConfig{
			ID: "01994f45-6770-7b45-9f2c-7ce5aca46666", ModelID: DefaultModelID,
			Reasoning: DefaultReasoning, Verbosity: DefaultVerbosity,
			EnabledTools: []string{"web_search"}, MaxOutputTokens: DefaultMaximumOutputTokens,
		},
		Prompt: PromptVersion{
			ID:              "01994f45-6770-7b45-9f2c-7ce5aca47777",
			SemanticVersion: prompts.ResearchSynthesisVersion, PromptSHA256: promptHash[:],
			SchemaVersion: schemas.ResearchSynthesisVersion, SchemaSHA256: schemaHash[:],
		},
	}
}

func validCompletedProviderResponse(t *testing.T) ProviderResponse {
	t.Helper()
	output := Output{
		Headline: "Go release", Summary: "Go 1.27 is released.",
		WhyItMatters: "The watched toolchain changed.", RecommendedAction: "Review the release notes.",
		Confidence: "high", Assertions: []Assertion{{
			Text: "Go 1.27 is released.", Material: true,
			ClaimIDs:   []string{"01994f45-6770-7b45-9f2c-7ce5aca44444"},
			SourceURLs: []string{"https://go.dev/doc/devel/release"},
		}}, Uncertainties: []string{},
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return ProviderResponse{
		ID: "resp_test", Status: "completed", Output: string(encoded),
		Sources: []Source{{URL: "https://go.dev/doc/devel/release", Domain: "go.dev"}},
		Usage:   Usage{InputTokens: 100, CachedInputTokens: 10, OutputTokens: 50, ToolCalls: 1},
	}
}

func testConfig() Config {
	return Config{
		ExpectedModelID: DefaultModelID, ExpectedReasoning: DefaultReasoning,
		ExpectedVerbosity: DefaultVerbosity, ExpectedMaxOutputTokens: DefaultMaximumOutputTokens,
		MaximumToolCalls: 4, AllowedDomains: []string{"go.dev", "github.com"},
		BlockedDomains: []string{"gist.github.com"}, Background: true, DailyWebSearchLimit: 100,
		MonthlySoftUSD: extraction.USD("25.00"), MonthlyHardUSD: extraction.USD("50.00"),
	}
}
