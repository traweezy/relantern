package parsing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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

func TestGitHubAdvisoryEntriesPreserveBoundedStructuredEvidence(t *testing.T) {
	baseURL := "https://fixtures.example.test/advisories"
	for _, test := range []struct {
		name          string
		metadata      map[string]any
		wantPatch     string
		wantWithdrawn string
	}{
		{
			name: "published repository advisory",
			metadata: map[string]any{
				"state": "published", "severity": "critical", "withdrawn_at": nil,
				"vulnerabilities": []any{map[string]any{
					"package":                  map[string]any{"ecosystem": "go", "name": "example.com/module"},
					"vulnerable_version_range": "< 1.2.3", "patched_versions": "1.2.3",
				}},
			},
			wantPatch: "1.2.3",
		},
		{
			name: "reviewed global advisory",
			metadata: map[string]any{
				"type": "reviewed", "severity": "critical",
				"github_reviewed_at": "2026-09-20T10:00:00Z", "withdrawn_at": nil,
				"vulnerabilities": []any{map[string]any{
					"package":                  map[string]any{"ecosystem": "npm", "name": "@example/library"},
					"vulnerable_version_range": "<= 2.0.0", "first_patched_version": "2.0.1",
				}},
			},
			wantPatch: "2.0.1",
		},
		{
			name: "withdrawn global advisory",
			metadata: map[string]any{
				"type": "reviewed", "severity": "critical",
				"github_reviewed_at": "2026-09-20T10:00:00Z",
				"withdrawn_at":       "2026-09-20T11:00:00Z",
			},
			wantPatch: "1.0.0", wantWithdrawn: "2026-09-20T11:00:00Z",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			advisory := testAdvisory()
			for key, value := range test.metadata {
				advisory[key] = value
			}
			body, err := json.Marshal([]any{advisory})
			if err != nil {
				t.Fatal(err)
			}
			entries, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, baseURL, body)
			if err != nil || len(entries) != 1 {
				t.Fatalf("SplitEntries() = %+v, %v", entries, err)
			}
			if err := ValidateEntry(baseURL, entries[0]); err != nil {
				t.Fatalf("ValidateEntry() = %v", err)
			}
			var payload sourceEntryPayload
			if err := json.Unmarshal(entries[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			var encoded struct {
				Advisory map[string]any `json:"advisory"`
			}
			if err := json.Unmarshal(entries[0].Payload, &encoded); err != nil {
				t.Fatal(err)
			}
			if encoded.Advisory["id"] != "GHSA-abcd-1234-efgh" || encoded.Advisory["ghsa_id"] != nil {
				t.Fatalf("advisory child JSON contract = %+v", encoded.Advisory)
			}
			encodedVulnerabilities, ok := encoded.Advisory["vulnerabilities"].([]any)
			if !ok || len(encodedVulnerabilities) != 1 {
				t.Fatalf("advisory child vulnerabilities = %+v", encoded.Advisory["vulnerabilities"])
			}
			encodedVulnerability, ok := encodedVulnerabilities[0].(map[string]any)
			if !ok || encodedVulnerability["ecosystem"] == nil ||
				encodedVulnerability["packageName"] == nil ||
				encodedVulnerability["versionRange"] == nil ||
				encodedVulnerability["patchedVersion"] != test.wantPatch ||
				encodedVulnerability["package"] != nil {
				t.Fatalf("advisory child vulnerability JSON contract = %+v", encodedVulnerability)
			}
			if payload.Advisory == nil || payload.Advisory.ID != "GHSA-abcd-1234-efgh" ||
				payload.Advisory.Severity != "critical" || payload.Advisory.PublishedAt != "2026-09-20T09:00:00Z" ||
				payload.Advisory.WithdrawnAt != test.wantWithdrawn || len(payload.Advisory.Vulnerabilities) != 1 {
				t.Fatalf("structured advisory evidence = %+v", payload.Advisory)
			}
			vulnerability := payload.Advisory.Vulnerabilities[0]
			if vulnerability.PatchedVersion != test.wantPatch {
				t.Fatalf("patch metadata = %+v, want %q", vulnerability, test.wantPatch)
			}
			if _, ok := test.metadata["type"]; ok {
				if payload.Advisory.Type != "reviewed" || payload.Advisory.ReviewedAt != "2026-09-20T10:00:00Z" {
					t.Fatalf("global review metadata = %+v", payload.Advisory)
				}
			} else if payload.Advisory.State != "published" {
				t.Fatalf("repository publication metadata = %+v", payload.Advisory)
			}
			again, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, baseURL, body)
			if err != nil || !bytes.Equal(entries[0].Payload, again[0].Payload) {
				t.Fatalf("advisory child payload is not deterministic: %v", err)
			}
			if bytes.Contains(entries[0].Payload, []byte(`"urgent"`)) {
				t.Fatal("parser inferred urgency from advisory metadata")
			}
		})
	}
}

