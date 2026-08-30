package discovery

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOPMLDetectsApprovedConnectorsAndDeduplicates(t *testing.T) {
	t.Parallel()
	payload := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><body>
  <outline text="Go" type="atom" xmlUrl="https://go.dev/blog/feed.atom" />
  <outline text="Go duplicate" type="atom" xmlUrl="https://go.dev/blog/feed.atom" />
  <outline text="Nested"><outline title="News" type="rss" xmlUrl="https://example.com/feed.xml" /></outline>
</body></opml>`)
	candidates, _, err := ParseOPML(payload)
	if err != nil {
		t.Fatalf("parseOPML() error = %v", err)
	}
	if len(candidates) != 2 || !candidates[0].Valid || !candidates[1].Valid {
		t.Fatalf("parseOPML() candidates = %+v", candidates)
	}
}

func TestParseOPMLRejectsUnsafeAndUnsupportedSources(t *testing.T) {
	t.Parallel()
	payload := []byte(`<opml version="2.0"><body>
<outline text="Private" type="rss" xmlUrl="http://127.0.0.1/feed" />
<outline text="Script" type="browser" xmlUrl="https://example.com/app" />
</body></opml>`)
	candidates, _, err := ParseOPML(payload)
	if err != nil {
		t.Fatalf("parseOPML() error = %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Valid || candidate.Explanation == "" {
			t.Fatalf("unsafe candidate accepted = %+v", candidate)
		}
	}
	if _, _, err := ParseOPML([]byte(strings.Repeat("x", MaximumOPMLBytes+1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized parseOPML() error = %v", err)
	}
}

func TestCanonicalCaptureURLAllowsOnlyHTTPSOrFakeFixture(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://go.dev/blog/go1.27", "http://fake-source:8090/page"} {
		if _, err := CanonicalCaptureURL(raw); err != nil {
			t.Errorf("CanonicalCaptureURL(%q) error = %v", raw, err)
		}
	}
	for _, raw := range []string{"http://example.com", "https://user@example.com", "https://example.com:8443"} {
		if _, err := CanonicalCaptureURL(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("CanonicalCaptureURL(%q) error = %v", raw, err)
		}
	}
}
