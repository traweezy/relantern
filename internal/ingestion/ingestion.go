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
	if endpoint.RegistryID == "github-global-advisories" && endpoint.SourceID == "github-global-advisories" &&
		endpoint.Connector == sources.ConnectorGitHubAdvisories && endpoint.URL == sources.GlobalReviewedAdvisoriesURL {
		return poller.pollGlobalAdvisories(ctx, *endpoint)
	}
	_, err = poller.fetchAndRecord(ctx, *endpoint)
	return err
}

// A poll always refreshes the first, update-ordered page. Two additional pages
// advance a durable cursor without allowing a large catalog to monopolize the
// fetch queue. Each page and its next cursor commit in one storage transaction.
func (poller *Poller) pollGlobalAdvisories(ctx context.Context, endpoint Endpoint) error {
	const continuationPagesPerPoll = 2
	priorCursor := endpoint.Checkpoint.Cursor
	if priorCursor != "" && !sources.IsGlobalAdvisoryPageURL(priorCursor) {
		return errors.New("reviewed advisory checkpoint contains an invalid continuation URL")
	}
	rootEndpoint := endpoint
	if advisoryScanDue(endpoint.Checkpoint, poller.clock.Now().UTC()) {
		// A conditional 304 cannot start a second catalog pass. Refresh the
		// pinned root unconditionally once a day after a completed scan.
		rootEndpoint.Checkpoint.ETag = ""
		rootEndpoint.Checkpoint.LastModified = ""
	}
	root, err := poller.fetchAndRecord(ctx, rootEndpoint, func(result *fetcher.Result) {
		if result.Outcome == fetcher.OutcomeNotModified && priorCursor == "" {
			return
		}
		if result.Outcome == fetcher.OutcomeStored && result.NextPageURL == "" {
			result.Checkpoint.Cursor = ""
		} else if priorCursor != "" {
			result.Checkpoint.Cursor = priorCursor
		} else {
			result.Checkpoint.Cursor = result.NextPageURL
		}
		setAdvisoryScanState(&result.Checkpoint, poller.clock.Now().UTC(), result.Checkpoint.Cursor, priorCursor == "", "")
	})
	if err != nil || root.Outcome == fetcher.OutcomeFailed {
		return err
	}
	checkpoint := root.Checkpoint
	if advisoryScanIssue(checkpoint) != "" {
		poller.logger.WarnContext(ctx, "reviewed advisory scan requires operator review",
			"registry_id", endpoint.RegistryID, "issue", advisoryScanIssue(checkpoint))
		return nil
	}
	for page := 0; page < continuationPagesPerPoll && checkpoint.Cursor != ""; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageEndpoint := endpoint
		pageEndpoint.URL = checkpoint.Cursor
		// Conditional validators belong to the pinned first page. A 304 for a
		// continuation cannot advance the cursor or prove older coverage.
		pageEndpoint.Checkpoint = fetcher.Checkpoint{}
		result, err := poller.fetchAndRecord(ctx, pageEndpoint, func(result *fetcher.Result) {
			result.Checkpoint = checkpoint
			result.Checkpoint.Cursor = result.NextPageURL
			setAdvisoryScanState(&result.Checkpoint, poller.clock.Now().UTC(), result.Checkpoint.Cursor, false, pageEndpoint.URL)
		})
		if err != nil || result.Outcome == fetcher.OutcomeFailed {
			return err
		}
		checkpoint = result.Checkpoint
		if advisoryScanIssue(checkpoint) != "" {
			poller.logger.WarnContext(ctx, "reviewed advisory scan requires operator review",
				"registry_id", endpoint.RegistryID, "issue", advisoryScanIssue(checkpoint))
			return nil
		}
	}
	if checkpoint.Cursor != "" {
		poller.logger.InfoContext(ctx, "reviewed advisory scan continues", "registry_id", endpoint.RegistryID)
	}
	return nil
}

func advisoryScanDue(checkpoint fetcher.Checkpoint, now time.Time) bool {
	if checkpoint.Cursor != "" {
		return false
	}
	completedAt, ok := checkpoint.ProviderState["advisoryScanCompletedAt"].(string)
	if !ok {
		return true
	}
	completed, err := time.Parse(time.RFC3339Nano, completedAt)
	return err != nil || !now.Before(completed.Add(24*time.Hour))
}

