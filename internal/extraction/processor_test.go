package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/contracts/schemas"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/prompts"
)

type processorRepository struct {
	prepared       PreparedRun
	prepareError   error
	reserveError   error
	recordError    error
	failError      error
	reservations   int
	recorded       []string
	failedCode     string
	completion     Completion
	completeResult ProcessResult
}

func (repository *processorRepository) Prepare(_ context.Context, _ ProcessRequest, _ time.Time) (PreparedRun, error) {
	return repository.prepared, repository.prepareError
}

func (repository *processorRepository) ReserveAttempt(
	_ context.Context,
	_ PreparedRun,
	_ int64,
	_ USD,
	_ USD,
	_ time.Time,
) (AttemptReservation, error) {
	repository.reservations++
	return AttemptReservation{ID: int64(repository.reservations), Number: repository.reservations}, repository.reserveError
}

func (repository *processorRepository) RecordAttempt(
	_ context.Context,
	_ PreparedRun,
	_ AttemptReservation,
	_ ProviderResponse,
	errorCode string,
	_ time.Time,
) error {
	repository.recorded = append(repository.recorded, errorCode)
	return repository.recordError
}

func (repository *processorRepository) Complete(
	_ context.Context,
	_ PreparedRun,
	completion Completion,
	_ time.Time,
) (ProcessResult, error) {
	repository.completion = completion
	if repository.completeResult.RunID == "" {
		repository.completeResult = ProcessResult{RunID: repository.prepared.RunID, ClaimCount: len(completion.Output.Claims), NeedsReview: completion.NeedsReview}
	}
	return repository.completeResult, nil
}

func (repository *processorRepository) Fail(_ context.Context, _ PreparedRun, errorCode string, _ time.Time) error {
	repository.failedCode = errorCode
	return repository.failError
}

type processorReader struct {
	payload []byte
	err     error
}

func (reader processorReader) Read(_ context.Context, _ string, maximumBytes int64) ([]byte, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	if int64(len(reader.payload)) > maximumBytes {
		return nil, fmt.Errorf("object exceeds %d bytes", maximumBytes)
	}
	return append([]byte(nil), reader.payload...), nil
}

type processorProvider struct {
	responses []ProviderResponse
	errors    []error
	requests  []ProviderRequest
}

func (provider *processorProvider) Extract(_ context.Context, request ProviderRequest) (ProviderResponse, error) {
	provider.requests = append(provider.requests, request)
	index := len(provider.requests) - 1
	var response ProviderResponse
	if index < len(provider.responses) {
		response = provider.responses[index]
	}
	if index < len(provider.errors) {
		return response, provider.errors[index]
	}
	return response, nil
}

