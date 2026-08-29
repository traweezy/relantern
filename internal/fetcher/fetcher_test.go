package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/storage"
)

var fixtureNow = time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)

type responseDoer struct {
	responses []*http.Response
	requests  []*http.Request
}

func (doer *responseDoer) Do(request *http.Request) (*http.Response, error) {
	doer.requests = append(doer.requests, request.Clone(request.Context()))
	if len(doer.responses) == 0 {
		return nil, errors.New("no fixture response")
	}
	response := doer.responses[0]
	doer.responses = doer.responses[1:]
	response.Request = request
	return response, nil
}

type memoryStore struct {
	staged  map[string][]byte
	objects map[string][]byte
	aborts  int
	stages  int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{staged: make(map[string][]byte), objects: make(map[string][]byte)}
}

func (store *memoryStore) Stage(_ context.Context, contentType string, body io.Reader) (storage.StagedObject, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return storage.StagedObject{}, err
	}
	store.stages++
	key := "_incoming/fixture"
	store.staged[key] = payload
	return storage.StagedObject{
		TemporaryKey: key,
		SHA256:       sha256.Sum256(payload),
		Bytes:        int64(len(payload)),
		ContentType:  contentType,
	}, nil
}

func (store *memoryStore) Commit(_ context.Context, staged storage.StagedObject, key string) error {
	store.objects[key] = bytes.Clone(store.staged[staged.TemporaryKey])
	delete(store.staged, staged.TemporaryKey)
	return nil
}

func (store *memoryStore) Abort(_ context.Context, staged storage.StagedObject) error {
	store.aborts++
	delete(store.staged, staged.TemporaryKey)
	return nil
}

func TestFetcherStoresBoundedGzipAndSendsCheckpoint(t *testing.T) {
	payload := []byte("<feed>fixture</feed>")
	compressed := gzipPayload(t, payload)
	doer := &responseDoer{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":     {"application/atom+xml; charset=utf-8"},
			"Content-Encoding": {"gzip"},
			"Etag":             {`"fixture-v2"`},
			"Last-Modified":    {"Sat, 29 Aug 2026 11:00:00 GMT"},
		},
		Body:          io.NopCloser(bytes.NewReader(compressed)),
		ContentLength: int64(len(compressed)),
	}}}
	store := newMemoryStore()
	fetcher := newFixtureFetcher(t, doer, store, nil)
	result, err := fetcher.Fetch(context.Background(), fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt), Checkpoint{
		ETag:         `"fixture-v1"`,
		LastModified: "Fri, 28 Aug 2026 11:00:00 GMT",
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Outcome != OutcomeStored || result.Bytes != int64(len(payload)) {
		t.Fatalf("Fetch() result = %+v", result)
	}
	if result.Checkpoint.ETag != `"fixture-v2"` || result.Checkpoint.LastModified == "" {
		t.Fatalf("Fetch() checkpoint = %+v", result.Checkpoint)
	}
	if !strings.HasPrefix(result.ObjectKey, "raw/go-blog/2026/08/29/") || !strings.HasSuffix(result.ObjectKey, ".xml") {
		t.Fatalf("Fetch() object key = %q", result.ObjectKey)
	}
	if !bytes.Equal(store.objects[result.ObjectKey], payload) {
		t.Fatalf("stored body = %q", store.objects[result.ObjectKey])
	}
	request := doer.requests[0]
	if request.Header.Get("If-None-Match") != `"fixture-v1"` || request.Header.Get("If-Modified-Since") == "" {
		t.Fatalf("conditional headers = %v", request.Header)
	}
	if request.Header.Get("User-Agent") != "Relantern/0.0.0 (+https://github.com/traweezy/relantern)" {
		t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
	}
}

func TestSecureClientFetchesOnlyExplicitLocalFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") == "" {
			t.Error("fixture request has no User-Agent")
		}
		response.Header().Set("Content-Type", "application/atom+xml")
		_, _ = response.Write([]byte("<feed/>"))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	parsedPort, err := strconv.ParseUint(serverURL.Port(), 10, 16)
	if err != nil {
		t.Fatalf("parse fixture port: %v", err)
	}
	policy, err := NewPolicy(net.DefaultResolver, []string{serverURL.Hostname()}, []FixtureTarget{{Host: serverURL.Hostname(), Port: uint16(parsedPort)}})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	client, err := NewSecureHTTPClient(policy, nil, DefaultNetworkLimits(), 5)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	configuration := DefaultConfig("Relantern/0.0.0 (+https://github.com/traweezy/relantern)")
	configuration.Now = func() time.Time { return fixtureNow }
	configuration.Sleep = func(context.Context, time.Duration) error { return nil }
	fetcher, err := New(configuration, policy, client, nil, newMemoryStore())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	endpoint := fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt)
	endpoint.URL = server.URL + "/feed"
	endpoint.AllowedHosts = []string{serverURL.Hostname()}
	result, err := fetcher.Fetch(context.Background(), endpoint, Checkpoint{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Outcome != OutcomeStored || result.Bytes != 7 {
		t.Fatalf("Fetch() result = %+v", result)
	}
}

func TestFetcherTreatsNotModifiedAsSuccessfulPoll(t *testing.T) {
	doer := &responseDoer{responses: []*http.Response{{
		StatusCode: http.StatusNotModified,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}}}
	fetcher := newFixtureFetcher(t, doer, newMemoryStore(), nil)
	result, err := fetcher.Fetch(context.Background(), fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt), Checkpoint{ETag: `"fixture-v1"`})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Outcome != OutcomeNotModified || result.Checkpoint.ETag != `"fixture-v1"` || len(result.Attempts) != 1 {
		t.Fatalf("Fetch() result = %+v", result)
	}
}