func TestGlobalAdvisoryChildIdentityIsStableAcrossCursorPages(t *testing.T) {
	root := sources.GlobalReviewedAdvisoriesURL
	pageTwo := root + "&after=cursor-2"
	advisory := testAdvisory()
	advisory["html_url"] = "https://github.com/advisories/GHSA-abcd-1234-efgh"
	advisory["type"] = "reviewed"
	advisory["github_reviewed_at"] = "2026-09-20T09:01:00Z"
	body, err := json.Marshal([]any{advisory})
	if err != nil {
		t.Fatal(err)
	}
	first, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, root, body)
	if err != nil || len(first) != 1 {
		t.Fatalf("root advisory entries = %+v, %v", first, err)
	}
	second, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, pageTwo, body)
	if err != nil || len(second) != 1 {
		t.Fatalf("continuation advisory entries = %+v, %v", second, err)
	}
	if first[0].ExternalID != second[0].ExternalID || first[0].URL != second[0].URL ||
		!bytes.Equal(first[0].Payload, second[0].Payload) {
		t.Fatalf("advisory moved pages but changed child identity: root=%+v page=%+v", first[0], second[0])
	}
	if err := ValidateEntry(pageTwo, second[0]); err != nil {
		t.Fatalf("validate continuation child: %v", err)
	}
}

func TestGitHubAdvisoryEntriesRejectMalformedOrOversizeMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing ID with severity", func(item map[string]any) { delete(item, "ghsa_id") }},
		{"invalid ID", func(item map[string]any) { item["ghsa_id"] = "GHSA-invalid" }},
		{"invalid severity", func(item map[string]any) { item["severity"] = "urgent" }},
		{"invalid state", func(item map[string]any) { item["state"] = "sent" }},
		{"invalid type", func(item map[string]any) { item["type"] = "official" }},
		{"invalid reviewed timestamp", func(item map[string]any) { item["github_reviewed_at"] = "tomorrow" }},
		{"invalid withdrawal timestamp", func(item map[string]any) { item["withdrawn_at"] = "not-a-date" }},
		{"non-array vulnerabilities", func(item map[string]any) { item["vulnerabilities"] = "all" }},
		{"too many vulnerabilities", func(item map[string]any) {
			items := make([]any, maximumAdvisoryVulnerabilities+1)
			for index := range items {
				items[index] = testVulnerability()
			}
			item["vulnerabilities"] = items
		}},
		{"invalid package", func(item map[string]any) { item["vulnerabilities"] = []any{map[string]any{"package": "npm"}} }},
		{"invalid ecosystem", func(item map[string]any) {
			item["vulnerabilities"] = []any{map[string]any{"package": map[string]any{"ecosystem": "unknown", "name": "module"}}}
		}},
		{"oversize package", func(item map[string]any) {
			item["vulnerabilities"] = []any{map[string]any{"package": map[string]any{"ecosystem": "npm", "name": strings.Repeat("x", 256)}}}
		}},
		{"oversize range", func(item map[string]any) {
			item["vulnerabilities"] = []any{map[string]any{"package": map[string]any{"ecosystem": "npm", "name": "module"}, "vulnerable_version_range": strings.Repeat("x", 513)}}
		}},
		{"malformed patch version", func(item map[string]any) {
			item["vulnerabilities"] = []any{map[string]any{"package": map[string]any{"ecosystem": "npm", "name": "module"}, "first_patched_version": 123}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := testAdvisory()
			test.mutate(item)
			body, err := json.Marshal([]any{item})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, "https://fixtures.example.test/advisories", body); err == nil {
				t.Fatal("accepted malformed or oversize advisory metadata")
			}
		})
	}
}

func TestGitHubAdvisoryEntryValidationRejectsTamperedEvidence(t *testing.T) {
	baseURL := "https://fixtures.example.test/advisories"
	body, err := json.Marshal([]any{testAdvisory()})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := SplitEntries(context.Background(), sources.ConnectorGitHubAdvisories, baseURL, body)
	if err != nil || len(entries) != 1 {
		t.Fatalf("SplitEntries() = %+v, %v", entries, err)
	}
	var payload sourceEntryPayload
	if err := json.Unmarshal(entries[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Advisory.Severity = "urgent"
	entries[0].Payload, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEntry(baseURL, entries[0]); err == nil {
		t.Fatal("accepted tampered advisory severity")
	}
	if _, err := New().Parse(context.Background(), Request{
		Connector: sources.ConnectorSourceEntry, URL: entries[0].URL,
		ContentType: "application/json", Body: bytes.NewReader(entries[0].Payload),
	}); err == nil {
		t.Fatal("parsed tampered advisory severity")
	}
}

func testAdvisory() map[string]any {
	return map[string]any{
		"ghsa_id": "GHSA-abcd-1234-efgh", "summary": "Official advisory",
		"html_url": "https://fixtures.example.test/advisories/GHSA-abcd-1234-efgh",
		"severity": "critical", "published_at": "2026-09-20T09:00:00Z",
		"vulnerabilities": []any{testVulnerability()},
	}
}

func testVulnerability() map[string]any {
	return map[string]any{
		"package":                  map[string]any{"ecosystem": "npm", "name": "example"},
		"vulnerable_version_range": "< 1.0.0", "first_patched_version": "1.0.0",
	}
}
