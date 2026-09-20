package fetcher

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestGitHubReadTokenIsSentOnlyToGitHubRESTOrigin(t *testing.T) {
	const token = "fixture-github-read-token"
	tests := []struct {
		name     string
		url      string
		host     string
		fixtures []FixtureTarget
		wantAuth bool
	}{
		{name: "GitHub REST", url: "https://api.github.com/advisories", host: "api.github.com", wantAuth: true},
		{name: "explicit default TLS port", url: "https://api.github.com:443/repos/owner/repo", host: "api.github.com", wantAuth: true},
		{name: "GitHub website", url: "https://github.com/advisories", host: "github.com"},
		{name: "lookalike host", url: "https://api.github.com.evil.example/advisories", host: "api.github.com.evil.example"},
		{name: "local fake fixture", url: "http://fake-source:8090/feed", host: "fake-source", fixtures: []FixtureTarget{{Host: "fake-source", Port: 8090}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := netip.MustParseAddr("93.184.216.34")
			if len(test.fixtures) > 0 {
				address = netip.MustParseAddr("127.0.0.1")
			}
			policy, err := NewPolicy(staticResolver{test.host: {address}}, []string{test.host}, test.fixtures)
			if err != nil {
				t.Fatal(err)
			}
			doer := &responseDoer{responses: []*http.Response{{StatusCode: http.StatusNotModified, Header: make(http.Header), Body: http.NoBody}}}
			configuration := DefaultConfig("Relantern/1.0 (+https://github.com/traweezy/relantern)")
			configuration.GitHubReadToken = token
			configuredFetcher, err := New(configuration, policy, doer, nil, newMemoryStore())
			if err != nil {
				t.Fatal(err)
			}
			endpoint := fixtureEndpoint(1024, ContentPolicyLinkAndExcerpt)
			endpoint.URL = test.url
			endpoint.AllowedHosts = []string{test.host}
			if _, err := configuredFetcher.Fetch(context.Background(), endpoint, Checkpoint{ETag: `"v1"`}); err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if len(doer.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(doer.requests))
			}
			got := doer.requests[0].Header.Get("Authorization")
			if test.wantAuth && got != "Bearer "+token {
				t.Fatal("GitHub REST request did not carry the configured bearer token")
			}
			if !test.wantAuth && got != "" {
				t.Fatal("non-GitHub request carried an authorization header")
			}
		})
	}
}

func TestGitHubReadTokenValidationDoesNotExposeSecret(t *testing.T) {
	policy, err := NewPolicy(staticResolver{"api.github.com": {netip.MustParseAddr("93.184.216.34")}}, []string{"api.github.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	configuration := DefaultConfig("Relantern/1.0 (+https://github.com/traweezy/relantern)")
	configuration.GitHubReadToken = "unsafe\r\ncredential"
	_, err = New(configuration, policy, &responseDoer{}, nil, newMemoryStore())
	if err == nil || strings.Contains(err.Error(), configuration.GitHubReadToken) {
		t.Fatal("New() accepted or exposed a malformed read token")
	}
}
