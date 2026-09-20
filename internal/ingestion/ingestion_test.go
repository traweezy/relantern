package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	endpoint             *Endpoint
	recorded             []fetcher.Result
	recordError          error
	completeError        error
	failureError         error
	completeDecision     dedupe.ProcessResult
	completeRevision     string
	completeCapabilities HandoffCapabilities
	document             RawDocument
	entries              []parsing.Entry
	entriesError         error
	managedParent        bool
	pending              bool
	splitCode            parsing.ErrorCode
	failureRawID         string
	dueCount             int
	pendingCount         int
	scheduleError        error
	pendingError         error
	pendingCalls         int
	scheduleCalls        int
	reconcileOrder       []string
}

func (repository *memoryRepository) ScheduleDue(_ context.Context, _ time.Time, limit int) (int, error) {
	repository.scheduleCalls++
	repository.reconcileOrder = append(repository.reconcileOrder, "schedule")
	if limit != 100 {
		return 0, errors.New("unexpected source scheduling batch")
	}
	return repository.dueCount, repository.scheduleError
}
func (repository *memoryRepository) ReconcilePending(_ context.Context, _ HandoffCapabilities, limit int) (int, error) {
	repository.pendingCalls++
	repository.reconcileOrder = append(repository.reconcileOrder, "pending")
	if limit != 100 {
		return 0, errors.New("unexpected source intelligence reconciliation batch")
	}
	return repository.pendingCount, repository.pendingError
}
func (repository *memoryRepository) ReconcileAdvisoryObservations(context.Context, int) (int, error) {
	return 0, nil
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
func (repository *memoryRepository) ManagedAdvisoryParent(context.Context, string) (bool, error) {
	return repository.managedParent, nil
}
func (repository *memoryRepository) RecordIngestionFailure(_ context.Context, document RawDocument, _ string, code parsing.ErrorCode) error {
	if repository.failureError != nil {
		return repository.failureError
	}
	repository.splitCode = code
	repository.failureRawID = document.ID
	repository.pending = true
	return nil
}
func (repository *memoryRepository) CompleteParsedRevision(
	_ context.Context, _ RawDocument, _ string, decision dedupe.ProcessResult,
	revisionID string, capabilities HandoffCapabilities,
) error {
	if repository.completeError != nil {
		return repository.completeError
	}
	repository.completeDecision = decision
	repository.completeRevision = revisionID
	repository.completeCapabilities = capabilities
	repository.pending = false
	return nil
}

type fixtureFetcher struct {
	result    fetcher.Result
	err       error
	results   []fetcher.Result
	errors    []error
	endpoints []Endpoint
	calls     int
}

func (fixture *fixtureFetcher) Fetch(_ context.Context, endpoint Endpoint) (fetcher.Result, error) {
	fixture.endpoints = append(fixture.endpoints, endpoint)
	index := fixture.calls
	fixture.calls++
	if index < len(fixture.results) {
		var err error
		if index < len(fixture.errors) {
			err = fixture.errors[index]
		}
		return fixture.results[index], err
	}
	return fixture.result, fixture.err
}

func advisoryResult(now time.Time, url string, next string, etag string) fetcher.Result {
	return fetcher.Result{
		Outcome:     fetcher.OutcomeStored,
		NextPageURL: next,
		Checkpoint:  fetcher.Checkpoint{ETag: etag},
		Attempts: []fetcher.Attempt{{
			AttemptedAt: now, CompletedAt: now, StatusCode: 200,
			FinalURL: url,
		}},
	}
}

func TestGlobalAdvisoryPollBoundsPagesAndResumesStoredCursor(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	page2 := rootURL + "&after=page2"
	page3 := rootURL + "&after=page3"
	page4 := rootURL + "&after=page4"
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
	}}
	provider := &fixtureFetcher{results: []fetcher.Result{
		advisoryResult(now, rootURL, page2, `"root-etag"`),
		advisoryResult(now, page2, page3, `"page-etag"`),
		advisoryResult(now, page3, page4, `"other-page-etag"`),
	}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 || len(repository.recorded) != 3 {
		t.Fatalf("bounded first poll: calls=%d records=%d", provider.calls, len(repository.recorded))
	}
	if got := repository.recorded[2].Checkpoint; got.Cursor != page4 || got.ETag != `"root-etag"` || got.ProviderState["advisoryScanStartedAt"] == nil {
		t.Fatalf("durable continuation checkpoint = %+v", got)
	}
	for index, want := range []string{rootURL, page2, page3} {
		if provider.endpoints[index].URL != want {
			t.Fatalf("page %d requested %q, want %q", index, provider.endpoints[index].URL, want)
		}
		if index > 0 && (provider.endpoints[index].Checkpoint.ETag != "" || provider.endpoints[index].Checkpoint.LastModified != "") {
			t.Fatalf("continuation page %d received conditional validators", index)
		}
	}

	repository.endpoint.Checkpoint = repository.recorded[2].Checkpoint
	provider = &fixtureFetcher{results: []fetcher.Result{
		{Outcome: fetcher.OutcomeNotModified, Checkpoint: repository.endpoint.Checkpoint,
			Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 304, FinalURL: rootURL}}},
		advisoryResult(now, page4, "", `"last-page-etag"`),
	}}
	poller.fetcher = provider
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || repository.recorded[4].Checkpoint.Cursor != "" {
		t.Fatalf("resume did not complete: calls=%d checkpoint=%+v", provider.calls, repository.recorded[4].Checkpoint)
	}
	if provider.endpoints[0].Checkpoint.ETag != `"root-etag"` {
		t.Fatal("root validator was lost while resuming a pending scan")
	}
	if got := repository.recorded[4].Checkpoint; got.ETag != `"root-etag"` || got.ProviderState["advisoryScanCompletedAt"] == nil || got.ProviderState["advisoryScanStartedAt"] != nil {
		t.Fatalf("completed scan checkpoint = %+v", got)
	}
}

