package parsing

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

type processorClock struct {
	times []time.Time
	next  int
}

func (configured *processorClock) Now() time.Time {
	if configured.next >= len(configured.times) {
		return configured.times[len(configured.times)-1]
	}
	value := configured.times[configured.next]
	configured.next++
	return value
}

type processorObjectStore struct {
	stageError  error
	commitError error
	abortError  error
	corrupt     bool
	stagedBody  string
	commitKey   string
	abortCount  int
}

func (store *processorObjectStore) Stage(_ context.Context, contentType string, body io.Reader) (storage.StagedObject, error) {
	if store.stageError != nil {
		return storage.StagedObject{}, store.stageError
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return storage.StagedObject{}, err
	}
	store.stagedBody = string(payload)
	digest := sha256.Sum256(payload)
	if store.corrupt {
		digest = sha256.Sum256([]byte("corrupt"))
	}
	return storage.StagedObject{
		TemporaryKey: "_incoming/0123456789abcdef0123456789abcdef",
		SHA256:       digest,
		Bytes:        int64(len(payload)),
		ContentType:  contentType,
	}, nil
}

func (store *processorObjectStore) Commit(_ context.Context, _ storage.StagedObject, objectKey string) error {
	store.commitKey = objectKey
	return store.commitError
}

func (store *processorObjectStore) Abort(context.Context, storage.StagedObject) error {
	store.abortCount++
	return store.abortError
}

type processorRevisionRepository struct {
	recordResult RecordResult
	recordError  error
	failureError error
	successes    []RecordRequest
	failures     []FailureRequest
}

func (repository *processorRevisionRepository) RecordSuccess(_ context.Context, request RecordRequest) (RecordResult, error) {
	repository.successes = append(repository.successes, request)
	return repository.recordResult, repository.recordError
}

func (repository *processorRevisionRepository) RecordFailure(_ context.Context, request FailureRequest) error {
	repository.failures = append(repository.failures, request)
	return repository.failureError
}

