package parsing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/traweezy/relantern/internal/sources"
)

func TestReviewedCollectionsProduceDurableEntries(t *testing.T) {
	for _, suite := range loadFixtureCatalog(t).Suites {
		if !IsCollectionConnector(suite.Connector) {
			continue
		}
		t.Run(suite.ID, func(t *testing.T) {
			baseURL := "https://fixtures.example.test/" + suite.ID
			emptyBody, err := fixtureBody(suite.Payloads["empty"])
			if err != nil {
				t.Fatal(err)
			}
			emptyEntries, err := SplitEntries(context.Background(), suite.Connector, baseURL, emptyBody)
			if err != nil || emptyEntries == nil || len(emptyEntries) != 0 {
				t.Fatalf("valid empty collection = %+v, %v", emptyEntries, err)
			}
			load := func(name string) []Entry {
				t.Helper()
				body, err := fixtureBody(suite.Payloads[name])
				if err != nil {
					t.Fatal(err)
				}
				entries, err := SplitEntries(context.Background(), suite.Connector, baseURL, body)
				if err != nil {
					t.Fatalf("SplitEntries(%s): %v", name, err)
				}
				if len(entries) != 1 {
					t.Fatalf("SplitEntries(%s) returned %d entries, want 1", name, len(entries))
				}
				if err := ValidateEntry(baseURL, entries[0]); err != nil {
					t.Fatalf("ValidateEntry(%s): %v", name, err)
				}
				return entries
			}
			normal := load("normal")[0]
			changed := load("changed_revision")[0]
			if normal.ExternalID != changed.ExternalID || sha256.Sum256(normal.Payload) == sha256.Sum256(changed.Payload) {
				t.Fatalf("changed revision lost stable identity or content change: %q vs %q", normal.ExternalID, changed.ExternalID)
			}
			if normal.URL == baseURL || !strings.HasPrefix(normal.URL, "https://") {
				t.Fatalf("entry URL = %q", normal.URL)
			}
			if got := len(load("duplicate_story")); got != 1 {
				t.Fatalf("duplicate fixture produced %d entries", got)
			}
			prompt := load("prompt_injection")[0]
			parsed, err := New().Parse(context.Background(), Request{
				Connector: sources.ConnectorSourceEntry, URL: prompt.URL,
				ContentType: "application/json", Body: bytes.NewReader(prompt.Payload),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !hasWarning(parsed, "embedded_instruction_removed") || strings.Contains(strings.ToLower(parsed.NormalizedText), "ignore previous instructions") {
				t.Fatalf("source entry retained embedded instruction: %+v", parsed)
			}
		})
	}
}

func TestEmptyCollectionsStillRequireAValidEnvelope(t *testing.T) {
	for _, test := range []struct {
		connector sources.Connector
		payload   string
	}{
		{sources.ConnectorJSONFeed, `{}`},
		{sources.ConnectorJSONFeed, `{"items":null}`},
		{sources.ConnectorGitHubReleases, `null`},
		{sources.ConnectorStructuredAPI, `{}`},
		{sources.ConnectorStructuredAPI, `{"services":[]}`},
	} {
		if _, err := SplitEntries(context.Background(), test.connector, "https://example.test/feed", []byte(test.payload)); err == nil {
			t.Errorf("accepted incomplete %s collection %s", test.connector, test.payload)
		}
	}
}

func TestEntryExtractionRejectsUnsafeLinksAndConflictingIDs(t *testing.T) {
	base := "https://fixtures.example.test/feed"
	for _, link := range []string{"http://other.example.test/x", "https://localhost/x", "https://127.0.0.1/x", "https://127.0.0.1./x", "https://service.internal/x", "https://user@example.test/x", "https://example.test:8443/x", "file:///tmp/x"} {
		body := []byte(`{"items":[{"id":"one","url":"` + link + `","title":"First"}]}`)
		if _, err := SplitEntries(context.Background(), sources.ConnectorJSONFeed, base, body); err == nil {
			t.Errorf("accepted unsafe entry link %q", link)
		}
	}
	conflict := []byte(`{"items":[{"id":"one","title":"First"},{"id":"one","title":"Changed"}]}`)
	if _, err := SplitEntries(context.Background(), sources.ConnectorJSONFeed, base, conflict); err == nil {
		t.Fatal("accepted conflicting duplicate external IDs")
	}
	crossOrigin := []byte(`{"items":[{"id":"official-one","url":"https://third-party.example.test/story","title":"Referenced story"}]}`)
	entries, err := SplitEntries(context.Background(), sources.ConnectorJSONFeed, base, crossOrigin)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cross-origin reference = %+v, %v", entries, err)
	}
	if !strings.HasPrefix(entries[0].URL, base+"?relantern_entry=") || !bytes.Contains(entries[0].Payload, []byte(`"externalUrl":"https://third-party.example.test/story"`)) {
		t.Fatalf("cross-origin link escaped reviewed provenance: %+v", entries[0])
	}
	if err := ValidateEntry(base, entries[0]); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubAPIEntryLinksStayWithinTheReviewedRepository(t *testing.T) {
	for _, test := range []struct {
		name      string
		connector sources.Connector
		endpoint  string
		itemURL   string
	}{
		{
			name: "release", connector: sources.ConnectorGitHubReleases,
			endpoint: "https://api.github.com/repos/golang/go/releases",
			itemURL:  "https://github.com/golang/go/releases/tag/go1.27.0",
		},
		{
			name: "advisory", connector: sources.ConnectorGitHubAdvisories,
			endpoint: "https://api.github.com/repos/golang/go/security-advisories",
			itemURL:  "https://github.com/golang/go/security/advisories/GHSA-xxxx-yyyy-zzzz",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`[{"node_id":"RE_reviewed_1","html_url":"` + test.itemURL + `","name":"Reviewed item"}]`)
			entries, err := SplitEntries(context.Background(), test.connector, test.endpoint, body)
			if err != nil || len(entries) != 1 {
				t.Fatalf("SplitEntries() = %+v, %v", entries, err)
			}
			if entries[0].URL != test.itemURL || bytes.Contains(entries[0].Payload, []byte(`"externalUrl"`)) {
				t.Fatalf("reviewed item link was replaced: %+v", entries[0])
			}
			if err := ValidateEntry(test.endpoint, entries[0]); err != nil {
				t.Fatalf("ValidateEntry() = %v", err)
			}
		})
	}

	endpoint := "https://api.github.com/repos/golang/go/releases"
	for _, itemURL := range []string{
		"https://github.com/another/repo/releases/tag/v1.0.0",
		"https://github.com/golang/go/issues/123",
		"https://github.com.evil.test/golang/go/releases/tag/v1.0.0",
		"https://github.com:8443/golang/go/releases/tag/v1.0.0",
		"https://third-party.example.test/golang/go/releases/tag/v1.0.0",
	} {
		t.Run(itemURL, func(t *testing.T) {
			body := []byte(`[{"node_id":"RE_unreviewed_1","html_url":"` + itemURL + `","name":"Unreviewed item"}]`)
			entries, err := SplitEntries(context.Background(), sources.ConnectorGitHubReleases, endpoint, body)
			if itemURL == "https://github.com:8443/golang/go/releases/tag/v1.0.0" {
				if err == nil {
					t.Fatal("accepted a nonstandard GitHub port")
				}
				return
			}
			if err != nil || len(entries) != 1 {
				t.Fatalf("SplitEntries() = %+v, %v", entries, err)
			}
			if entries[0].URL == itemURL || !strings.HasPrefix(entries[0].URL, endpoint+"?relantern_entry=") {
				t.Fatalf("unreviewed item link became authoritative: %+v", entries[0])
			}
			if err := ValidateEntry(endpoint, entries[0]); err != nil {
				t.Fatalf("ValidateEntry() = %v", err)
			}
		})
	}
}
