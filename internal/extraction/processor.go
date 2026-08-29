package extraction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/contracts/schemas"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/storage"
	"github.com/traweezy/relantern/prompts"
)

var (
	ErrInvalidTarget      = errors.New("structured extraction target is invalid")
	ErrBudgetExceeded     = errors.New("OpenAI monthly hard budget is exhausted")
	ErrRevisionIntegrity  = errors.New("structured extraction revision failed integrity validation")
	ErrConfigurationDrift = errors.New("structured extraction prompt or schema registry drift")
	ErrSchemaInvalid      = errors.New("structured extraction failed schema validation twice")
)

type Config struct {
	ExpectedModelID         string
	ExpectedReasoning       string
	ExpectedVerbosity       string
	ExpectedMaxOutputTokens int
	MonthlySoftUSD          USD
	MonthlyHardUSD          USD
	MaximumAge              time.Duration
}

type Processor struct {
	repository RunRepository
	reader     storage.ObjectReader
	provider   Provider
	clock      clock.Clock
	config     Config
}

func NewProcessor(
	repository RunRepository,
	reader storage.ObjectReader,
	provider Provider,
	configuredClock clock.Clock,
	config Config,
) (*Processor, error) {
	if repository == nil || reader == nil || provider == nil || configuredClock == nil {
		return nil, errors.New("structured extraction requires repository, object storage, provider, and clock dependencies")
	}
	if err := ValidateBudgetRange(config.MonthlySoftUSD, config.MonthlyHardUSD); err != nil {
		return nil, fmt.Errorf("invalid OpenAI budget configuration: %w", err)
	}
	if config.MaximumAge <= 0 || config.MaximumAge > 365*24*time.Hour {
		return nil, errors.New("structured extraction maximum age must be positive and at most one year")
	}
	if config.ExpectedModelID == "" || config.ExpectedReasoning == "" ||
		config.ExpectedVerbosity == "" || config.ExpectedMaxOutputTokens < 256 {
		return nil, errors.New("structured extraction requires complete expected model configuration")
	}
	return &Processor{
		repository: repository,
		reader:     reader,
		provider:   provider,
		clock:      configuredClock,
		config:     config,
	}, nil
}

func (processor *Processor) Process(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	if _, err := uuid.Parse(request.ItemID); err != nil {
		return ProcessResult{}, fmt.Errorf("%w: item ID must be a UUID", ErrInvalidTarget)
	}
	if _, err := uuid.Parse(request.RevisionID); err != nil {
		return ProcessResult{}, fmt.Errorf("%w: revision ID must be a UUID", ErrInvalidTarget)
	}
	now := processor.clock.Now().UTC()
	prepared, err := processor.repository.Prepare(ctx, request, now)
	if err != nil {
		return ProcessResult{}, err
	}
	if prepared.Obsolete || prepared.State == "obsolete" {
		return ProcessResult{RunID: prepared.RunID, Obsolete: true}, nil
	}
	if prepared.State == "completed" || prepared.State == "needs_review" {
		return ProcessResult{
			RunID:            prepared.RunID,
			AlreadyCompleted: true,
			NeedsReview:      prepared.State == "needs_review",
		}, nil
	}
	if now.Sub(prepared.FirstSeenAt) > processor.config.MaximumAge {
		return ProcessResult{}, processor.fail(
			ctx,
			prepared,
			"document_too_old",
			fmt.Errorf("%w: document exceeds the configured extraction age", ErrInvalidTarget),
		)
	}
	if prepared.NormalizedBytes < 1 || prepared.NormalizedBytes > MaximumNormalizedBytes {
		return ProcessResult{}, processor.fail(
			ctx,
			prepared,
			"input_too_large",
			fmt.Errorf("%w: normalized content exceeds %d bytes", ErrInvalidTarget, MaximumNormalizedBytes),
		)
	}
	if err := validateRegistry(prepared, processor.config); err != nil {
		return ProcessResult{}, processor.fail(ctx, prepared, "configuration_drift", err)
	}
	payload, err := processor.reader.Read(ctx, prepared.ObjectKey, MaximumNormalizedBytes)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("read normalized extraction evidence: %w", err)
	}
	digest := sha256.Sum256(payload)
	if len(prepared.NormalizedHash) != sha256.Size ||
		!bytes.Equal(digest[:], prepared.NormalizedHash) ||
		!utf8.Valid(payload) {
		return ProcessResult{}, processor.fail(ctx, prepared, "revision_integrity", ErrRevisionIntegrity)
	}
	spans, err := BuildEvidenceSpans(string(payload), prepared.Outline)
	if err != nil {
		return ProcessResult{}, processor.fail(
			ctx,
			prepared,
			"invalid_evidence_spans",
			fmt.Errorf("%w: %v", ErrRevisionIntegrity, err),
		)
	}
	input, err := EncodeEvidenceInput(prepared.Title, prepared.SourceID, prepared.SourceTier, spans)
	if err != nil {
		return ProcessResult{}, err
	}
	prompt := prompts.StructuredExtractionV1()
	estimatedInputTokens := estimateInputTokens(prompt, input, schemas.StructuredExtractionV1())

	for schemaAttempt := 1; schemaAttempt <= MaximumSchemaAttempts; schemaAttempt++ {
		reservation, reserveError := processor.repository.ReserveAttempt(
			ctx,
			prepared,
			estimatedInputTokens,
			processor.config.MonthlySoftUSD,
			processor.config.MonthlyHardUSD,
			processor.clock.Now().UTC(),
		)
		if reserveError != nil {
			return ProcessResult{}, reserveError
		}
		response, providerError := processor.provider.Extract(ctx, ProviderRequest{
			ModelID:         prepared.Model.ModelID,
			Reasoning:       prepared.Model.Reasoning,
			Verbosity:       prepared.Model.Verbosity,
			MaxOutputTokens: prepared.Model.MaxOutputTokens,
			Prompt:          prompt,
			Input:           input,
		})
		if providerError != nil {
			recordError := processor.repository.RecordAttempt(
				ctx,
				prepared,
				reservation,
				ProviderResponse{},
				"provider_error",
				processor.clock.Now().UTC(),
			)
			if recordError != nil {
				providerError = errors.Join(providerError, fmt.Errorf("record failed provider attempt: %w", recordError))
			}
			return ProcessResult{}, processor.fail(ctx, prepared, "provider_error", providerError)
		}
		output, validationError := DecodeAndValidate(response.Output, spans)
		errorCode := ""
		if validationError != nil {
			errorCode = "schema_invalid"
		}
		if recordError := processor.repository.RecordAttempt(
			ctx,
			prepared,
			reservation,
			response,
			errorCode,
			processor.clock.Now().UTC(),
		); recordError != nil {
			return ProcessResult{}, processor.fail(
				ctx,
				prepared,
				"provider_error",
				fmt.Errorf("record OpenAI extraction attempt: %w", recordError),
			)
		}
		if validationError != nil {
			if schemaAttempt < MaximumSchemaAttempts {
				continue
			}
			return ProcessResult{}, processor.fail(
				ctx,
				prepared,
				"schema_invalid",
				fmt.Errorf("%w: %v", ErrSchemaInvalid, validationError),
			)
		}

		needsReview := needsHumanReview(output, prepared.SourceTier)
		state := "completed"
		if needsReview {
			state = "needs_review"
		}
		return processor.repository.Complete(ctx, prepared, Completion{
			Output:         output,
			Spans:          spans,
			ProviderID:     response.ID,
			Usage:          response.Usage,
			NeedsReview:    needsReview,
			CompletedState: state,
		}, processor.clock.Now().UTC())
	}
	return ProcessResult{}, ErrSchemaInvalid
}