func TestProcessorPersistsContentAddressedNormalizedRevision(t *testing.T) {
	started := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	objects := &processorObjectStore{}
	revisions := &processorRevisionRepository{recordResult: RecordResult{RevisionID: "revision-1", Outcome: "created"}}
	processor, err := NewProcessor(objects, revisions, &processorClock{times: []time.Time{started, started.Add(25 * time.Millisecond)}})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	result, err := processor.Process(context.Background(), ProcessRequest{
		RawDocumentID: "raw-1",
		SourceID:      "go-blog",
		Parse: Request{
			Connector:   sources.ConnectorStructuredAPI,
			URL:         "https://example.test/status",
			ContentType: "application/json",
			Body:        strings.NewReader(`{"status":"operational","services":[{"id":"api","name":"API","status":"operational"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	wantKey, err := storage.NormalizedObjectKey("go-blog", result.Parsed.NormalizedSHA256)
	if err != nil {
		t.Fatalf("NormalizedObjectKey() error = %v", err)
	}
	if result.ObjectKey != wantKey || objects.commitKey != wantKey || objects.stagedBody != result.Parsed.NormalizedText {
		t.Fatalf("result=%+v commitKey=%q stagedBody=%q", result, objects.commitKey, objects.stagedBody)
	}
	if len(revisions.successes) != 1 || len(revisions.failures) != 0 {
		t.Fatalf("successes=%d failures=%d", len(revisions.successes), len(revisions.failures))
	}
	recorded := revisions.successes[0]
	if recorded.ObjectKey != wantKey || !recorded.AttemptedAt.Equal(started) || !recorded.CompletedAt.Equal(started.Add(25*time.Millisecond)) {
		t.Fatalf("RecordSuccess() request = %+v", recorded)
	}
}

func TestProcessorRecordsParserAndStorageFailures(t *testing.T) {
	started := time.Date(2026, time.August, 29, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		parse      Request
		objects    *processorObjectStore
		wantCode   ErrorCode
		wantAborts int
	}{
		{
			name:     "parse",
			parse:    Request{Connector: sources.ConnectorRSS, URL: "https://example.test/feed", ContentType: "application/rss+xml", Body: strings.NewReader("not a feed")},
			objects:  &processorObjectStore{},
			wantCode: ErrorInvalidDocument,
		},
		{
			name:     "stage",
			parse:    validProcessorParseRequest(),
			objects:  &processorObjectStore{stageError: errors.New("storage unavailable")},
			wantCode: ErrorObjectStorage,
		},
		{
			name:       "integrity",
			parse:      validProcessorParseRequest(),
			objects:    &processorObjectStore{corrupt: true},
			wantCode:   ErrorObjectStorage,
			wantAborts: 1,
		},
		{
			name:       "commit",
			parse:      validProcessorParseRequest(),
			objects:    &processorObjectStore{commitError: errors.New("copy failed")},
			wantCode:   ErrorObjectStorage,
			wantAborts: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			revisions := &processorRevisionRepository{}
			processor, err := NewProcessor(test.objects, revisions, &processorClock{times: []time.Time{started, started.Add(time.Second)}})
			if err != nil {
				t.Fatalf("NewProcessor() error = %v", err)
			}
			_, processError := processor.Process(context.Background(), ProcessRequest{RawDocumentID: "raw-1", SourceID: "go-blog", Parse: test.parse})
			if processError == nil {
				t.Fatal("Process() succeeded")
			}
			if len(revisions.failures) != 1 || revisions.failures[0].ErrorCode != test.wantCode {
				t.Fatalf("failures = %+v, want code %q", revisions.failures, test.wantCode)
			}
			if test.objects.abortCount != test.wantAborts || len(revisions.successes) != 0 {
				t.Fatalf("abortCount=%d successes=%d", test.objects.abortCount, len(revisions.successes))
			}
		})
	}
}

func TestProcessorSurfacesRevisionAndFailureRecordingErrors(t *testing.T) {
	started := time.Date(2026, time.August, 29, 14, 0, 0, 0, time.UTC)
	revisionFailure := &processorRevisionRepository{recordError: errors.New("database unavailable")}
	processor, err := NewProcessor(&processorObjectStore{}, revisionFailure, &processorClock{times: []time.Time{started, started.Add(time.Second)}})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), ProcessRequest{RawDocumentID: "raw-1", SourceID: "go-blog", Parse: validProcessorParseRequest()}); err == nil || !strings.Contains(err.Error(), "record normalized revision") {
		t.Fatalf("Process() error = %v", err)
	}
	if len(revisionFailure.failures) != 0 {
		t.Fatalf("database success failure was misrecorded as parse failure: %+v", revisionFailure.failures)
	}

	failureRepository := &processorRevisionRepository{failureError: errors.New("attempt log unavailable")}
	processor, err = NewProcessor(&processorObjectStore{}, failureRepository, &processorClock{times: []time.Time{started, started.Add(time.Second)}})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	_, err = processor.Process(context.Background(), ProcessRequest{
		RawDocumentID: "raw-1",
		SourceID:      "go-blog",
		Parse:         Request{Connector: sources.ConnectorRSS, URL: "https://example.test/feed", ContentType: "application/rss+xml", Body: strings.NewReader("invalid")},
	})
	if err == nil || !strings.Contains(err.Error(), "attempt log unavailable") || errorCode(err) != ErrorInvalidDocument {
		t.Fatalf("Process() joined error = %v", err)
	}
}

func TestNewProcessorAndProcessValidateDependenciesAndIdentity(t *testing.T) {
	objects := &processorObjectStore{}
	revisions := &processorRevisionRepository{}
	configuredClock := &processorClock{times: []time.Time{time.Now()}}
	if _, err := NewProcessor(nil, revisions, configuredClock); err == nil {
		t.Fatal("NewProcessor() accepted missing object storage")
	}
	if _, err := NewProcessor(objects, nil, configuredClock); err == nil {
		t.Fatal("NewProcessor() accepted missing revision repository")
	}
	if _, err := NewProcessor(objects, revisions, nil); err == nil {
		t.Fatal("NewProcessor() accepted missing clock")
	}
	processor, err := NewProcessor(objects, revisions, configuredClock)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), ProcessRequest{SourceID: "go-blog"}); err == nil {
		t.Fatal("Process() accepted a missing raw document id")
	}
}

func TestParserNameForEveryConnector(t *testing.T) {
	tests := map[sources.Connector]string{
		sources.ConnectorAtom:             "gofeed-atom",
		sources.ConnectorRSS:              "gofeed-rss",
		sources.ConnectorJSONFeed:         "gofeed-json_feed",
		sources.ConnectorPage:             "readeck-readability",
		sources.ConnectorGitHubReleases:   "github-releases-json",
		sources.ConnectorGitHubAdvisories: "github-advisories-json",
		sources.ConnectorRegistry:         "registry-json",
		sources.ConnectorStructuredAPI:    "structured-api-json",
		sources.Connector("unknown"):      "unsupported",
	}
	for connector, expected := range tests {
		if got := parserNameFor(connector); got != expected {
			t.Errorf("parserNameFor(%q) = %q, want %q", connector, got, expected)
		}
	}
}

func validProcessorParseRequest() Request {
	return Request{
		Connector:   sources.ConnectorStructuredAPI,
		URL:         "https://example.test/status",
		ContentType: "application/json",
		Body:        strings.NewReader(`{"status":"operational","services":[{"id":"api","name":"API","status":"operational"}]}`),
	}
}