func TestProcessorPersistsGroundedExtraction(t *testing.T) {
	t.Parallel()

	content := []byte("Go 1.27 is a stable release on 2026-08-29.")
	repository := &processorRepository{prepared: validPreparedRun(content, "T0")}
	provider := &processorProvider{responses: []ProviderResponse{{
		ID:     "resp_test_success",
		Output: encodedOutput(validProcessorOutput()),
		Usage:  Usage{InputTokens: 200, CachedInputTokens: 40, OutputTokens: 80},
	}}}
	processor := newTestProcessor(t, repository, processorReader{payload: content}, provider)
	result, err := processor.Process(context.Background(), ProcessRequest{
		ItemID: repository.prepared.ItemID, RevisionID: repository.prepared.RevisionID,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.ClaimCount != 1 || result.NeedsReview || repository.reservations != 1 ||
		len(repository.recorded) != 1 || repository.recorded[0] != "" ||
		repository.completion.ProviderID != "resp_test_success" ||
		repository.completion.CompletedState != "completed" {
		t.Fatalf("unexpected extraction result = %+v, repository = %+v", result, repository)
	}
	if len(provider.requests) != 1 || provider.requests[0].ModelID != DefaultFastModelID ||
		provider.requests[0].Prompt != prompts.StructuredExtractionV1() ||
		provider.requests[0].MaxOutputTokens != DefaultMaximumOutputTokens {
		t.Fatalf("provider request = %+v", provider.requests)
	}
	if repository.completion.Spans[0].Text != string(content) {
		t.Fatalf("persisted evidence spans = %+v", repository.completion.Spans)
	}
}

func TestProcessorRetriesSchemaOnceAndRequiresReviewForUntrustedMaterialClaims(t *testing.T) {
	t.Parallel()

	content := []byte("A community post claims Go 1.27 is a stable release.")
	repository := &processorRepository{prepared: validPreparedRun(content, "T2")}
	provider := &processorProvider{responses: []ProviderResponse{
		{ID: "resp_bad", Output: `{"event_type":"execute_tools"}`, Usage: Usage{InputTokens: 100, OutputTokens: 10}},
		{ID: "resp_good", Output: encodedOutput(validProcessorOutput()), Usage: Usage{InputTokens: 100, OutputTokens: 60}},
	}}
	processor := newTestProcessor(t, repository, processorReader{payload: content}, provider)
	result, err := processor.Process(context.Background(), ProcessRequest{
		ItemID: repository.prepared.ItemID, RevisionID: repository.prepared.RevisionID,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if !result.NeedsReview || repository.completion.CompletedState != "needs_review" ||
		repository.reservations != 2 || len(repository.recorded) != 2 ||
		repository.recorded[0] != "schema_invalid" || repository.recorded[1] != "" {
		t.Fatalf("retry result = %+v, repository = %+v", result, repository)
	}
}

func TestProcessorMovesSecondSchemaFailureToReview(t *testing.T) {
	t.Parallel()

	content := []byte("Stable evidence.")
	repository := &processorRepository{prepared: validPreparedRun(content, "T0")}
	provider := &processorProvider{responses: []ProviderResponse{
		{ID: "resp_bad_1", Output: `{}`, Usage: Usage{InputTokens: 10, OutputTokens: 2}},
		{ID: "resp_bad_2", Output: `{}`, Usage: Usage{InputTokens: 10, OutputTokens: 2}},
	}}
	processor := newTestProcessor(t, repository, processorReader{payload: content}, provider)
	_, err := processor.Process(context.Background(), ProcessRequest{
		ItemID: repository.prepared.ItemID, RevisionID: repository.prepared.RevisionID,
	})
	if !errors.Is(err, ErrSchemaInvalid) || !Permanent(err) || repository.failedCode != "schema_invalid" ||
		repository.reservations != MaximumSchemaAttempts || len(repository.recorded) != MaximumSchemaAttempts {
		t.Fatalf("Process() error = %v, repository = %+v", err, repository)
	}
}

func TestProcessorClassifiesProviderFailureAsRetryableAndPreservesPersistenceErrors(t *testing.T) {
	t.Parallel()

	content := []byte("Stable evidence.")
	providerFailure := errors.New("provider unavailable")
	recordFailure := errors.New("attempt ledger unavailable")
	failFailure := errors.New("run state unavailable")
	repository := &processorRepository{
		prepared:    validPreparedRun(content, "T0"),
		recordError: recordFailure,
		failError:   failFailure,
	}
	provider := &processorProvider{errors: []error{providerFailure}}
	processor := newTestProcessor(t, repository, processorReader{payload: content}, provider)
	_, err := processor.Process(context.Background(), ProcessRequest{
		ItemID: repository.prepared.ItemID, RevisionID: repository.prepared.RevisionID,
	})
	if !errors.Is(err, providerFailure) || !errors.Is(err, recordFailure) || !errors.Is(err, failFailure) ||
		Permanent(err) || repository.failedCode != "provider_error" {
		t.Fatalf("Process() error = %v, repository = %+v", err, repository)
	}
}

func TestProcessorRejectsInvalidOrStaleBoundariesBeforeProviderUse(t *testing.T) {
	t.Parallel()

	content := []byte("Stable evidence.")
	base := validPreparedRun(content, "T0")
	tests := []struct {
		name          string
		mutate        func(*PreparedRun, *processorReader)
		wantError     error
		wantFailCode  string
		wantObsolete  bool
		wantCompleted bool
	}{
		{name: "obsolete", mutate: func(run *PreparedRun, _ *processorReader) { run.Obsolete = true }, wantObsolete: true},
		{name: "completed", mutate: func(run *PreparedRun, _ *processorReader) { run.State = "completed" }, wantCompleted: true},
		{name: "old", mutate: func(run *PreparedRun, _ *processorReader) {
			run.FirstSeenAt = run.FirstSeenAt.Add(-366 * 24 * time.Hour)
		}, wantError: ErrInvalidTarget, wantFailCode: "document_too_old"},
		{name: "too large", mutate: func(run *PreparedRun, _ *processorReader) { run.NormalizedBytes = MaximumNormalizedBytes + 1 }, wantError: ErrInvalidTarget, wantFailCode: "input_too_large"},
		{name: "registry drift", mutate: func(run *PreparedRun, _ *processorReader) { run.Model.ModelID = "unreviewed-model" }, wantError: ErrConfigurationDrift, wantFailCode: "configuration_drift"},
		{name: "hash mismatch", mutate: func(run *PreparedRun, _ *processorReader) { run.NormalizedHash = make([]byte, sha256.Size) }, wantError: ErrRevisionIntegrity, wantFailCode: "revision_integrity"},
		{name: "invalid utf8", mutate: func(run *PreparedRun, reader *processorReader) {
			reader.payload = []byte{0xff}
			digest := sha256.Sum256(reader.payload)
			run.NormalizedHash = digest[:]
			run.NormalizedBytes = int64(len(reader.payload))
		}, wantError: ErrRevisionIntegrity, wantFailCode: "revision_integrity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := base
			run.NormalizedHash = append([]byte(nil), base.NormalizedHash...)
			reader := processorReader{payload: append([]byte(nil), content...)}
			test.mutate(&run, &reader)
			repository := &processorRepository{prepared: run}
			provider := &processorProvider{}
			processor := newTestProcessor(t, repository, reader, provider)
			result, err := processor.Process(context.Background(), ProcessRequest{ItemID: run.ItemID, RevisionID: run.RevisionID})
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("Process() error = %v, want %v", err, test.wantError)
			}
			if test.wantError == nil && err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if result.Obsolete != test.wantObsolete || result.AlreadyCompleted != test.wantCompleted ||
				repository.failedCode != test.wantFailCode || len(provider.requests) != 0 {
				t.Fatalf("result = %+v, repository = %+v, provider calls = %d", result, repository, len(provider.requests))
			}
		})
	}
}

func TestProcessorValidatesConstructionAndTargetIDs(t *testing.T) {
	t.Parallel()

	content := []byte("evidence")
	repository := &processorRepository{prepared: validPreparedRun(content, "T0")}
	provider := &processorProvider{}
	validConfig := testProcessorConfig()
	if _, err := NewProcessor(nil, processorReader{payload: content}, provider, clock.NewFixed(time.Now()), validConfig); err == nil {
		t.Fatal("NewProcessor() accepted a nil repository")
	}
	invalidBudget := validConfig
	invalidBudget.MonthlySoftUSD = "0"
	if _, err := NewProcessor(repository, processorReader{payload: content}, provider, clock.NewFixed(time.Now()), invalidBudget); err == nil {
		t.Fatal("NewProcessor() accepted an invalid budget")
	}
	processor := newTestProcessor(t, repository, processorReader{payload: content}, provider)
	if _, err := processor.Process(context.Background(), ProcessRequest{ItemID: "not-a-uuid", RevisionID: uuid.NewString()}); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("Process() invalid item error = %v", err)
	}
	if _, err := processor.Process(context.Background(), ProcessRequest{ItemID: uuid.NewString(), RevisionID: "not-a-uuid"}); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("Process() invalid revision error = %v", err)
	}
	if ErrorCode(ErrBudgetExceeded) != "budget_exceeded" || ErrorCode(errors.New("network")) != "provider_error" {
		t.Fatal("ErrorCode() classification drifted")
	}
}

