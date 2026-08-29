package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/contracts/schemas"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/prompts"
)

var (
	ErrInvalidTarget      = errors.New("research target is invalid")
	ErrConfigurationDrift = errors.New("research prompt, schema, model, or tool configuration drift")
	ErrSchemaInvalid      = errors.New("research output failed schema or evidence validation twice")
	ErrSchemaRetry        = errors.New("research output failed validation and will retry once")
	ErrProviderTerminal   = errors.New("background OpenAI research response ended unsuccessfully")
	ErrWebSearchLimit     = errors.New("OpenAI daily web-search limit is exhausted")
	ErrAttemptsExhausted  = errors.New("background OpenAI research exhausted its provider attempts")
)

type Processor struct {
	repository RunRepository
	provider   Provider
	clock      clock.Clock
	config     Config
}

func NewProcessor(repository RunRepository, provider Provider, configuredClock clock.Clock, config Config) (*Processor, error) {
	if repository == nil || provider == nil || configuredClock == nil {
		return nil, errors.New("research requires repository, provider, and clock dependencies")
	}
	if err := extraction.ValidateBudgetRange(config.MonthlySoftUSD, config.MonthlyHardUSD); err != nil {
		return nil, fmt.Errorf("invalid research budget configuration: %w", err)
	}
	if config.ExpectedModelID == "" || config.ExpectedReasoning == "" || config.ExpectedVerbosity == "" ||
		config.ExpectedMaxOutputTokens < 256 || config.MaximumToolCalls < 1 || config.MaximumToolCalls > 10 ||
		config.DailyWebSearchLimit < config.MaximumToolCalls || !config.Background {
		return nil, errors.New("research requires complete bounded background model and tool configuration")
	}
	if err := validateDomainFilters(config.AllowedDomains, config.BlockedDomains); err != nil {
		return nil, err
	}
	return &Processor{repository: repository, provider: provider, clock: configuredClock, config: config}, nil
}

func (processor *Processor) Process(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	if _, err := uuid.Parse(request.ClusterID); err != nil {
		return ProcessResult{}, fmt.Errorf("%w: cluster ID must be a UUID", ErrInvalidTarget)
	}
	prepared, err := processor.repository.Prepare(ctx, request, processor.clock.Now().UTC())
	if err != nil {
		return ProcessResult{}, err
	}
	if prepared.Obsolete || prepared.State == "obsolete" {
		return ProcessResult{RunID: prepared.RunID, Obsolete: true}, nil
	}
	if prepared.State == "completed" || prepared.State == "needs_review" {
		return ProcessResult{
			RunID: prepared.RunID, ProviderID: prepared.ProviderResponseID,
			AlreadyCompleted: true, NeedsReview: prepared.State == "needs_review",
		}, nil
	}
	if prepared.ProviderResponseID != "" && prepared.State == "running" {
		return ProcessResult{RunID: prepared.RunID, ProviderID: prepared.ProviderResponseID, Pending: true}, nil
	}
	if err := validateRegistry(prepared, processor.config); err != nil {
		return ProcessResult{}, processor.fail(ctx, prepared, "configuration_drift", err)
	}
	input, inputHash, err := EncodeValidatedFacts(prepared.Title, prepared.ClusterID, prepared.Claims)
	if err != nil {
		return ProcessResult{}, processor.fail(ctx, prepared, "invalid_claims", fmt.Errorf("%w: %v", ErrInvalidTarget, err))
	}
	if !bytes.Equal(inputHash, prepared.InputHash) {
		return ProcessResult{}, processor.fail(ctx, prepared, "claim_integrity", fmt.Errorf("%w: research input hash changed", ErrInvalidTarget))
	}
	prompt := prompts.ResearchSynthesisV1()
	estimatedInputTokens := estimateInputTokens(prompt, input, schemas.ResearchSynthesisV1())
	reservation, err := processor.repository.ReserveAttempt(
		ctx,
		prepared,
		estimatedInputTokens,
		processor.config,
		processor.clock.Now().UTC(),
	)
	if err != nil {
		return ProcessResult{}, err
	}
	response, err := processor.provider.Start(ctx, ProviderRequest{
		ModelID:         prepared.Model.ModelID,
		Reasoning:       prepared.Model.Reasoning,
		Verbosity:       prepared.Model.Verbosity,
		MaxOutputTokens: prepared.Model.MaxOutputTokens,
		MaxToolCalls:    processor.config.MaximumToolCalls,
		Prompt:          prompt,
		Input:           input,
		AllowedDomains:  processor.config.AllowedDomains,
		BlockedDomains:  processor.config.BlockedDomains,
		Background:      processor.config.Background,
	})
	if err != nil {
		return ProcessResult{}, processor.providerFailure(ctx, prepared, reservation, ProviderResponse{}, err)
	}
	if err := processor.repository.AttachProvider(ctx, prepared, reservation, response, processor.clock.Now().UTC()); err != nil {
		return ProcessResult{}, fmt.Errorf("persist background research response ID: %w", err)
	}
	if response.Status == "queued" || response.Status == "in_progress" {
		return ProcessResult{RunID: prepared.RunID, ProviderID: response.ID, Pending: true}, nil
	}
	return processor.finalize(ctx, prepared, reservation, response)
}

