package ingestion

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fixtureRoundTripper func(*http.Request) (*http.Response, error)

func (trip fixtureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return trip(request)
}

func TestRobotsRequestIsBoundedAndFailClosed(t *testing.T) {
	target, _ := url.Parse("https://example.com/blocked/feed.xml")
	tests := []struct {
		name   string
		status int
		body   string
		denied bool
	}{
		{name: "missing policy", status: 404},
		{name: "disallowed path", status: 200, body: "User-agent: *\nDisallow: /blocked", denied: true},
		{name: "publisher unavailable", status: 503, denied: true},
		{name: "oversized policy", status: 200, body: strings.Repeat("x", int(robotsLimit)+1), denied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/robots.txt" || request.Header.Get("User-Agent") != userAgent {
					t.Fatalf("unexpected robots request: %s", request.URL)
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}
			err := checkRobots(t.Context(), client, target)
			if (err != nil) != test.denied {
				t.Fatalf("checkRobots() = %v, denied=%t", err, test.denied)
			}
		})
	}
}

func TestRobotsUsesMatchingGroupAndLongestRule(t *testing.T) {
	document := `User-agent: unrelated
Disallow: /

User-agent: *
Disallow: /private
Allow: /private/public

User-agent: Relantern
Disallow: /preview/*
Allow: /preview/approved$
`
	for _, test := range []struct {
		path   string
		denied bool
	}{
		{path: "/private/file", denied: false},
		{path: "/preview/other", denied: true},
		{path: "/preview/approved", denied: false},
		{path: "/preview/approved/child", denied: true},
	} {
		if got := robotsDisallows(document, test.path); got != test.denied {
			t.Errorf("robotsDisallows(%q) = %t, want %t", test.path, got, test.denied)
		}
	}
}
