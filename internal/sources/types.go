package sources

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	RegistryVersion = 1
	FixtureVersion  = 1
)

type Connector string

const (
	ConnectorAtom             Connector = "atom"
	ConnectorRSS              Connector = "rss"
	ConnectorJSONFeed         Connector = "json_feed"
	ConnectorPage             Connector = "page"
	ConnectorGitHubReleases   Connector = "github_releases"
	ConnectorGitHubAdvisories Connector = "github_advisories"
	ConnectorRegistry         Connector = "registry"
	ConnectorStructuredAPI    Connector = "structured_api"
)

type TrustTier string

const (
	TrustTierZero TrustTier = "T0"
	TrustTierOne  TrustTier = "T1"
)

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityNormal   Priority = "normal"
)

type Duration struct {
	time.Duration
}

func (duration *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", node.Value, err)
	}
	duration.Duration = parsed
	return nil
}

type Date struct {
	time.Time
}

func (date *Date) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("date must be a scalar")
	}
	parsed, err := time.Parse(time.DateOnly, node.Value)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", node.Value, err)
	}
	date.Time = parsed
	return nil
}

type Registry struct {
	Version      int          `yaml:"version"`
	Enabled      bool         `yaml:"enabled"`
	Contact      string       `yaml:"contact"`
	Sources      []Source     `yaml:"sources"`
	Repositories []Repository `yaml:"repositories"`
}

type Source struct {
	ID                   string    `yaml:"id"`
	Name                 string    `yaml:"name"`
	TrustTier            TrustTier `yaml:"trust_tier"`
	Connector            Connector `yaml:"connector"`
	URL                  string    `yaml:"url"`
	Topics               []string  `yaml:"topics"`
	PollInterval         Duration  `yaml:"poll_interval"`
	Priority             Priority  `yaml:"priority"`
	RobotsPolicy         string    `yaml:"robots_policy"`
	ContentLicense       string    `yaml:"content_license"`
	Enabled              bool      `yaml:"enabled"`
	Owner                string    `yaml:"owner"`
	Origin               string    `yaml:"origin"`
	ReviewedAt           Date      `yaml:"reviewed_at"`
	ExpectedContentTypes []string  `yaml:"expected_content_types"`
	MaxResponseBytes     int64     `yaml:"max_response_bytes"`
	FixtureSuite         string    `yaml:"fixture_suite"`
}

type RepositoryEvent string

const (
	RepositoryEventReleases           RepositoryEvent = "releases"
	RepositoryEventSecurityAdvisories RepositoryEvent = "security_advisories"
)

type Repository struct {
	ID               string                     `yaml:"id"`
	Name             string                     `yaml:"name"`
	TrustTier        TrustTier                  `yaml:"trust_tier"`
	URL              string                     `yaml:"url"`
	NodeID           string                     `yaml:"node_id"`
	RepositoryOwner  string                     `yaml:"repository_owner"`
	RepositoryName   string                     `yaml:"repository_name"`
	Topics           []string                   `yaml:"topics"`
	PollInterval     Duration                   `yaml:"poll_interval"`
	Priority         Priority                   `yaml:"priority"`
	ContentLicense   string                     `yaml:"content_license"`
	Enabled          bool                       `yaml:"enabled"`
	Owner            string                     `yaml:"owner"`
	Origin           string                     `yaml:"origin"`
	ReviewedAt       Date                       `yaml:"reviewed_at"`
	MaxResponseBytes int64                      `yaml:"max_response_bytes"`
	EnabledEvents    []RepositoryEvent          `yaml:"enabled_events"`
	FixtureSuites    map[RepositoryEvent]string `yaml:"fixture_suites"`
}

type Endpoint struct {
	ID                   string
	SourceID             string
	Name                 string
	TrustTier            TrustTier
	Connector            Connector
	URL                  string
	Topics               []string
	PollInterval         time.Duration
	Priority             Priority
	RobotsPolicy         string
	ContentLicense       string
	Enabled              bool
	Owner                string
	Origin               string
	ReviewedAt           time.Time
	ExpectedContentTypes []string
	MaxResponseBytes     int64
	FixtureSuite         string
	RepositoryNodeID     string
	RepositoryOwner      string
	RepositoryName       string
	RepositoryEvent      RepositoryEvent
}

type FixtureCatalog struct {
	Version  int              `yaml:"version"`
	Profiles []FixtureProfile `yaml:"profiles"`
	Suites   []FixtureSuite   `yaml:"suites"`
}

type FixtureProfile struct {
	ID    string        `yaml:"id"`
	Cases []FixtureCase `yaml:"cases"`
}

type FixtureCase struct {
	ID        string            `yaml:"id"`
	Status    int               `yaml:"status"`
	Headers   map[string]string `yaml:"headers"`
	Payload   string            `yaml:"payload"`
	Generator *BodyGenerator    `yaml:"generator"`
	Outcome   string            `yaml:"outcome"`
}

type BodyGenerator struct {
	Kind        string `yaml:"kind"`
	Byte        string `yaml:"byte"`
	Repeat      int64  `yaml:"repeat"`
	Compression string `yaml:"compression"`
}

type FixtureSuite struct {
	ID          string                    `yaml:"id"`
	Connector   Connector                 `yaml:"connector"`
	Profile     string                    `yaml:"profile"`
	ContentType string                    `yaml:"content_type"`
	Payloads    map[string]FixturePayload `yaml:"payloads"`
}

type FixturePayload struct {
	Body        *string `yaml:"body"`
	BodyBase64  *string `yaml:"body_base64"`
	ContentType string  `yaml:"content_type"`
}