func TestFetcherRetriesRateLimitThenSucceeds(t *testing.T) {
	doer := &responseDoer{responses: []*http.Response{
		{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": {"1"}},
			Body:       http.NoBody,
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/atom+xml"}},
			Body:       io.NopCloser(strings.NewReader("<feed/>")),
		},
	}}
	var delays []time.Duration
	fetcher := newFixtureFetcher(t, doer, newMemoryStore(), func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	result, err := fetcher.Fetch(context.Background(), fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt), Checkpoint{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Outcome != OutcomeStored || len(result.Attempts) != 2 || len(delays) != 1 || delays[0] != time.Second {
		t.Fatalf("result = %+v, delays = %v", result, delays)
	}
}

func TestFetcherAbortsOversizedAndCompressionBombBodies(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		encoding string
		maximum  int64
		wantCode ErrorCode
	}{
		{name: "oversized", body: []byte("12345"), maximum: 4, wantCode: ErrorCompressedTooLarge},
		{name: "compression bomb", body: gzipPayload(t, bytes.Repeat([]byte("a"), 10_000)), encoding: "gzip", maximum: 20_000, wantCode: ErrorCompressionRatio},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doer := &responseDoer{responses: []*http.Response{{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":     {"application/atom+xml"},
					"Content-Encoding": {test.encoding},
				},
				Body: io.NopCloser(bytes.NewReader(test.body)),
			}}}
			store := newMemoryStore()
			fetcher := newFixtureFetcher(t, doer, store, nil)
			_, err := fetcher.Fetch(context.Background(), fixtureEndpoint(test.maximum, ContentPolicyLinkAndExcerpt), Checkpoint{})
			var fetchError *FetchError
			if !errors.As(err, &fetchError) || fetchError.Code != test.wantCode {
				t.Fatalf("Fetch() error = %v, want code %q", err, test.wantCode)
			}
			if store.aborts != 1 || len(store.objects) != 0 {
				t.Fatalf("store aborts = %d, objects = %d", store.aborts, len(store.objects))
			}
		})
	}
}

