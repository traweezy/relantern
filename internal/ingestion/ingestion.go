package ingestion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

// Endpoint is the immutable configuration and checkpoint for one polling attempt.
type Endpoint struct {
	RegistryID           string
	SourceID             string
	Connector            sources.Connector
	URL                  string
	ContentPolicy        string
	ExpectedContentTypes []string
	MaxResponseBytes     int64
	Checkpoint           fetcher.Checkpoint
}

type RawDocument struct {
	ID               string
	SourceID         string
	Connector        sources.Connector
	URL              string
	ContentType      string
	ContentPolicy    string
	ObjectKey        string
	MaxResponseBytes int64
	RawSHA256        [sha256.Size]byte
	ParentRawID      string
	SourceEntryID    string
}

// HandoffCapabilities names only downstream workers available in this process.
// An empty embedding model or a false AI flag keeps that work durable for a
// later reconciliation instead of queuing a job with no registered worker.
type HandoffCapabilities struct {
	EmbeddingModelID string
	ExtractEnabled   bool
	ResearchEnabled  bool
}

type Repository interface {
	ScheduleDue(context.Context, time.Time, int) (int, error)
	ReconcilePending(context.Context, HandoffCapabilities, int) (int, error)
	LoadEndpoint(context.Context, string) (*Endpoint, error)
	RecordFetch(context.Context, Endpoint, fetcher.Result, error) error
	LoadRawDocument(context.Context, string, string) (RawDocument, error)
	RecordEntries(context.Context, RawDocument, string, []parsing.Entry) (int, error)
	RecordIngestionFailure(context.Context, RawDocument, string, parsing.ErrorCode) error
	CompleteParsedRevision(context.Context, RawDocument, string, dedupe.ProcessResult, string, HandoffCapabilities) error
}

type Fetcher interface {
	Fetch(context.Context, Endpoint) (fetcher.Result, error)
}

type Parser interface {
	Process(context.Context, parsing.ProcessRequest) (parsing.ProcessResult, error)
}

type Deduper interface {
	Process(context.Context, dedupe.ProcessRequest) (dedupe.ProcessResult, error)
}

type Poller struct {
	repository   Repository
	fetcher      Fetcher
	clock        clock.Clock
	logger       *slog.Logger
	enabled      bool
	capabilities HandoffCapabilities
}

func NewPoller(repository Repository, configuredFetcher Fetcher, configuredClock clock.Clock, logger *slog.Logger, enabled bool, capabilities HandoffCapabilities) (*Poller, error) {
	if repository == nil || configuredFetcher == nil || configuredClock == nil || logger == nil {
		return nil, errors.New("source poller requires repository, fetcher, clock, and logger")
	}
	if err := capabilities.validate(); err != nil {
		return nil, err
	}
	return &Poller{repository: repository, fetcher: configuredFetcher, clock: configuredClock, logger: logger, enabled: enabled, capabilities: capabilities}, nil
}

func (poller *Poller) Reconcile(ctx context.Context) (int, error) {
	var due int
	var scheduleErr error
	if poller.enabled {
		due, scheduleErr = poller.repository.ScheduleDue(ctx, poller.clock.Now().UTC(), 100)
		if scheduleErr == nil {
			poller.logger.InfoContext(ctx, "due source polling reconciled", "jobs_enqueued", due)
		} else {
			scheduleErr = fmt.Errorf("schedule due source polling: %w", scheduleErr)
		}
	}
	pending, pendingErr := poller.repository.ReconcilePending(ctx, poller.capabilities, 100)
	if pendingErr == nil {
		poller.logger.InfoContext(ctx, "pending source intelligence reconciled", "jobs_enqueued", pending)
	} else {
		pendingErr = fmt.Errorf("reconcile pending source intelligence: %w", pendingErr)
	}
	return due + pending, errors.Join(scheduleErr, pendingErr)
}

func (poller *Poller) Poll(ctx context.Context, registryID string) error {
	if !poller.enabled {
		return nil
	}
	if registryID == "" {
		return errors.New("source registry ID is required")
	}
	endpoint, err := poller.repository.LoadEndpoint(ctx, registryID)
	if err != nil {
		return err
	}
	if endpoint == nil {
		// An operator can pause a source after its durable job was inserted.
		return nil
	}
	result, fetchErr := poller.fetcher.Fetch(ctx, *endpoint)
	if len(result.Attempts) == 0 {
		if fetchErr != nil {
			return fmt.Errorf("fetch source %s without a recorded attempt: %w", registryID, fetchErr)
		}
		return fmt.Errorf("fetch source %s returned no attempt", registryID)
	}
	if err := poller.repository.RecordFetch(ctx, *endpoint, result, fetchErr); err != nil {
		return fmt.Errorf("record source %s fetch: %w", registryID, err)
	}
	finalAttempt := result.Attempts[len(result.Attempts)-1]
	poller.logger.InfoContext(ctx, "source poll complete",
		"registry_id", registryID,
		"outcome", result.Outcome,
		"status_code", finalAttempt.StatusCode,
		"error_code", finalAttempt.ErrorCode,
		"attempts", len(result.Attempts),
		"duration_ms", finalAttempt.Duration.Milliseconds(),
	)
	// Fetch failures are persisted and rescheduled from their checkpoint. River
	// retries only storage failures, avoiding a burst of repeat provider calls.
	return nil
}

type RawReader interface {
	Read(context.Context, string, int64) ([]byte, error)
}

type ParseWorker struct {
	repository   Repository
	objects      RawReader
	parser       Parser
	deduper      Deduper
	logger       *slog.Logger
	capabilities HandoffCapabilities
}