func newTestProcessor(
	t *testing.T,
	repository RunRepository,
	reader processorReader,
	provider Provider,
) *Processor {
	t.Helper()
	processor, err := NewProcessor(
		repository,
		reader,
		provider,
		clock.NewFixed(time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)),
		testProcessorConfig(),
	)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	return processor
}

func testProcessorConfig() Config {
	return Config{
		ExpectedModelID: DefaultFastModelID, ExpectedReasoning: DefaultFastReasoning,
		ExpectedVerbosity: DefaultVerbosity, ExpectedMaxOutputTokens: DefaultMaximumOutputTokens,
		MonthlySoftUSD: "25.00", MonthlyHardUSD: "50.00", MaximumAge: 365 * 24 * time.Hour,
	}
}

func validPreparedRun(content []byte, sourceTier string) PreparedRun {
	digest := sha256.Sum256(content)
	promptDigest := sha256.Sum256([]byte(prompts.StructuredExtractionV1()))
	schemaDigest := sha256.Sum256(schemas.StructuredExtractionV1())
	return PreparedRun{
		RunID: uuid.NewString(), ItemID: uuid.NewString(), RevisionID: uuid.NewString(),
		SourceID: "go-blog", SourceTier: sourceTier, Title: "Go release",
		ObjectKey: "normalized/go-blog/content.txt", NormalizedHash: digest[:],
		NormalizedBytes: int64(len(content)), Outline: []byte(`[]`),
		FirstSeenAt: time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC), State: "pending",
		Model: ModelConfig{
			ID: uuid.NewString(), ModelID: DefaultFastModelID, Reasoning: DefaultFastReasoning,
			Verbosity: DefaultVerbosity, MaxOutputTokens: DefaultMaximumOutputTokens,
		},
		Prompt: PromptVersion{
			ID: uuid.NewString(), SemanticVersion: prompts.StructuredExtractionVersion,
			PromptSHA256: promptDigest[:], SchemaVersion: schemas.StructuredExtractionVersion,
			SchemaSHA256: schemaDigest[:],
		},
	}
}

func validProcessorOutput() Output {
	return Output{
		TopicIDs: []string{"go"}, EventType: "release", LifecycleState: "stable",
		DeepExtractionJustified: false,
		Claims: []Claim{{
			ClaimType: "release", ClaimText: "Go 1.27 is a stable release.", NormalizedValue: "1.27",
			Confidence: "high", Material: true, EvidenceSpanIDs: []string{"span_0001"},
		}},
		Uncertainties: []string{},
	}
}

func encodedOutput(output Output) string {
	encoded, err := json.Marshal(output)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
