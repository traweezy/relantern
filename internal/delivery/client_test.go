package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/digest"
)

func TestDiscordDisablesMentionsAndBoundsItems(t *testing.T) {
	var received struct {
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
		Embeds []struct {
			Title string `json:"title"`
		} `json:"embeds"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("wait") != "true" || request.Header.Get("Idempotency-Key") != "digest-key-0123456789" {
			t.Errorf("request URL/headers = %s %+v", request.URL, request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"discord-message"}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{
		Mode: "live", DiscordURL: server.URL, PublicBaseURL: "https://private.example",
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]digest.DigestRenderedItem, 11)
	for index := range items {
		items[index] = digest.DigestRenderedItem{
			Headline: "@everyone *unsafe*", Summary: "summary", Reason: "reason",
			AppPath: "/story/value", SourceURL: "https://example.test/source",
		}
	}
	receipt, err := client.Send(context.Background(), digest.Delivery{
		DigestID: "digest", Channel: digest.ChannelDiscord, Attempt: 1,
		IdempotencyKey: "digest-key-0123456789", Payload: digest.DigestPayload{
			ExecutiveSummary: "@everyone update", Items: items,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "discord-message" || len(received.AllowedMentions.Parse) != 0 || len(received.Embeds) != 10 || strings.Contains(received.Embeds[0].Title, "@everyone") {
		t.Fatalf("receipt/capture = %+v %+v", receipt, received)
	}
}

func TestProviderFailureClassification(t *testing.T) {
	for _, fixture := range []struct {
		status    int
		permanent bool
	}{
		{status: http.StatusBadRequest, permanent: true},
		{status: http.StatusTooManyRequests},
		{status: http.StatusServiceUnavailable},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(fixture.status)
		}))
		client, err := New(Config{Mode: "live", DiscordURL: server.URL, PublicBaseURL: "https://private.example", RequestTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Send(context.Background(), digest.Delivery{
			DigestID: "digest", Channel: digest.ChannelDiscord, Attempt: 1,
			IdempotencyKey: "digest-key-0123456789", Payload: digest.DigestPayload{},
		})
		server.Close()
		if err == nil || Permanent(err) != fixture.permanent {
			t.Fatalf("status %d error = %v permanent = %t", fixture.status, err, Permanent(err))
		}
	}
}