func TestGlobalAdvisoryPollRestartsCompletedScanDaily(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	page2 := rootURL + "&after=page2"
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
		Checkpoint: fetcher.Checkpoint{
			ETag: `"root-etag"`, ProviderState: map[string]any{
				"advisoryScanCompletedAt": now.Add(-25 * time.Hour).Format(time.RFC3339Nano),
			},
		},
	}}
	provider := &fixtureFetcher{results: []fetcher.Result{
		advisoryResult(now, rootURL, page2, `"fresh-root-etag"`),
		advisoryResult(now, page2, "", ""),
	}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || provider.endpoints[0].Checkpoint.ETag != "" ||
		repository.recorded[1].Checkpoint.ETag != `"fresh-root-etag"` {
		t.Fatalf("daily full scan did not restart with a fresh root: calls=%d root=%+v last=%+v",
			provider.calls, provider.endpoints[0].Checkpoint, repository.recorded[1].Checkpoint)
	}
}

func TestGlobalAdvisoryNotModifiedKeepsCompletionTime(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	completedAt := now.Add(-time.Hour).Format(time.RFC3339Nano)
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
		Checkpoint: fetcher.Checkpoint{ETag: `"root-etag"`, ProviderState: map[string]any{
			"advisoryScanCompletedAt": completedAt,
		}},
	}}
	provider := &fixtureFetcher{result: fetcher.Result{
		Outcome: fetcher.OutcomeNotModified, Checkpoint: repository.endpoint.Checkpoint,
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 304, FinalURL: rootURL}},
	}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || provider.endpoints[0].Checkpoint.ETag != `"root-etag"` ||
		repository.recorded[0].Checkpoint.ProviderState["advisoryScanCompletedAt"] != completedAt {
		t.Fatalf("304 changed completion time or lost validator: calls=%d checkpoint=%+v",
			provider.calls, repository.recorded[0].Checkpoint)
	}
}

func TestGlobalAdvisoryContinuationFailurePreservesStoredCursor(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	page2 := rootURL + "&after=page2"
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
	}}
	provider := &fixtureFetcher{results: []fetcher.Result{
		advisoryResult(now, rootURL, page2, `"root-etag"`),
		{Outcome: fetcher.OutcomeFailed, Attempts: []fetcher.Attempt{{
			AttemptedAt: now, CompletedAt: now, StatusCode: 429,
			FinalURL: page2, ErrorCode: fetcher.ErrorUnexpectedStatus,
		}}},
	}, errors: []error{nil, &fetcher.FetchError{Code: fetcher.ErrorUnexpectedStatus, Retryable: true, Err: errors.New("rate limited")}}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || len(repository.recorded) != 2 || repository.recorded[0].Checkpoint.Cursor != page2 || repository.recorded[1].Outcome != fetcher.OutcomeFailed {
		t.Fatalf("failure lost cursor or retried burst: calls=%d records=%+v", provider.calls, repository.recorded)
	}
}

func TestGlobalAdvisoryContinuationCannotCompleteFromNotModified(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	page2 := rootURL + "&after=page2"
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
	}}
	provider := &fixtureFetcher{results: []fetcher.Result{
		advisoryResult(now, rootURL, page2, `"root-etag"`),
		{Outcome: fetcher.OutcomeNotModified, Attempts: []fetcher.Attempt{{
			AttemptedAt: now, CompletedAt: now, StatusCode: 304, FinalURL: page2,
		}}},
	}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if len(repository.recorded) != 2 || repository.recorded[0].Checkpoint.Cursor != page2 ||
		repository.recorded[1].Outcome != fetcher.OutcomeFailed {
		t.Fatalf("304 continuation falsely completed scan: %+v", repository.recorded)
	}
}

