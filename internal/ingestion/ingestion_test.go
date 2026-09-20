package ingestion

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/parsing"
	"github.com/traweezy/relantern/internal/sources"
)

type memoryRepository struct {
	endpoint     *Endpoint
	recorded     []fetcher.Result
	recordError  error
	document     RawDocument
	entries      []parsing.Entry
	entriesError error
	pending      bool
	splitCode    parsing.ErrorCode
	failureRawID string
	dueCount     int
}

func (repository *memoryRepository) ScheduleDue(_ context.Context, _ time.Time, limit int) (int, error) {
	if limit != 100 {
		return 0, errors.New("unexpected source scheduling batch")
	}
	return repository.dueCount, nil
}
func (repository *memoryRepository) LoadEndpoint(context.Context, string) (*Endpoint, error) {
	return repository.endpoint, nil
}
func (repository *memoryRepository) RecordFetch(_ context.Context, _ Endpoint, result fetcher.Result, _ error) error {
	repository.recorded = append(repository.recorded, result)
	return repository.recordError
}
func (repository *memoryRepository) LoadRawDocument(_ context.Context, _ string, _ string) (RawDocument, error) {
	return repository.document, nil
}
func (repository *memoryRepository) RecordEntries(_ context.Context, _ RawDocument, _ string, entries []parsing.Entry) (int, error) {
	if repository.entriesError != nil {
		return 0, repository.entriesError
	}
	repository.entries = entries
	repository.pending = false
	return len(entries), nil
}
func (repository *memoryRepository) RecordIngestionFailure(_ context.Context, document RawDocument, _ string, code parsing.ErrorCode) error {
	repository.splitCode = code
	repository.failureRawID = document.ID
	return nil
}
func (repository *memoryRepository) ClearIngestionFailure(context.Context, RawDocument, string) error {
	return nil
}

type fixtureFetcher struct {
	result fetcher.Result
	err    error
	calls  int
}

func (fixture *fixtureFetcher) Fetch(context.Context, Endpoint) (fetcher.Result, error) {
	fixture.calls++
	return fixture.result, fixture.err
}

func TestPollSkipsPausedSourceAndRecordsProviderFailure(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	repository := &memoryRepository{dueCount: 2}
	provider := &fixtureFetcher{}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := poller.Reconcile(context.Background()); err != nil || count != 2 {
		t.Fatalf("Reconcile() = %d, %v", count, err)
	}
	if err := poller.Poll(context.Background(), "paused-source"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatal("paused source made a provider request")
	}

	repository.endpoint = &Endpoint{RegistryID: "active-source", SourceID: "active"}
	provider.result = fetcher.Result{
		Outcome:  fetcher.OutcomeFailed,
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 429, ErrorCode: fetcher.ErrorUnexpectedStatus}},
	}
	provider.err = &fetcher.FetchError{Code: fetcher.ErrorUnexpectedStatus, Retryable: true, Err: errors.New("provider is rate limited")}
	if err := poller.Poll(context.Background(), "active-source"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(repository.recorded) != 1 || repository.recorded[0].Outcome != fetcher.OutcomeFailed {
		t.Fatalf("failure was not recorded: calls=%d records=%d", provider.calls, len(repository.recorded))
	}
	repository.recordError = errors.New("database unavailable")
	if err := poller.Poll(context.Background(), "active-source"); err == nil {
		t.Fatal("database failure must retry the River job")
	}
}