func (processor *Processor) Poll(ctx context.Context, request PollRequest) (ProcessResult, error) {
	if request.RunID != "" {
		if _, err := uuid.Parse(request.RunID); err != nil {
			return ProcessResult{}, fmt.Errorf("%w: run ID must be a UUID", ErrInvalidTarget)
		}
	}
	if request.ResponseID == "" || len(request.ResponseID) > 255 {
		return ProcessResult{}, fmt.Errorf("%w: response ID is required and bounded", ErrInvalidTarget)
	}
	prepared, err := processor.repository.LoadPending(ctx, request)
	if err != nil {
		return ProcessResult{}, err
	}
	if prepared.Obsolete || prepared.State == "obsolete" {
		return ProcessResult{RunID: prepared.RunID, ProviderID: prepared.ProviderResponseID, Obsolete: true}, nil
	}
	if prepared.State == "budget_blocked" {
		if prepared.ErrorCode == "web_search_limit" {
			return ProcessResult{}, ErrWebSearchLimit
		}
		return ProcessResult{}, extraction.ErrBudgetExceeded
	}
	if prepared.State == "completed" || prepared.State == "needs_review" {
		return ProcessResult{
			RunID: prepared.RunID, ProviderID: prepared.ProviderResponseID,
			AlreadyCompleted: true, NeedsReview: prepared.State == "needs_review",
		}, nil
	}
	if prepared.State == "failed_retryable" {
		return processor.Process(ctx, ProcessRequest{ClusterID: prepared.ClusterID})
	}
	if err := validateRegistry(prepared, processor.config); err != nil {
		return ProcessResult{}, processor.fail(ctx, prepared, "configuration_drift", err)
	}
	response, err := processor.provider.Get(ctx, request.ResponseID)
	if err != nil {
		return ProcessResult{}, err
	}
	if response.ID != prepared.ProviderResponseID || response.ID != request.ResponseID {
		return ProcessResult{}, processor.fail(ctx, prepared, "provider_id_mismatch", fmt.Errorf("%w: provider response ID mismatch", ErrProviderTerminal))
	}
	if response.Status == "queued" || response.Status == "in_progress" {
		return ProcessResult{RunID: prepared.RunID, ProviderID: response.ID, Pending: true}, nil
	}
	return processor.finalize(ctx, prepared, prepared.Reservation, response)
}