func TestGlobalAdvisoryPollRejectsUntrustedCheckpointBeforeNetwork(t *testing.T) {
	rootURL := sources.GlobalReviewedAdvisoriesURL
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories,
		URL:       rootURL, Checkpoint: fetcher.Checkpoint{Cursor: "https://evil.example/advisories?after=stolen"},
	}}
	provider := &fixtureFetcher{}
	poller, err := NewPoller(repository, provider, clock.NewFixed(time.Now()), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err == nil || provider.calls != 0 {
		t.Fatalf("untrusted checkpoint accepted: calls=%d err=%v", provider.calls, err)
	}
}

func TestGlobalAdvisoryPollHoldsCursorCycleForReview(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	rootURL := sources.GlobalReviewedAdvisoriesURL
	pageA := rootURL + "&after=page-a"
	pageB := rootURL + "&after=page-b"
	repository := &memoryRepository{endpoint: &Endpoint{
		RegistryID: "github-global-advisories", SourceID: "github-global-advisories",
		Connector: sources.ConnectorGitHubAdvisories, URL: rootURL,
	}}
	provider := &fixtureFetcher{results: []fetcher.Result{
		advisoryResult(now, rootURL, pageA, `"root-etag"`),
		advisoryResult(now, pageA, pageB, ""),
		advisoryResult(now, pageB, pageA, ""),
	}}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 || len(repository.recorded) != 3 ||
		advisoryScanIssue(repository.recorded[2].Checkpoint) != "cursor_cycle" {
		t.Fatalf("cursor cycle was not held for review: calls=%d records=%+v", provider.calls, repository.recorded)
	}
	encoded, err := json.Marshal(repository.recorded[2].Checkpoint.ProviderState)
	if err != nil {
		t.Fatal(err)
	}
	repository.endpoint.Checkpoint = repository.recorded[2].Checkpoint
	if err := json.Unmarshal(encoded, &repository.endpoint.Checkpoint.ProviderState); err != nil {
		t.Fatal(err)
	}
	provider = &fixtureFetcher{results: []fetcher.Result{{
		Outcome: fetcher.OutcomeNotModified, Checkpoint: repository.endpoint.Checkpoint,
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, StatusCode: 304, FinalURL: rootURL}},
	}}}
	poller.fetcher = provider
	if err := poller.Poll(context.Background(), repository.endpoint.RegistryID); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || advisoryScanIssue(repository.recorded[3].Checkpoint) != "cursor_cycle" {
		t.Fatalf("cycle did not survive restart: calls=%d checkpoint=%+v", provider.calls, repository.recorded[3].Checkpoint)
	}
}

func TestGlobalAdvisoryScanCapsPageTraversal(t *testing.T) {
	checkpoint := fetcher.Checkpoint{ProviderState: map[string]any{
		"advisoryScanPages": float64(10000),
	}}
	rootURL := sources.GlobalReviewedAdvisoriesURL
	setAdvisoryScanState(&checkpoint, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
		rootURL+"&after=next", false, rootURL+"&after=current")
	if got := advisoryScanIssue(checkpoint); got != "page_limit" {
		t.Fatalf("page ceiling issue = %q", got)
	}
}