func setAdvisoryScanState(checkpoint *fetcher.Checkpoint, now time.Time, nextURL string, newScan bool, fetchedPageURL string) {
	const maximumPagesPerScan = 10000
	const recentPagesLimit = 32
	state := make(map[string]any, len(checkpoint.ProviderState)+5)
	for key, value := range checkpoint.ProviderState {
		state[key] = value
	}
	if nextURL == "" {
		delete(state, "advisoryScanStartedAt")
		delete(state, "advisoryScanPages")
		delete(state, "advisoryScanRecentPages")
		delete(state, "advisoryScanIssue")
		state["advisoryScanCompletedAt"] = now.Format(time.RFC3339Nano)
	} else {
		if newScan {
			state["advisoryScanPages"] = 0
			state["advisoryScanRecentPages"] = []string{}
			delete(state, "advisoryScanIssue")
		}
		if newScan || state["advisoryScanStartedAt"] == nil {
			state["advisoryScanStartedAt"] = now.Format(time.RFC3339Nano)
		}
		if fetchedPageURL != "" {
			pages := advisoryScanPageCount(state["advisoryScanPages"]) + 1
			state["advisoryScanPages"] = pages
			recent := advisoryRecentPages(state["advisoryScanRecentPages"])
			nextDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(nextURL)))
			for _, previous := range recent {
				if previous == nextDigest {
					state["advisoryScanIssue"] = "cursor_cycle"
					break
				}
			}
			recent = append(recent, fmt.Sprintf("%x", sha256.Sum256([]byte(fetchedPageURL))))
			if len(recent) > recentPagesLimit {
				recent = recent[len(recent)-recentPagesLimit:]
			}
			state["advisoryScanRecentPages"] = recent
			if pages > maximumPagesPerScan {
				state["advisoryScanIssue"] = "page_limit"
			}
		}
	}
	checkpoint.ProviderState = state
}

func advisoryScanPageCount(value any) int {
	switch pages := value.(type) {
	case int:
		return pages
	case float64:
		if pages >= 0 && pages <= 10001 {
			return int(pages)
		}
		return 10001
	}
	return 0
}

func advisoryRecentPages(value any) []string {
	const recentPagesLimit = 32
	var recent []string
	switch values := value.(type) {
	case []string:
		recent = append(recent, values...)
	case []any:
		for _, value := range values {
			if digest, ok := value.(string); ok && len(digest) == 64 {
				recent = append(recent, digest)
			}
		}
	}
	if len(recent) > recentPagesLimit {
		recent = recent[len(recent)-recentPagesLimit:]
	}
	return recent
}

func advisoryScanIssue(checkpoint fetcher.Checkpoint) string {
	issue, _ := checkpoint.ProviderState["advisoryScanIssue"].(string)
	return issue
}

func (poller *Poller) fetchAndRecord(ctx context.Context, endpoint Endpoint, prepare ...func(*fetcher.Result)) (fetcher.Result, error) {
	result, fetchErr := poller.fetcher.Fetch(ctx, endpoint)
	if len(result.Attempts) == 0 {
		if fetchErr != nil {
			return result, fmt.Errorf("fetch source %s without a recorded attempt: %w", endpoint.RegistryID, fetchErr)
		}
		return result, fmt.Errorf("fetch source %s returned no attempt", endpoint.RegistryID)
	}
	if endpoint.RegistryID == "github-global-advisories" && endpoint.URL != sources.GlobalReviewedAdvisoriesURL &&
		result.Outcome == fetcher.OutcomeNotModified {
		result.Outcome = fetcher.OutcomeFailed
		result.Attempts[len(result.Attempts)-1].ErrorCode = fetcher.ErrorUnexpectedStatus
		fetchErr = &fetcher.FetchError{
			Code: fetcher.ErrorUnexpectedStatus, Retryable: true,
			Err: errors.New("reviewed advisory continuation returned 304 without a conditional validator"),
		}
	}
	if fetchErr == nil && result.Outcome != fetcher.OutcomeFailed {
		for _, apply := range prepare {
			apply(&result)
		}
	}
	if err := poller.repository.RecordFetch(ctx, endpoint, result, fetchErr); err != nil {
		return result, fmt.Errorf("record source %s fetch: %w", endpoint.RegistryID, err)
	}
	finalAttempt := result.Attempts[len(result.Attempts)-1]
	pageKind := "root"
	if endpoint.Connector == sources.ConnectorGitHubAdvisories && endpoint.URL != sources.GlobalReviewedAdvisoriesURL {
		pageKind = "continuation"
	}
	poller.logger.InfoContext(ctx, "source poll complete",
		"registry_id", endpoint.RegistryID,
		"page_kind", pageKind,
		"outcome", result.Outcome,
		"status_code", finalAttempt.StatusCode,
		"error_code", finalAttempt.ErrorCode,
		"attempts", len(result.Attempts),
		"duration_ms", finalAttempt.Duration.Milliseconds(),
	)
	// Fetch failures are persisted and rescheduled from their checkpoint. River
	// retries only storage failures, avoiding a burst of repeat provider calls.
	return result, nil
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
