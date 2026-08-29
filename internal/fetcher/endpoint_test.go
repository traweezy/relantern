package fetcher

import (
	"testing"

	"github.com/traweezy/relantern/internal/sources"
)

func TestEndpointFromSourceKeepsReviewedBounds(t *testing.T) {
	converted, err := EndpointFromSource(sources.Endpoint{
		ID:                   "go-blog",
		SourceID:             "go-blog",
		URL:                  "https://go.dev/blog/feed.atom",
		ExpectedContentTypes: []string{"application/atom+xml"},
		MaxResponseBytes:     5 << 20,
		ContentLicense:       ContentPolicyLinkAndExcerpt,
	})
	if err != nil {
		t.Fatalf("EndpointFromSource() error = %v", err)
	}
	if len(converted.AllowedHosts) != 1 || converted.AllowedHosts[0] != "go.dev" || converted.MaxBodyBytes != 5<<20 {
		t.Fatalf("EndpointFromSource() = %+v", converted)
	}
}

func TestEndpointFromSourceRejectsInvalidURL(t *testing.T) {
	if _, err := EndpointFromSource(sources.Endpoint{ID: "invalid", URL: "://"}); err == nil {
		t.Fatal("EndpointFromSource() accepted an invalid URL")
	}
}