func TestFetcherMetadataOnlyHashesWithoutPersistingBody(t *testing.T) {
	doer := &responseDoer{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"version":"1.0.0"}`)),
	}}}
	store := newMemoryStore()
	fetcher := newFixtureFetcher(t, doer, store, nil)
	endpoint := fixtureEndpoint(1024, ContentPolicyMetadataOnly)
	endpoint.ExpectedContentTypes = []string{"application/json"}
	result, err := fetcher.Fetch(context.Background(), endpoint, Checkpoint{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Outcome != OutcomeMetadataOnly || result.ObjectKey != "" || store.stages != 0 || result.SHA256 == ([sha256.Size]byte{}) {
		t.Fatalf("result = %+v, stages = %d", result, store.stages)
	}
}

func TestFetcherRejectsUnexpectedContentAndEncoding(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		wantCode ErrorCode
	}{
		{name: "content type", headers: http.Header{"Content-Type": {"text/html"}}, wantCode: ErrorContentType},
		{name: "content encoding", headers: http.Header{"Content-Type": {"application/atom+xml"}, "Content-Encoding": {"br"}}, wantCode: ErrorUnsupportedEncoding},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doer := &responseDoer{responses: []*http.Response{{
				StatusCode: http.StatusOK,
				Header:     test.headers,
				Body:       io.NopCloser(strings.NewReader("fixture")),
			}}}
			_, err := newFixtureFetcher(t, doer, newMemoryStore(), nil).Fetch(
				context.Background(),
				fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt),
				Checkpoint{},
			)
			var fetchError *FetchError
			if !errors.As(err, &fetchError) || fetchError.Code != test.wantCode {
				t.Fatalf("Fetch() error = %v, want code %q", err, test.wantCode)
			}
		})
	}
}

func TestFetcherRetriesTransportErrors(t *testing.T) {
	store := newMemoryStore()
	policy, err := NewPolicy(
		staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}},
		[]string{"source.example"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	configuration := DefaultConfig("Relantern/0.0.0 (+https://github.com/traweezy/relantern)")
	configuration.Now = func() time.Time { return fixtureNow }
	configuration.Jitter = func(time.Duration) time.Duration { return 0 }
	var sleeps int
	configuration.Sleep = func(context.Context, time.Duration) error {
		sleeps++
		return nil
	}
	fetcher, err := New(configuration, policy, errorDoer{}, nil, store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := fetcher.Fetch(context.Background(), fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt), Checkpoint{})
	var fetchError *FetchError
	if !errors.As(err, &fetchError) || fetchError.Code != ErrorTransport || len(result.Attempts) != 5 || sleeps != 4 {
		t.Fatalf("result = %+v, error = %v, sleeps = %d", result, err, sleeps)
	}
}

func TestFetcherDefersLongRetryAfter(t *testing.T) {
	doer := &responseDoer{responses: []*http.Response{{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": {"120"}},
		Body:       http.NoBody,
	}}}
	_, err := newFixtureFetcher(t, doer, newMemoryStore(), nil).Fetch(
		context.Background(),
		fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt),
		Checkpoint{},
	)
	var fetchError *FetchError
	if !errors.As(err, &fetchError) || fetchError.Code != ErrorUnexpectedStatus || !fetchError.Retryable {
		t.Fatalf("Fetch() error = %v, want deferred retry", err)
	}
}

func TestFetcherConstructionAndEndpointValidation(t *testing.T) {
	policy, err := NewPolicy(staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}}, []string{"source.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	valid := DefaultConfig("Relantern/0.0.0 (+https://github.com/traweezy/relantern)")
	tests := []Config{
		{RetryAttempts: 1, BaseBackoff: time.Second, MaximumRetryDelay: time.Second, MaximumCompressionRatio: 1},
		func() Config { candidate := valid; candidate.RetryAttempts = 0; return candidate }(),
		func() Config { candidate := valid; candidate.BaseBackoff = 0; return candidate }(),
		func() Config { candidate := valid; candidate.MaximumCompressionRatio = 0; return candidate }(),
	}
	for _, configuration := range tests {
		if _, err := New(configuration, policy, errorDoer{}, nil, newMemoryStore()); err == nil {
			t.Fatalf("New(%+v) succeeded", configuration)
		}
	}
	if _, err := New(valid, nil, errorDoer{}, nil, newMemoryStore()); err == nil {
		t.Fatal("New() accepted nil policy")
	}
	if _, err := New(valid, policy, nil, nil, newMemoryStore()); err == nil {
		t.Fatal("New() accepted nil HTTP client")
	}
	if _, err := New(valid, policy, errorDoer{}, nil, nil); err == nil {
		t.Fatal("New() accepted nil object store")
	}

	fetcher := newFixtureFetcher(t, errorDoer{}, newMemoryStore(), nil)
	invalid := fixtureEndpoint(1024, "unsupported")
	if _, err := fetcher.Fetch(context.Background(), invalid, Checkpoint{}); err == nil {
		t.Fatal("Fetch() accepted an unsupported content policy")
	}
}

func TestRetryHelpersBoundAndCancel(t *testing.T) {
	for index := 0; index < 100; index++ {
		if jitter := cryptoJitter(time.Millisecond); jitter < 0 || jitter > time.Millisecond {
			t.Fatalf("cryptoJitter() = %s", jitter)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep() error = %v, want cancellation", err)
	}
	inner := errors.New("fixture")
	fetchError := newFetchError(ErrorTransport, true, inner)
	if fetchError.Error() != "fixture" || !errors.Is(fetchError, inner) {
		t.Fatalf("FetchError methods do not preserve the cause: %v", fetchError)
	}
}

func newFixtureFetcher(t *testing.T, doer HTTPDoer, store ObjectStore, sleeper func(context.Context, time.Duration) error) *Fetcher {
	t.Helper()
	policy, err := NewPolicy(
		staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}},
		[]string{"source.example"},
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	configuration := DefaultConfig("Relantern/0.0.0 (+https://github.com/traweezy/relantern)")
	configuration.Now = func() time.Time { return fixtureNow }
	configuration.Jitter = func(time.Duration) time.Duration { return 0 }
	if sleeper != nil {
		configuration.Sleep = sleeper
	} else {
		configuration.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	fetcher, err := New(configuration, policy, doer, nil, store)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return fetcher
}

func fixtureEndpoint(maximum int64, contentPolicy string) Endpoint {
	return Endpoint{
		ID:                   "go-blog",
		SourceID:             "go-blog",
		URL:                  "https://source.example/feed.xml",
		AllowedHosts:         []string{"source.example"},
		ExpectedContentTypes: []string{"application/atom+xml"},
		MaxBodyBytes:         maximum,
		ContentPolicy:        contentPolicy,
	}
}

func gzipPayload(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("gzip write error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	return compressed.Bytes()
}

type errorDoer struct{}

func (errorDoer) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("fixture transport failure")
}
