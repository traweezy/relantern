package sources_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/sources"
)

const (
	registryPath = "../../sources/registry.yaml"
	fixturesPath = "../../sources/fixtures.yaml"
)

func TestReviewedRegistryAndFixturesValidate(t *testing.T) {
	registry, catalog := loadReviewedFiles(t)
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)

	if err := sources.Validate(registry, catalog, now); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := len(registry.Endpoints()), 91; got != want {
		t.Fatalf("len(Endpoints()) = %d, want %d", got, want)
	}
	if got, want := len(catalog.Suites), 8; got != want {
		t.Fatalf("len(Suites) = %d, want %d", got, want)
	}
}

func TestRepositoryWatchExpandsStableEndpoints(t *testing.T) {
	registry, _ := loadReviewedFiles(t)
	var releaseURL string
	var advisoryURL string
	for _, endpoint := range registry.Endpoints() {
		if endpoint.SourceID != "github-react-react" {
			continue
		}
		switch endpoint.RepositoryEvent {
		case sources.RepositoryEventReleases:
			releaseURL = endpoint.URL
		case sources.RepositoryEventSecurityAdvisories:
			advisoryURL = endpoint.URL
		}
		if endpoint.RepositoryNodeID != "MDEwOlJlcG9zaXRvcnkxMDI3MDI1MA==" {
			t.Fatalf("RepositoryNodeID = %q", endpoint.RepositoryNodeID)
		}
	}
	if releaseURL != "https://api.github.com/repos/react/react/releases" {
		t.Fatalf("release URL = %q", releaseURL)
	}
	if advisoryURL != "https://api.github.com/repos/react/react/security-advisories" {
		t.Fatalf("advisory URL = %q", advisoryURL)
	}
}

func TestValidationRejectsEnabledNetworkFuse(t *testing.T) {
	registry, catalog := loadReviewedFiles(t)
	registry.Enabled = true

	err := sources.Validate(registry, catalog, time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC))
	assertErrorContains(t, err, "live source fetching requires a separately reviewed owner rollout")
}

func TestValidationRejectsStaleReview(t *testing.T) {
	registry, catalog := loadReviewedFiles(t)

	err := sources.Validate(registry, catalog, time.Date(2026, time.December, 1, 12, 0, 0, 0, time.UTC))
	assertErrorContains(t, err, "review is older than 90 days")
}

func TestValidationRejectsUnsafeURL(t *testing.T) {
	registry, catalog := loadReviewedFiles(t)
	registry.Sources[0].URL = "https://user:secret@go.dev:8443/blog/feed.atom#fragment"

	err := sources.Validate(registry, catalog, time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC))
	assertErrorContains(t, err, "URL must not contain user information")
}

func TestValidationRejectsMissingFixturePayload(t *testing.T) {
	registry, catalog := loadReviewedFiles(t)
	delete(catalog.Suites[0].Payloads, "prompt_injection")

	err := sources.Validate(registry, catalog, time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC))
	assertErrorContains(t, err, "lacks payload \"prompt_injection\"")
}

func TestFixtureCatalogContainsUntrustedPromptContent(t *testing.T) {
	_, catalog := loadReviewedFiles(t)
	for _, suite := range catalog.Suites {
		payload, exists := suite.Payloads["prompt_injection"]
		if !exists || payload.Body == nil {
			t.Fatalf("suite %q lacks inline prompt-injection content", suite.ID)
		}
		if !strings.Contains(*payload.Body, "untrusted source content") {
			t.Fatalf("suite %q prompt fixture is not visibly untrusted", suite.ID)
		}
	}
}

func TestLoadRegistryRejectsUnknownFields(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "registry.yaml")
	contents := "version: 1\nenabled: false\ncontact: https://example.test\nunknown: true\nsources: []\nrepositories: []\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := sources.LoadRegistry(path)
	assertErrorContains(t, err, "field unknown not found")
}

func TestLoadFixtureCatalogRejectsMultipleDocuments(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixtures.yaml")
	contents := "version: 1\nprofiles: []\nsuites: []\n---\nversion: 1\nprofiles: []\nsuites: []\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := sources.LoadFixtureCatalog(path)
	assertErrorContains(t, err, "multiple YAML documents")
}

func loadReviewedFiles(t *testing.T) (sources.Registry, sources.FixtureCatalog) {
	t.Helper()
	registry, err := sources.LoadRegistry(registryPath)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	catalog, err := sources.LoadFixtureCatalog(fixturesPath)
	if err != nil {
		t.Fatalf("LoadFixtureCatalog() error = %v", err)
	}
	return registry, catalog
}

func assertErrorContains(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", expected)
	}
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("error = %q, want substring %q", err, expected)
	}
}
