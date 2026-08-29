package parsing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

type ProcessRequest struct {
	RawDocumentID string
	SourceID      string
	Parse         Request
}

type ProcessResult struct {
	Parsed    Result
	Recorded  RecordResult
	ObjectKey string
}

type Processor struct {
	parser    Parser
	objects   ObjectStore
	revisions RevisionRepository
	clock     clock.Clock
}

func NewProcessor(objects ObjectStore, revisions RevisionRepository, configuredClock clock.Clock) (*Processor, error) {
	if objects == nil || revisions == nil || configuredClock == nil {
		return nil, errors.New("parser processor requires object storage, revision repository, and clock")
	}
	return &Processor{parser: New(), objects: objects, revisions: revisions, clock: configuredClock}, nil
}

func (processor *Processor) Process(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	if strings.TrimSpace(request.RawDocumentID) == "" || strings.TrimSpace(request.SourceID) == "" {
		return ProcessResult{}, errors.New("raw document id and source id are required")
	}
	attemptedAt := processor.clock.Now().UTC()
	parsed, err := processor.parser.Parse(ctx, request.Parse)
	if err != nil {
		return ProcessResult{}, processor.recordParseFailure(ctx, request, attemptedAt, errorCode(err), nil, err)
	}
	objectKey, err := storage.NormalizedObjectKey(request.SourceID, parsed.NormalizedSHA256)
	if err != nil {
		return ProcessResult{}, processor.recordParseFailure(ctx, request, attemptedAt, ErrorObjectStorage, parsed.Warnings, err)
	}
	staged, err := processor.objects.Stage(ctx, "text/plain; charset=utf-8", strings.NewReader(parsed.NormalizedText))
	if err != nil {
		wrapped := fmt.Errorf("stage normalized content: %w", err)
		return ProcessResult{}, processor.recordParseFailure(ctx, request, attemptedAt, ErrorObjectStorage, parsed.Warnings, wrapped)
	}
	if staged.SHA256 != parsed.NormalizedSHA256 || staged.Bytes != int64(len(parsed.NormalizedText)) {
		_ = processor.objects.Abort(ctx, staged)
		integrityError := errors.New("staged normalized object does not match parser digest and size")
		return ProcessResult{}, processor.recordParseFailure(ctx, request, attemptedAt, ErrorObjectStorage, parsed.Warnings, integrityError)
	}
	if err := processor.objects.Commit(ctx, staged, objectKey); err != nil {
		abortError := processor.objects.Abort(ctx, staged)
		wrapped := fmt.Errorf("commit normalized content: %w", err)
		if abortError != nil {
			wrapped = errors.Join(wrapped, fmt.Errorf("abort normalized staging object: %w", abortError))
		}
		return ProcessResult{}, processor.recordParseFailure(ctx, request, attemptedAt, ErrorObjectStorage, parsed.Warnings, wrapped)
	}
	completedAt := processor.clock.Now().UTC()
	recorded, err := processor.revisions.RecordSuccess(ctx, RecordRequest{
		RawDocumentID: request.RawDocumentID,
		ObjectKey:     objectKey,
		Result:        parsed,
		AttemptedAt:   attemptedAt,
		CompletedAt:   completedAt,
	})
	if err != nil {
		return ProcessResult{}, fmt.Errorf("record normalized revision: %w", err)
	}
	return ProcessResult{Parsed: parsed, Recorded: recorded, ObjectKey: objectKey}, nil
}

func (processor *Processor) recordParseFailure(
	ctx context.Context,
	request ProcessRequest,
	attemptedAt time.Time,
	code ErrorCode,
	warnings []Warning,
	parseError error,
) error {
	completedAt := processor.clock.Now().UTC()
	recordError := processor.revisions.RecordFailure(ctx, FailureRequest{
		RawDocumentID: request.RawDocumentID,
		ParserName:    parserNameFor(request.Parse.Connector),
		ErrorCode:     code,
		Warnings:      warnings,
		AttemptedAt:   attemptedAt,
		CompletedAt:   completedAt,
	})
	if recordError != nil {
		return errors.Join(parseError, fmt.Errorf("record parser failure: %w", recordError))
	}
	return parseError
}

func parserNameFor(connector sources.Connector) string {
	switch connector {
	case sources.ConnectorAtom, sources.ConnectorRSS, sources.ConnectorJSONFeed:
		return "gofeed-" + string(connector)
	case sources.ConnectorPage:
		return "readeck-readability"
	case sources.ConnectorGitHubReleases:
		return "github-releases-json"
	case sources.ConnectorGitHubAdvisories:
		return "github-advisories-json"
	case sources.ConnectorRegistry:
		return "registry-json"
	case sources.ConnectorStructuredAPI:
		return "structured-api-json"
	default:
		return "unsupported"
	}
}