func (processor *Processor) finalize(
	ctx context.Context,
	prepared PreparedRun,
	reservation AttemptReservation,
	response ProviderResponse,
) (ProcessResult, error) {
	if response.Status != "completed" {
		return ProcessResult{}, processor.providerFailure(
			ctx,
			prepared,
			reservation,
			response,
			fmt.Errorf("%w: %s", ErrProviderTerminal, response.Status),
		)
	}
	validationError := error(nil)
	if response.Usage.ToolCalls > reservation.MaximumToolCalls {
		validationError = fmt.Errorf("web-search calls %d exceed reserved maximum %d", response.Usage.ToolCalls, reservation.MaximumToolCalls)
	}
	var output Output
	if validationError == nil {
		output, validationError = DecodeAndValidate(
			response.Output,
			prepared.Claims,
			response.Sources,
			processor.config.AllowedDomains,
			processor.config.BlockedDomains,
		)
	}
	errorCode := ""
	if validationError != nil {
		errorCode = "schema_invalid"
	}
	if err := processor.repository.RecordAttempt(
		ctx,
		prepared,
		reservation,
		response,
		errorCode,
		processor.clock.Now().UTC(),
	); err != nil {
		return ProcessResult{}, fmt.Errorf("record background research attempt: %w", err)
	}
	if validationError != nil {
		if prepared.SchemaFailures+1 < MaximumSchemaAttempts {
			return ProcessResult{}, processor.fail(
				ctx,
				prepared,
				"schema_invalid_retryable",
				fmt.Errorf("%w: %v", ErrSchemaRetry, validationError),
			)
		}
		return ProcessResult{}, processor.fail(
			ctx,
			prepared,
			"schema_invalid",
			fmt.Errorf("%w: %v", ErrSchemaInvalid, validationError),
		)
	}
	return processor.repository.Complete(ctx, prepared, Completion{
		Output: output, ProviderID: response.ID, Sources: response.Sources, Usage: response.Usage,
	}, processor.clock.Now().UTC())
}

func (processor *Processor) providerFailure(
	ctx context.Context,
	prepared PreparedRun,
	reservation AttemptReservation,
	response ProviderResponse,
	cause error,
) error {
	if err := processor.repository.RecordAttempt(
		ctx,
		prepared,
		reservation,
		response,
		"provider_error",
		processor.clock.Now().UTC(),
	); err != nil {
		cause = errors.Join(cause, fmt.Errorf("record failed research provider attempt: %w", err))
	}
	return processor.fail(ctx, prepared, "provider_error", cause)
}

func (processor *Processor) fail(ctx context.Context, prepared PreparedRun, errorCode string, cause error) error {
	if err := processor.repository.Fail(ctx, prepared, errorCode, processor.clock.Now().UTC()); err != nil {
		return errors.Join(cause, fmt.Errorf("persist research failure: %w", err))
	}
	return cause
}

func validateRegistry(prepared PreparedRun, config Config) error {
	promptDigest := sha256.Sum256([]byte(prompts.ResearchSynthesisV1()))
	schemaDigest := SchemaDigest()
	if prepared.Prompt.SemanticVersion != prompts.ResearchSynthesisVersion ||
		prepared.Prompt.SchemaVersion != schemas.ResearchSynthesisVersion ||
		!bytes.Equal(prepared.Prompt.PromptSHA256, promptDigest[:]) ||
		!bytes.Equal(prepared.Prompt.SchemaSHA256, schemaDigest[:]) ||
		prepared.Model.ModelID != config.ExpectedModelID ||
		prepared.Model.Reasoning != config.ExpectedReasoning ||
		prepared.Model.Verbosity != config.ExpectedVerbosity ||
		len(prepared.Model.EnabledTools) != 1 || prepared.Model.EnabledTools[0] != "web_search" ||
		prepared.Model.MaxOutputTokens != config.ExpectedMaxOutputTokens {
		return ErrConfigurationDrift
	}
	return nil
}

func estimateInputTokens(prompt string, input string, schema []byte) int64 {
	return int64((len(prompt) + len(input) + len(schema) + 3) / 4)
}

func Permanent(err error) bool {
	return errors.Is(err, ErrInvalidTarget) || errors.Is(err, ErrConfigurationDrift) ||
		errors.Is(err, ErrSchemaInvalid) || errors.Is(err, ErrWebSearchLimit) ||
		errors.Is(err, ErrAttemptsExhausted) || errors.Is(err, extraction.ErrBudgetExceeded)
}