func TestDisabledPollerDoesNotReadDatabaseOrNetwork(t *testing.T) {
	repository := &memoryRepository{dueCount: 3, endpoint: &Endpoint{RegistryID: "active"}}
	provider := &fixtureFetcher{}
	poller, err := NewPoller(repository, provider, clock.NewFixed(time.Now()), slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := poller.Reconcile(context.Background()); err != nil || count != 0 {
		t.Fatalf("disabled Reconcile() = %d, %v", count, err)
	}
	if err := poller.Poll(context.Background(), "active"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatal("disabled poller made a provider request")
	}
}

type fixtureReader struct {
	payload string
	key     string
}

func (reader *fixtureReader) Read(_ context.Context, key string, _ int64) ([]byte, error) {
	reader.key = key
	return []byte(reader.payload), nil
}

type fixtureParser struct {
	request parsing.ProcessRequest
	err     error
}

func (parser *fixtureParser) Process(_ context.Context, request parsing.ProcessRequest) (parsing.ProcessResult, error) {
	parser.request = request
	if parser.err != nil {
		return parsing.ProcessResult{}, parser.err
	}
	return parsing.ProcessResult{Recorded: parsing.RecordResult{RevisionID: "revision", Outcome: "created"}}, nil
}

type fixtureDeduper struct{ request dedupe.ProcessRequest }

func (deduper *fixtureDeduper) Process(_ context.Context, request dedupe.ProcessRequest) (dedupe.ProcessResult, error) {
	deduper.request = request
	return dedupe.ProcessResult{ItemID: "item", ClusterID: "cluster"}, nil
}

func TestParseLoadsCommittedRawObject(t *testing.T) {
	feed := `<rss version="2.0"><channel><title>Fixture</title><link>https://example.com/rss</link><description>Fixture</description><item><guid>one</guid><title>First</title><link>https://example.com/first</link><description>Body</description></item></channel></rss>`
	repository := &memoryRepository{document: RawDocument{
		ID: "raw", SourceID: "source", Connector: sources.ConnectorRSS,
		URL: "https://example.com/rss", ContentType: "application/rss+xml",
		ContentPolicy: "link-and-excerpt", ObjectKey: "raw/source/2026/09/19/digest.xml", MaxResponseBytes: 1024,
		RawSHA256: sha256.Sum256([]byte(feed)),
	}}
	reader := &fixtureReader{payload: feed}
	parser := &fixtureParser{}
	deduper := &fixtureDeduper{}
	worker, err := NewParseWorker(repository, reader, parser, deduper, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "raw"); err != nil {
		t.Fatal(err)
	}
	if reader.key != repository.document.ObjectKey || parser.request.Parse.Connector != sources.ConnectorRSS {
		t.Fatal("committed raw document was not passed to its registered connector")
	}
	payload, err := io.ReadAll(parser.request.Parse.Body)
	if err != nil || strings.TrimSpace(string(payload)) != feed {
		t.Fatalf("parser body = %q, %v", payload, err)
	}
	if len(repository.entries) != 1 || repository.entries[0].ExternalID != "rss:one" || deduper.request.RevisionID != "" {
		t.Fatalf("aggregate did not split into one undeduped entry: entries=%+v dedupe=%+v", repository.entries, deduper.request)
	}

	repository.document = RawDocument{
		ID: "child", SourceID: "source", Connector: sources.ConnectorSourceEntry,
		URL: repository.entries[0].URL, ContentType: "application/json",
		ContentPolicy: "link-and-excerpt", ObjectKey: "raw/source-entry/child.json",
		MaxResponseBytes: 1024, RawSHA256: sha256.Sum256(repository.entries[0].Payload),
		ParentRawID: "raw", SourceEntryID: "entry",
	}
	reader.payload = string(repository.entries[0].Payload)
	if err := worker.Parse(context.Background(), "registry", "child"); err != nil {
		t.Fatal(err)
	}
	if deduper.request.RevisionID != "revision" || len(repository.entries) != 1 {
		t.Fatalf("child did not enter dedupe: %+v", deduper.request)
	}
}

func TestSplitFailureIsRecordedBeforeParentRevision(t *testing.T) {
	payload := `{"items":[{"id":"one","url":"https://127.0.0.1/private","title":"Unsafe"}]}`
	repository := &memoryRepository{document: RawDocument{
		ID: "raw", SourceID: "source", Connector: sources.ConnectorJSONFeed,
		URL: "https://example.com/feed", ContentType: "application/feed+json",
		ContentPolicy: "link-and-excerpt", ObjectKey: "raw/source/feed.json",
		MaxResponseBytes: 1024, RawSHA256: sha256.Sum256([]byte(payload)),
	}}
	parser := &fixtureParser{}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "raw"); err == nil {
		t.Fatal("unsafe child URL was accepted")
	}
	if repository.splitCode != parsing.ErrorInvalidURL || parser.request.RawDocumentID != "" || len(repository.entries) != 0 {
		t.Fatalf("split failure was not recorded before revision: code=%s parser=%+v entries=%d", repository.splitCode, parser.request, len(repository.entries))
	}
}

