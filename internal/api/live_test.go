package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/api"
	"github.com/traweezy/relantern/internal/intelligence"
)

type replayReader struct {
	fixtureReader
	batch intelligence.ReplayBatch
}

func (reader replayReader) LatestCursor(context.Context, time.Time) (int64, error) { return 0, nil }
func (reader replayReader) Replay(context.Context, int64, time.Time, int) (intelligence.ReplayBatch, error) {
	return reader.batch, nil
}
func (reader replayReader) Subscribe(context.Context) (intelligence.LiveSubscription, error) {
	return closedSubscription{}, nil
}

type closedSubscription struct{}

func (closedSubscription) Wait(context.Context) error { return context.Canceled }
func (closedSubscription) Close()                     {}

func liveApplication(reader intelligence.Reader, token string) api.Application {
	return api.New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		api.Info{Version: "test"},
		func(context.Context) error { return nil },
		api.WithIntelligence(reader, token),
	)
}

type blockingReplayReader struct {
	replayReader
	subscribed chan struct{}
}

func (reader blockingReplayReader) Subscribe(context.Context) (intelligence.LiveSubscription, error) {
	reader.subscribed <- struct{}{}
	return blockingSubscription{}, nil
}

type blockingSubscription struct{}

func (blockingSubscription) Wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
func (blockingSubscription) Close() {}

func TestLiveStreamRejectsExcessSubscriptionsWithoutBlocking(t *testing.T) {
	const limit = 4
	token := strings.Repeat("s", 32)
	subscribed := make(chan struct{}, limit+1)
	application := liveApplication(blockingReplayReader{subscribed: subscribed}, token)
	cancels := make([]context.CancelFunc, 0, limit)
	completed := make(chan struct{}, limit)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
		for range cancels {
			select {
			case <-completed:
			case <-time.After(2 * time.Second):
				t.Error("active live stream did not close after cancellation")
			}
		}
	}()
	for range limit {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		request := httptest.NewRequest(http.MethodGet, "/internal/v1/live/stream?after=0", nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer "+token)
		go func() {
			application.Handler.ServeHTTP(httptest.NewRecorder(), request)
			completed <- struct{}{}
		}()
		select {
		case <-subscribed:
		case <-time.After(2 * time.Second):
			t.Fatal("live stream did not subscribe")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/live/stream?after=0", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	application.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "5" {
		t.Fatalf("over-capacity response = %d, Retry-After %q", response.Code, response.Header().Get("Retry-After"))
	}
	select {
	case <-subscribed:
		t.Fatal("over-capacity request opened another subscription")
	default:
	}
	cancels[0]()
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled live stream did not release its slot")
	}
	replacementContext, replacementCancel := context.WithCancel(context.Background())
	cancels[0] = replacementCancel
	replacement := httptest.NewRequest(http.MethodGet, "/internal/v1/live/stream?after=0", nil).WithContext(replacementContext)
	replacement.Header.Set("Authorization", "Bearer "+token)
	go func() {
		application.Handler.ServeHTTP(httptest.NewRecorder(), replacement)
		completed <- struct{}{}
	}()
	select {
	case <-subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("live stream slot was not reusable after cancellation")
	}
}

func TestLiveStreamReplaysDurableEvent(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	reader := replayReader{batch: intelligence.ReplayBatch{Events: []intelligence.ReplayEvent{{
		Cursor: 42,
		Event: intelligence.LiveEvent{
			ID: "42", ObservedAt: time.Unix(10, 0).UTC(), Type: "story-created",
			Story: intelligence.StorySummary{ID: storyID, Headline: "Published story"},
		},
	}}}}
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/live/stream?after=0", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	liveApplication(reader, token).Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q", contentType)
	}
	for _, fragment := range []string{"id: 42\n", "event: story-created\n", `"headline":"Published story"`} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Fatalf("stream missing %q: %s", fragment, response.Body.String())
		}
	}
}

func TestLiveStreamRejectsInvalidCredentialAndCursor(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	application := liveApplication(replayReader{}, token)
	for _, test := range []struct {
		name          string
		authorization string
		path          string
		status        int
	}{
		{"missing credential", "", "/internal/v1/live/stream", http.StatusUnauthorized},
		{"malformed cursor", "Bearer " + token, "/internal/v1/live/stream?after=1x", http.StatusBadRequest},
		{"negative cursor", "Bearer " + token, "/internal/v1/live/stream?after=-1", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()
			application.Handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("response disclosed private service token")
			}
		})
	}
}

func TestLiveStreamSignalsExpiredCursor(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("s", 32)
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/live/stream?after=12", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	liveApplication(replayReader{batch: intelligence.ReplayBatch{ResetRequired: true}}, token).Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: reset_required\n") {
		t.Fatalf("expired cursor response = %d %s", response.Code, response.Body.String())
	}
}