func (processor *Processor) fail(
	ctx context.Context,
	prepared PreparedRun,
	errorCode string,
	cause error,
) error {
	if err := processor.repository.Fail(ctx, prepared, errorCode, processor.clock.Now().UTC()); err != nil {
		return errors.Join(cause, fmt.Errorf("persist structured extraction failure: %w", err))
	}
	return cause
}

func validateRegistry(prepared PreparedRun, config Config) error {
	promptDigest := sha256.Sum256([]byte(prompts.StructuredExtractionV1()))
	schemaDigest := SchemaDigest()
	if prepared.Prompt.SemanticVersion != prompts.StructuredExtractionVersion ||
		prepared.Prompt.SchemaVersion != schemas.StructuredExtractionVersion ||
		!bytes.Equal(prepared.Prompt.PromptSHA256, promptDigest[:]) ||
		!bytes.Equal(prepared.Prompt.SchemaSHA256, schemaDigest[:]) ||
		prepared.Model.ModelID != config.ExpectedModelID ||
		prepared.Model.Reasoning != config.ExpectedReasoning ||
		prepared.Model.Verbosity != config.ExpectedVerbosity ||
		len(prepared.Model.EnabledTools) != 0 ||
		prepared.Model.MaxOutputTokens != config.ExpectedMaxOutputTokens {
		return ErrConfigurationDrift
	}
	return nil
}

func estimateInputTokens(prompt string, input string, schema []byte) int64 {
	bytesCount := len(prompt) + len(input) + len(schema)
	return int64((bytesCount + 3) / 4)
}

func needsHumanReview(output Output, sourceTier string) bool {
	if len(output.Claims) == 0 {
		return true
	}
	trustedMaterialSource := sourceTier == "T0" || sourceTier == "T1"
	if trustedMaterialSource {
		return false
	}
	for _, claim := range output.Claims {
		if claim.Material {
			return true
		}
	}
	return false
}

func Permanent(err error) bool {
	return errors.Is(err, ErrInvalidTarget) ||
		errors.Is(err, ErrRevisionIntegrity) ||
		errors.Is(err, ErrConfigurationDrift) ||
		errors.Is(err, ErrSchemaInvalid) ||
		errors.Is(err, ErrBudgetExceeded)
}

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrBudgetExceeded):
		return "budget_exceeded"
	case errors.Is(err, ErrRevisionIntegrity):
		return "revision_integrity"
	case errors.Is(err, ErrConfigurationDrift):
		return "configuration_drift"
	case errors.Is(err, ErrSchemaInvalid):
		return "schema_invalid"
	case errors.Is(err, ErrInvalidTarget):
		return "invalid_target"
	default:
		return "provider_error"
	}
}