func TestValidEmptyCollectionClearsPendingReplayWithoutCreatingARevision(t *testing.T) {
	payload := `{"version":"https://jsonfeed.org/version/1.1","title":"Quiet feed","items":[]}`
	repository := &memoryRepository{document: RawDocument{
		ID: "raw", SourceID: "source", Connector: sources.ConnectorJSONFeed,
		URL: "https://example.com/feed", ContentType: "application/feed+json",
		ContentPolicy: "link-and-excerpt", ObjectKey: "raw/source/feed.json",
		MaxResponseBytes: 1024, RawSHA256: sha256.Sum256([]byte(payload)),
	}, pending: true}
	parser := &fixtureParser{}
	deduper := &fixtureDeduper{}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, deduper, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "raw"); err != nil {
		t.Fatal(err)
	}
	if repository.pending || repository.entries == nil || len(repository.entries) != 0 ||
		parser.request.RawDocumentID != "" || deduper.request.RevisionID != "" || repository.splitCode != "" {
		t.Fatalf("valid quiet feed failed: pending=%t entries=%v parser=%+v dedupe=%+v failure=%q",
			repository.pending, repository.entries, parser.request, deduper.request, repository.splitCode)
	}
}

func TestPermanentEntryParseFailureMarksChildForReplay(t *testing.T) {
	payload := `{"id":"rss:one","url":"https://example.com/one","title":"First","contentText":"Body"}`
	child := RawDocument{ID: "child", SourceID: "source", Connector: sources.ConnectorSourceEntry,
		URL: "https://example.com/one", ContentType: "application/json", ObjectKey: "child.json",
		ContentPolicy: "link-and-excerpt", MaxResponseBytes: 1024,
		RawSHA256: sha256.Sum256([]byte(payload)), ParentRawID: "parent", SourceEntryID: "one"}
	repository := &memoryRepository{document: child}
	parser := &fixtureParser{err: &parsing.ParseError{Code: parsing.ErrorInvalidDocument, Err: errors.New("unreadable child")}}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "child"); err == nil {
		t.Fatal("permanent child parse error was accepted")
	}
	if repository.splitCode != parsing.ErrorInvalidDocument || repository.failureRawID != "child" {
		t.Fatalf("child replay failure = %q on %q", repository.splitCode, repository.failureRawID)
	}
}

func TestEntryRecordingFailureRetainsReplayUntilSuccessfulRetry(t *testing.T) {
	feed := `<rss version="2.0"><channel><title>Fixture</title><link>https://example.com/feed</link><description>Fixture</description><item><guid>one</guid><title>First</title><link>https://example.com/one</link><description>Body</description></item></channel></rss>`
	repository := &memoryRepository{document: RawDocument{
		ID: "raw", SourceID: "source", Connector: sources.ConnectorRSS,
		URL: "https://example.com/feed", ContentType: "application/rss+xml",
		ContentPolicy: "link-and-excerpt", ObjectKey: "feed.xml", MaxResponseBytes: 1024,
		RawSHA256: sha256.Sum256([]byte(feed)),
	}, entriesError: errors.New("object storage unavailable"), pending: true}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: feed}, &fixtureParser{}, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "raw"); err == nil {
		t.Fatal("entry recording failure was accepted")
	}
	if !repository.pending || repository.failureRawID != "raw" || repository.splitCode != "source_entry_record_failed" {
		t.Fatalf("failed entry recording lost replay: pending=%t raw=%q code=%q", repository.pending, repository.failureRawID, repository.splitCode)
	}
	repository.entriesError = nil
	if err := worker.Parse(context.Background(), "registry", "raw"); err != nil {
		t.Fatal(err)
	}
	if repository.pending || len(repository.entries) != 1 || repository.entries[0].ExternalID != "rss:one" {
		t.Fatalf("replayed entry was not recorded: pending=%t entries=%+v", repository.pending, repository.entries)
	}
}
