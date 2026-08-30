package embedding_test

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/fakeprovider"
)

func TestClientReturnsDeterministicBoundedEmbedding(t *testing.T) {
	t.Parallel()
	handler, err := fakeprovider.New(fakeprovider.KindOpenAI, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("fakeprovider.New() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: server.URL, ModelID: embedding.DefaultModelID,
		Dimensions: embedding.DefaultDimensions, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("embedding.NewClient() error = %v", err)
	}
	first, err := client.Embed(context.Background(), "PostgreSQL replication and database failover")
	if err != nil {
		t.Fatalf("first Embed() error = %v", err)
	}
	second, err := client.Embed(context.Background(), "PostgreSQL replication and database failover")
	if err != nil {
		t.Fatalf("second Embed() error = %v", err)
	}
	if len(first) != embedding.DefaultDimensions || len(second) != len(first) {
		t.Fatalf("embedding dimensions = %d and %d", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("embedding differs at index %d", index)
		}
	}
	if err := embedding.ValidateVector(first, embedding.DefaultDimensions); err != nil {
		t.Fatalf("ValidateVector() error = %v", err)
	}
}

func TestClientRejectsUnsafeAndMalformedBoundaries(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{
		"https://api.openai.com",
		"http://example.com",
		"http://user:secret@localhost:8091",
		"http://localhost:8091?unsafe=true",
	} {
		if _, err := embedding.NewClient(embedding.ClientConfig{
			BaseURL: baseURL, ModelID: embedding.DefaultModelID,
			Dimensions: embedding.DefaultDimensions, Timeout: time.Second,
		}); err == nil {
			t.Errorf("NewClient() accepted unsafe URL %q", baseURL)
		}
	}
	if _, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "http://localhost:8091", Dimensions: embedding.DefaultDimensions, Timeout: time.Second,
	}); err == nil {
		t.Error("NewClient() accepted an empty model")
	}
	if _, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "http://localhost:8091", ModelID: embedding.DefaultModelID, Timeout: time.Second,
	}); err == nil {
		t.Error("NewClient() accepted zero dimensions")
	}
	if _, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "http://localhost:8091", ModelID: embedding.DefaultModelID,
		Dimensions: embedding.DefaultDimensions,
	}); err == nil {
		t.Error("NewClient() accepted a zero timeout")
	}
	if _, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "https://api.openai.com", ModelID: embedding.DefaultModelID,
		Dimensions: embedding.DefaultDimensions, Timeout: time.Second, Hosted: true,
	}); err == nil {
		t.Error("NewClient() accepted a hosted request without an API key")
	}
	if _, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "https://example.com", APIKey: "fixture-key", ModelID: embedding.DefaultModelID,
		Dimensions: embedding.DefaultDimensions, Timeout: time.Second, Hosted: true,
	}); err == nil {
		t.Error("NewClient() accepted an untrusted hosted provider")
	}

	tests := []struct {
		name    string
		status  int
		payload string
	}{
		{name: "provider failure", status: http.StatusServiceUnavailable, payload: `{"error":"unavailable"}`},
		{name: "malformed response", status: http.StatusOK, payload: `{"model":"text-embedding-3-small","data":[]}`},
		{name: "invalid JSON", status: http.StatusOK, payload: "{"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.payload))
			}))
			t.Cleanup(server.Close)
			client, err := embedding.NewClient(embedding.ClientConfig{
				BaseURL: server.URL, ModelID: embedding.DefaultModelID,
				Dimensions: embedding.DefaultDimensions, Timeout: time.Second,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, err := client.Embed(context.Background(), "bounded input"); err == nil {
				t.Fatal("Embed() accepted a malformed provider response")
			}
		})
	}
}

func TestHostedClientAuthenticatesOnlyThePinnedOpenAIEndpoint(t *testing.T) {
	t.Parallel()
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.openai.com/v1/embeddings" {
			t.Fatalf("request URL = %q", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer fixture-key" ||
			request.Header.Get("OpenAI-Project") != "project-fixture" ||
			request.Header.Get("OpenAI-Organization") != "org-fixture" {
			t.Fatalf("request headers = %#v", request.Header)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"model":"text-embedding-3-small","data":[{"embedding":[1,0,0],"index":0}]}`,
			)),
			Request: request,
		}, nil
	})
	client, err := embedding.NewClient(embedding.ClientConfig{
		BaseURL: "https://api.openai.com/v1", APIKey: "fixture-key",
		ProjectID: "project-fixture", Organization: "org-fixture",
		ModelID: embedding.DefaultModelID, Dimensions: 3, Timeout: time.Second,
		Hosted: true, HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.Embed(context.Background(), "bounded hosted input")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(result) != 3 || result[0] != 1 {
		t.Fatalf("Embed() = %v", result)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEmbeddingValidationRejectsInvalidInputAndVectors(t *testing.T) {
	t.Parallel()
	if err := embedding.ValidateInput("valid input"); err != nil {
		t.Fatalf("ValidateInput() error = %v", err)
	}
	for _, input := range []string{"", "  ", strings.Repeat("x", embedding.MaximumInputBytes+1)} {
		if err := embedding.ValidateInput(input); err == nil {
			t.Errorf("ValidateInput() accepted %d bytes", len(input))
		}
	}
	valid := make(embedding.Vector, 3)
	valid[0] = 1
	if err := embedding.ValidateVector(valid, 3); err != nil {
		t.Fatalf("ValidateVector(valid) error = %v", err)
	}
	for _, vector := range []embedding.Vector{
		nil,
		make(embedding.Vector, 3),
		{float32(math.NaN()), 0, 0},
		{float32(math.Inf(1)), 0, 0},
	} {
		if err := embedding.ValidateVector(vector, 3); err == nil {
			t.Errorf("ValidateVector() accepted %v", vector)
		}
	}
	if embedding.ContentDigest("stable") != embedding.ContentDigest("stable") {
		t.Fatal("ContentDigest() is not deterministic")
	}
}
