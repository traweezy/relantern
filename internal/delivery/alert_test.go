package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/alert"
)

func testAlertDelivery() alert.Delivery {
	return alert.Delivery{
		AlertID: "alert-1", Channel: "discord", IdempotencyKey: "alert:alert-1:discord",
		Attempt: 1, Title: "Critical advisory for @everyone *package*",
		SourceURL:   "https://github.com/advisories/GHSA-abcd-1234-5678",
		PackageName: "@scope/<script>", Ecosystem: "npm", CurrentVersion: "1.2.0",
		VersionRange: ">= 1.0.0, < 1.3.0", PatchedVersion: "1.3.0",
	}
}

func TestAlertLogCaptureUsesStableIdempotency(t *testing.T) {
	t.Parallel()
	var received struct {
		Kind           string `json:"kind"`
		AlertID        string `json:"alertId"`
		Channel        string `json:"channel"`
		IdempotencyKey string `json:"idempotencyKey"`
		Attempt        int    `json:"attempt"`
		Payload        struct {
			Title       string `json:"title"`
			PackageName string `json:"packageName"`
		} `json:"payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Idempotency-Key") != "alert:alert-1:discord" || request.Header.Get("Authorization") != "" {
			t.Errorf("capture headers = %+v", request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode capture: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{Mode: "log", CaptureURL: server.URL, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.SendAlert(context.Background(), testAlertDelivery())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "capture:alert-1" || received.Kind != "critical_alert" ||
		received.AlertID != "alert-1" || received.Channel != "discord" ||
		received.IdempotencyKey != "alert:alert-1:discord" || received.Attempt != 1 ||
		received.Payload.Title != testAlertDelivery().Title || received.Payload.PackageName != testAlertDelivery().PackageName {
		t.Fatalf("receipt/capture = %+v %+v", receipt, received)
	}
}

func TestAlertDiscordEscapesUntrustedContentAndSuppressesMentions(t *testing.T) {
	t.Parallel()
	var received struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
		Embeds []struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"embeds"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("wait") != "true" || request.Header.Get("Idempotency-Key") != "alert:alert-1:discord" {
			t.Errorf("request URL/headers = %s %+v", request.URL, request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode Discord request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"discord-alert-message"}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{
		Mode: "live", DiscordURL: server.URL, PublicBaseURL: "https://private.example",
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.SendAlert(context.Background(), testAlertDelivery())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "discord-alert-message" || len(received.AllowedMentions.Parse) != 0 || len(received.Embeds) != 1 {
		t.Fatalf("receipt/Discord payload = %+v %+v", receipt, received)
	}
	if strings.Contains(received.Embeds[0].Title, "@everyone") || strings.Contains(received.Embeds[0].Title, "*package*") ||
		strings.Contains(received.Embeds[0].Fields[0].Value, "@scope") ||
		received.Embeds[0].URL != testAlertDelivery().SourceURL {
		t.Fatalf("unescaped Discord alert = %+v", received.Embeds[0])
	}
}

func TestAlertEmailEscapesHTMLAndIncludesPlainText(t *testing.T) {
	t.Parallel()
	var received struct {
		From    string            `json:"from"`
		To      []string          `json:"to"`
		Subject string            `json:"subject"`
		Text    string            `json:"text"`
		HTML    string            `json:"html"`
		Headers map[string]string `json:"headers"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Idempotency-Key") != "alert:alert-1:email" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("email headers = %+v", request.Header)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode email request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"email-alert-message"}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{
		Mode: "live", ResendURL: server.URL, ResendAPIKey: "test-key",
		EmailFrom: "alerts@example.test", EmailTo: "reader@example.test",
		PublicBaseURL: "https://private.example", RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := testAlertDelivery()
	request.Channel = "email"
	request.IdempotencyKey = "alert:alert-1:email"
	request.Title = "Critical advisory <img src=x onerror=alert(1)>"
	receipt, err := client.SendAlert(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "email-alert-message" || received.Headers["X-Relantern-Alert-ID"] != request.AlertID ||
		!strings.Contains(received.Text, request.Title) || !strings.Contains(received.Text, request.SourceURL) ||
		!strings.Contains(received.HTML, "&lt;img src=x onerror=alert(1)&gt;") ||
		strings.Contains(received.HTML, "<img") || strings.Contains(received.HTML, "@scope/<script>") ||
		!strings.Contains(received.HTML, `<a href="https://github.com/advisories/GHSA-abcd-1234-5678">Read the official advisory</a>`) ||
		!strings.Contains(received.HTML, `<html lang="en" dir="ltr">`) ||
		!strings.HasPrefix(received.Subject, "Critical security alert: ") {
		t.Fatalf("receipt/email payload = %+v %+v", receipt, received)
	}
}

func TestAlertRejectsInvalidOrUnconfiguredRequests(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name   string
		config Config
		change func(*alert.Delivery)
	}{
		{name: "disabled", config: Config{Mode: "disabled"}},
		{name: "invalid source", config: Config{Mode: "log", CaptureURL: "https://example.test/capture"}, change: func(request *alert.Delivery) { request.SourceURL = "javascript:alert(1)" }},
		{name: "header injection", config: Config{Mode: "log", CaptureURL: "https://example.test/capture"}, change: func(request *alert.Delivery) { request.Title = "unsafe\r\nBcc: attacker@example.test" }},
		{name: "missing Discord config", config: Config{Mode: "live", PublicBaseURL: "https://private.example"}},
		{name: "missing email config", config: Config{Mode: "live", PublicBaseURL: "https://private.example"}, change: func(request *alert.Delivery) { request.Channel = "email" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			client, err := New(Config{
				Mode: fixture.config.Mode, CaptureURL: fixture.config.CaptureURL,
				PublicBaseURL: fixture.config.PublicBaseURL, RequestTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := testAlertDelivery()
			if fixture.change != nil {
				fixture.change(&request)
			}
			_, err = client.SendAlert(context.Background(), request)
			if err == nil || !Permanent(err) {
				t.Fatalf("SendAlert error = %v, want permanent", err)
			}
			var classified interface{ Permanent() bool }
			if !errors.As(err, &classified) || !classified.Permanent() {
				t.Fatalf("SendAlert error = %v, want method classification", err)
			}
		})
	}
}

func TestAlertLongOfficialMetadataRemainsDeliverable(t *testing.T) {
	request := testAlertDelivery()
	request.Title = strings.Repeat("*", 300)
	request.SourceURL = "https://github.com/advisories/GHSA-abcd-1234-5678/" + strings.Repeat("a", 2100)
	if err := validateAlertDelivery(request); err != nil {
		t.Fatalf("valid bounded alert rejected: %v", err)
	}
	formatted := discordAlertTitle(request.Title)
	if len([]rune(formatted)) > 256 || !strings.HasSuffix(formatted, "…") {
		t.Fatalf("Discord title length %d or suffix invalid", len([]rune(formatted)))
	}
}
