package sources_test

import (
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/sources"
)

func TestValidationRejectsRegistryContractViolations(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		mutate      func(*sources.Registry)
		expectation string
	}{
		{
			name: "unsupported registry version",
			mutate: func(registry *sources.Registry) {
				registry.Version = 2
			},
			expectation: "registry version must be 1",
		},
		{
			name: "duplicate source id",
			mutate: func(registry *sources.Registry) {
				registry.Sources[1].ID = registry.Sources[0].ID
			},
			expectation: "duplicate id",
		},
		{
			name: "unsupported connector",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].Connector = sources.Connector("crawler")
			},
			expectation: "unsupported connector",
		},
		{
			name: "repository connector on ordinary source",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].Connector = sources.ConnectorGitHubReleases
			},
			expectation: "GitHub connectors require a repository watch",
		},
		{
			name: "invalid content type",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].ExpectedContentTypes = []string{"not a media type"}
			},
			expectation: "invalid content type",
		},
		{
			name: "body limit exceeds connector cap",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].MaxResponseBytes = 5<<20 + 1
			},
			expectation: "exceeds connector limit",
		},
		{
			name: "unknown fixture suite",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].FixtureSuite = "missing-v1"
			},
			expectation: "does not exist",
		},
		{
			name: "invalid topic",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].Topics = []string{"Go Language"}
			},
			expectation: "invalid topic",
		},
		{
			name: "poll interval too short",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].PollInterval.Duration = time.Minute
			},
			expectation: "poll interval must be",
		},
		{
			name: "unreviewed entry",
			mutate: func(registry *sources.Registry) {
				registry.Sources[0].ReviewedAt.Time = time.Time{}
			},
			expectation: "reviewed_at is required",
		},
		{
			name: "missing repository node id",
			mutate: func(registry *sources.Registry) {
				registry.Repositories[0].NodeID = ""
			},
			expectation: "node_id is required",
		},
		{
			name: "repository URL mismatch",
			mutate: func(registry *sources.Registry) {
				registry.Repositories[0].URL = "https://api.github.com/repos/example/wrong"
			},
			expectation: "URL must be",
		},
		{
			name: "duplicate repository event",
			mutate: func(registry *sources.Registry) {
				registry.Repositories[0].EnabledEvents = append(registry.Repositories[0].EnabledEvents, sources.RepositoryEventReleases)
			},
			expectation: "duplicate event",
		},
		{
			name: "critical repository without security path",
			mutate: func(registry *sources.Registry) {
				registry.Repositories[0].EnabledEvents = []sources.RepositoryEvent{sources.RepositoryEventReleases}
			},
			expectation: "requires security advisories",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, catalog := loadReviewedFiles(t)
			test.mutate(&registry)
			err := sources.Validate(registry, catalog, now)
			assertErrorContains(t, err, test.expectation)
		})
	}
}

func TestValidationRejectsFixtureContractViolations(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		mutate      func(*sources.FixtureCatalog)
		expectation string
	}{
		{
			name: "unsupported fixture version",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Version = 2
			},
			expectation: "fixture version must be 1",
		},
		{
			name: "missing required case",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Profiles[0].Cases = catalog.Profiles[0].Cases[1:]
			},
			expectation: "lacks case \"normal\"",
		},
		{
			name: "wrong scenario status",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Profiles[0].Cases[0].Status = 201
			},
			expectation: "must use status 200",
		},
		{
			name: "redirect without location",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Profiles[0].Cases[3].Headers = nil
			},
			expectation: "redirect fixture requires Location",
		},
		{
			name: "invalid body generator",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Profiles[0].Cases[7].Generator.Repeat = 0
			},
			expectation: "invalid body generator",
		},
		{
			name: "unsupported fixture connector",
			mutate: func(catalog *sources.FixtureCatalog) {
				catalog.Suites[0].Connector = sources.Connector("crawler")
			},
			expectation: "unsupported connector",
		},
		{
			name: "invalid base64 payload",
			mutate: func(catalog *sources.FixtureCatalog) {
				invalid := "***"
				catalog.Suites[0].Payloads["character_encoding"] = sources.FixturePayload{BodyBase64: &invalid}
			},
			expectation: "is not valid base64",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, catalog := loadReviewedFiles(t)
			test.mutate(&catalog)
			err := sources.Validate(registry, catalog, now)
			assertErrorContains(t, err, test.expectation)
		})
	}
}