func TestPollSkipsPausedSourceAndRecordsProviderFailure(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	repository := &memoryRepository{dueCount: 2}
	provider := &fixtureFetcher{}
	poller, err := NewPoller(repository, provider, clock.NewFixed(now), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
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

func TestDisabledPollerReconcilesPendingWithoutFetchingSources(t *testing.T) {
	repository := &memoryRepository{dueCount: 3, pendingCount: 2, endpoint: &Endpoint{RegistryID: "active"}}
	provider := &fixtureFetcher{}
	poller, err := NewPoller(repository, provider, clock.NewFixed(time.Now()), slog.New(slog.NewTextHandler(io.Discard, nil)), false, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := poller.Reconcile(context.Background()); err != nil || count != 2 {
		t.Fatalf("disabled Reconcile() = %d, %v", count, err)
	}
	if err := poller.Poll(context.Background(), "active"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 || repository.scheduleCalls != 0 || repository.pendingCalls != 1 {
		t.Fatal("disabled poller made a provider request")
	}
}

func TestPollerReconcileSchedulesSourcesEvenWhenIntelligenceRecoveryFails(t *testing.T) {
	pendingErr := errors.New("research backlog unavailable")
	repository := &memoryRepository{dueCount: 2, pendingError: pendingErr}
	poller, err := NewPoller(repository, &fixtureFetcher{}, clock.NewFixed(time.Now()), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	count, err := poller.Reconcile(context.Background())
	if count != 2 || !errors.Is(err, pendingErr) {
		t.Fatalf("Reconcile() = %d, %v; want due sources and pending error", count, err)
	}
	if got := strings.Join(repository.reconcileOrder, ","); got != "schedule,pending" {
		t.Fatalf("reconcile order = %q; want source scheduling first", got)
	}
}

func TestPollerReconcileAttemptsIntelligenceRecoveryWhenSchedulingFails(t *testing.T) {
	scheduleErr := errors.New("source schedule unavailable")
	repository := &memoryRepository{pendingCount: 3, scheduleError: scheduleErr}
	poller, err := NewPoller(repository, &fixtureFetcher{}, clock.NewFixed(time.Now()), slog.New(slog.NewTextHandler(io.Discard, nil)), true, HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	count, err := poller.Reconcile(context.Background())
	if count != 3 || !errors.Is(err, scheduleErr) || repository.pendingCalls != 1 {
		t.Fatalf("Reconcile() = %d, %v; want pending recovery and schedule error", count, err)
	}
}

type fixtureReader struct {
	payload string
	key     string
}

func TestManagedAdvisoryParentSkipsLegacyParserJob(t *testing.T) {
	repository := &memoryRepository{managedParent: true, document: RawDocument{
		ID: "managed-parent", SourceID: "advisories",
		Connector: sources.ConnectorGitHubAdvisories,
		ObjectKey: "should-not-be-read",
	}}
	reader := &fixtureReader{}
	worker, err := NewParseWorker(repository, reader, &fixtureParser{},
		&fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		HandoffCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "advisories", "managed-parent"); err != nil {
		t.Fatal(err)
	}
	if reader.key != "" || repository.entries != nil {
		t.Fatalf("legacy parser touched managed parent: read key %q entries %d",
			reader.key, len(repository.entries))
	}
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
	worker, err := NewParseWorker(repository, reader, parser, deduper, slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{EmbeddingModelID: "text-embedding-3-small", ExtractEnabled: true})
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
	if deduper.request.RevisionID != "revision" || len(repository.entries) != 1 ||
		repository.completeRevision != "revision" || !repository.completeCapabilities.ExtractEnabled {
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
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{})
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
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, deduper, slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{})
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
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload}, parser, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{})
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
	worker, err := NewParseWorker(repository, &fixtureReader{payload: feed}, &fixtureParser{}, &fixtureDeduper{}, slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{})
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

func TestLeafHandoffFailureRetainsReplayUntilSuccessfulRetry(t *testing.T) {
	payload := `{"id":"rss:one","url":"https://example.com/one","title":"First","contentText":"Body"}`
	repository := &memoryRepository{document: RawDocument{
		ID: "child", SourceID: "source", Connector: sources.ConnectorSourceEntry,
		URL: "https://example.com/one", ContentType: "application/json", ObjectKey: "child.json",
		ContentPolicy: "link-and-excerpt", MaxResponseBytes: 1024,
		RawSHA256: sha256.Sum256([]byte(payload)), ParentRawID: "parent", SourceEntryID: "one",
	}, completeError: errors.New("queue unavailable")}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload},
		&fixtureParser{}, &fixtureDeduper{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{ExtractEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Parse(context.Background(), "registry", "child"); err == nil {
		t.Fatal("downstream handoff failure was accepted")
	}
	if !repository.pending || repository.completeRevision != "" ||
		repository.splitCode != "source_handoff_failed" || repository.failureRawID != "child" {
		t.Fatal("failed handoff did not create the raw replay marker")
	}
	repository.completeError = nil
	if err := worker.Parse(context.Background(), "registry", "child"); err != nil {
		t.Fatal(err)
	}
	if repository.pending || repository.completeRevision != "revision" {
		t.Fatal("replayed handoff did not complete")
	}
}

func TestLeafHandoffFailurePreservesBothErrorsWhenMarkerWriteFails(t *testing.T) {
	payload := `{"id":"rss:one","url":"https://example.com/one","title":"First","contentText":"Body"}`
	handoffErr := errors.New("queue unavailable")
	markerErr := errors.New("database unavailable")
	repository := &memoryRepository{document: RawDocument{
		ID: "child", SourceID: "source", Connector: sources.ConnectorSourceEntry,
		URL: "https://example.com/one", ContentType: "application/json", ObjectKey: "child.json",
		MaxResponseBytes: 1024, RawSHA256: sha256.Sum256([]byte(payload)),
		ParentRawID: "parent", SourceEntryID: "one",
	}, completeError: handoffErr, failureError: markerErr}
	worker, err := NewParseWorker(repository, &fixtureReader{payload: payload},
		&fixtureParser{}, &fixtureDeduper{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), HandoffCapabilities{ExtractEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	parseErr := worker.Parse(context.Background(), "registry", "child")
	if !errors.Is(parseErr, handoffErr) || !errors.Is(parseErr, markerErr) {
		t.Fatalf("handoff failure lost the enqueue or marker error: %v", parseErr)
	}
}