func NewParseWorker(repository Repository, objects RawReader, parser Parser, deduper Deduper, logger *slog.Logger, capabilities HandoffCapabilities) (*ParseWorker, error) {
	if repository == nil || objects == nil || parser == nil || deduper == nil || logger == nil {
		return nil, errors.New("source parser requires repository, object reader, parser, deduper, and logger")
	}
	if err := capabilities.validate(); err != nil {
		return nil, err
	}
	return &ParseWorker{repository: repository, objects: objects, parser: parser, deduper: deduper, logger: logger, capabilities: capabilities}, nil
}

func (capabilities HandoffCapabilities) validate() error {
	if capabilities.EmbeddingModelID != strings.TrimSpace(capabilities.EmbeddingModelID) || len(capabilities.EmbeddingModelID) > 255 {
		return errors.New("handoff embedding model ID must be trimmed and at most 255 bytes")
	}
	return nil
}

func (worker *ParseWorker) Parse(ctx context.Context, registryID string, rawDocumentID string) error {
	if registryID == "" || rawDocumentID == "" {
		return errors.New("source registry ID and raw document ID are required")
	}
	document, err := worker.repository.LoadRawDocument(ctx, registryID, rawDocumentID)
	if err != nil {
		return err
	}
	payload, err := worker.objects.Read(ctx, document.ObjectKey, document.MaxResponseBytes)
	if err != nil {
		return fmt.Errorf("read raw source document: %w", err)
	}
	if sha256.Sum256(payload) != document.RawSHA256 {
		return errors.New("raw source object digest does not match the recorded evidence")
	}
	var entries []parsing.Entry
	if document.ParentRawID == "" && parsing.IsCollectionConnector(document.Connector) {
		entries, err = parsing.SplitEntries(ctx, document.Connector, document.URL, payload)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			code := parsing.ErrorInvalidDocument
			var typed *parsing.ParseError
			if errors.As(err, &typed) {
				code = typed.Code
			} else {
				err = &parsing.ParseError{Code: code, Err: err}
			}
			if recordErr := worker.repository.RecordIngestionFailure(ctx, document, registryID, code); recordErr != nil {
				return fmt.Errorf("record source entry split failure: %w", recordErr)
			}
			return fmt.Errorf("split source document %s: %w", rawDocumentID, err)
		}
		if len(entries) == 0 {
			return worker.recordCollectionEntries(ctx, document, registryID, entries)
		}
	}
	parsed, err := worker.parser.Process(ctx, parsing.ProcessRequest{
		RawDocumentID: document.ID,
		SourceID:      document.SourceID,
		Parse: parsing.Request{
			Connector:   document.Connector,
			URL:         document.URL,
			ContentType: document.ContentType,
			Body:        bytes.NewReader(payload),
			MaxBytes:    document.MaxResponseBytes,
		},
	})
	if err != nil {
		var typed *parsing.ParseError
		if errors.As(err, &typed) && typed.Code != parsing.ErrorObjectStorage && typed.Code != parsing.ErrorCanceled {
			if recordErr := worker.repository.RecordIngestionFailure(ctx, document, registryID, typed.Code); recordErr != nil {
				return fmt.Errorf("record source entry parse failure: %w", recordErr)
			}
			return fmt.Errorf("parse source document %s: %w", rawDocumentID, typed)
		}
		return fmt.Errorf("parse source document %s: %w", rawDocumentID, err)
	}
	if entries != nil {
		return worker.recordCollectionEntries(ctx, document, registryID, entries)
	}
	decision, err := worker.deduper.Process(ctx, dedupe.ProcessRequest{
		RevisionID:           parsed.Recorded.RevisionID,
		NormalizedText:       parsed.Parsed.NormalizedText,
		DeclaredCanonicalURL: parsed.Parsed.CanonicalURL,
	})
	if err != nil {
		return fmt.Errorf("deduplicate source document %s: %w", rawDocumentID, err)
	}
	if err := worker.repository.CompleteParsedRevision(
		ctx, document, registryID, decision, parsed.Recorded.RevisionID, worker.capabilities,
	); err != nil {
		handoffErr := fmt.Errorf("complete source revision handoff: %w", err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return handoffErr
		}
		if recordErr := worker.repository.RecordIngestionFailure(ctx, document, registryID, parsing.ErrorCode("source_handoff_failed")); recordErr != nil {
			return errors.Join(handoffErr, fmt.Errorf("persist source revision handoff failure: %w", recordErr))
		}
		return handoffErr
	}
	worker.logger.InfoContext(ctx, "source document parsed",
		"registry_id", registryID,
		"raw_document_id", rawDocumentID,
		"revision_id", parsed.Recorded.RevisionID,
		"outcome", parsed.Recorded.Outcome,
		"item_id", decision.ItemID,
		"cluster_id", decision.ClusterID,
	)
	return nil
}

func (worker *ParseWorker) recordCollectionEntries(ctx context.Context, document RawDocument, registryID string, entries []parsing.Entry) error {
	inserted, err := worker.repository.RecordEntries(ctx, document, registryID, entries)
	if err != nil {
		recordErr := worker.repository.RecordIngestionFailure(ctx, document, registryID, parsing.ErrorCode("source_entry_record_failed"))
		if recordErr != nil {
			return errors.Join(
				fmt.Errorf("record source entries for %s: %w", document.ID, err),
				fmt.Errorf("persist source entry recording failure: %w", recordErr),
			)
		}
		return fmt.Errorf("record source entries for %s: %w", document.ID, err)
	}
	worker.logger.InfoContext(ctx, "source entries recorded",
		"registry_id", registryID, "raw_document_id", document.ID,
		"entry_count", len(entries), "new_entries", inserted,
	)
	return nil
}
