package sources

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maximumFeedBytes = 5 << 20
	maximumPageBytes = 15 << 20
	maximumJSONBytes = 10 << 20
	minimumEndpoints = 60
	maximumReviewAge = 90 * 24 * time.Hour
)

var (
	idPattern         = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	topicPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	requiredCases     = []string{
		"normal",
		"empty",
		"malformed",
		"redirect",
		"not_modified",
		"rate_limited",
		"server_error",
		"oversized",
		"compression_bomb",
		"changed_revision",
		"duplicate_story",
		"prompt_injection",
		"character_encoding",
	}
	requiredCaseStatuses = map[string]int{
		"normal":             200,
		"empty":              200,
		"malformed":          200,
		"redirect":           302,
		"not_modified":       304,
		"rate_limited":       429,
		"server_error":       500,
		"oversized":          200,
		"compression_bomb":   200,
		"changed_revision":   200,
		"duplicate_story":    200,
		"prompt_injection":   200,
		"character_encoding": 200,
	}
	approvedConnectors = []Connector{
		ConnectorAtom,
		ConnectorRSS,
		ConnectorJSONFeed,
		ConnectorPage,
		ConnectorGitHubReleases,
		ConnectorGitHubAdvisories,
		ConnectorRegistry,
		ConnectorStructuredAPI,
	}
)

func Validate(registry Registry, catalog FixtureCatalog, now time.Time) error {
	var validationErrors []error
	validationErrors = append(validationErrors, validateFixtures(catalog)...)
	validationErrors = append(validationErrors, validateRegistry(registry, catalog, now)...)
	return errors.Join(validationErrors...)
}

func (registry Registry) Endpoints() []Endpoint {
	endpoints := make([]Endpoint, 0, len(registry.Sources)+(len(registry.Repositories)*2))
	for _, source := range registry.Sources {
		endpoints = append(endpoints, Endpoint{
			ID:                   source.ID,
			SourceID:             source.ID,
			Name:                 source.Name,
			TrustTier:            source.TrustTier,
			Connector:            source.Connector,
			URL:                  source.URL,
			Topics:               slices.Clone(source.Topics),
			PollInterval:         source.PollInterval.Duration,
			Priority:             source.Priority,
			RobotsPolicy:         source.RobotsPolicy,
			ContentLicense:       source.ContentLicense,
			Enabled:              source.Enabled,
			Owner:                source.Owner,
			Origin:               source.Origin,
			ReviewedAt:           source.ReviewedAt.Time,
			ExpectedContentTypes: slices.Clone(source.ExpectedContentTypes),
			MaxResponseBytes:     source.MaxResponseBytes,
			FixtureSuite:         source.FixtureSuite,
		})
	}
	for _, repository := range registry.Repositories {
		for _, event := range repository.EnabledEvents {
			connector, suffix := connectorForEvent(event)
			endpoints = append(endpoints, Endpoint{
				ID:                   repository.ID + "-" + strings.ReplaceAll(string(event), "_", "-"),
				SourceID:             repository.ID,
				Name:                 repository.Name + " " + eventLabel(event),
				TrustTier:            repository.TrustTier,
				Connector:            connector,
				URL:                  strings.TrimSuffix(repository.URL, "/") + suffix,
				Topics:               slices.Clone(repository.Topics),
				PollInterval:         repository.PollInterval.Duration,
				Priority:             repository.Priority,
				RobotsPolicy:         "api",
				ContentLicense:       repository.ContentLicense,
				Enabled:              repository.Enabled,
				Owner:                repository.Owner,
				Origin:               repository.Origin,
				ReviewedAt:           repository.ReviewedAt.Time,
				ExpectedContentTypes: []string{"application/vnd.github+json", "application/json"},
				MaxResponseBytes:     repository.MaxResponseBytes,
				FixtureSuite:         repository.FixtureSuites[event],
				RepositoryNodeID:     repository.NodeID,
				RepositoryOwner:      repository.RepositoryOwner,
				RepositoryName:       repository.RepositoryName,
				RepositoryEvent:      event,
			})
		}
	}
	return endpoints
}

func connectorForEvent(event RepositoryEvent) (Connector, string) {
	switch event {
	case RepositoryEventReleases:
		return ConnectorGitHubReleases, "/releases"
	case RepositoryEventSecurityAdvisories:
		return ConnectorGitHubAdvisories, "/security-advisories"
	default:
		return Connector(""), ""
	}
}

func eventLabel(event RepositoryEvent) string {
	if event == RepositoryEventSecurityAdvisories {
		return "security advisories"
	}
	return string(event)
}

func validateRegistry(registry Registry, catalog FixtureCatalog, now time.Time) []error {
	var validationErrors []error
	if registry.Version != RegistryVersion {
		validationErrors = append(validationErrors, fmt.Errorf("registry version must be %d", RegistryVersion))
	}
	if registry.Enabled {
		validationErrors = append(validationErrors, errors.New("live source fetching must remain disabled until PR 4"))
	}
	if err := validateHTTPSURL(registry.Contact); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("registry contact: %w", err))
	}
	if len(registry.Sources) == 0 {
		validationErrors = append(validationErrors, errors.New("registry requires official sources"))
	}
	if len(registry.Repositories) == 0 {
		validationErrors = append(validationErrors, errors.New("registry requires repository watches"))
	}

	suites := make(map[string]Connector, len(catalog.Suites))
	for _, suite := range catalog.Suites {
		suites[suite.ID] = suite.Connector
	}

	identifiers := make(map[string]struct{}, len(registry.Sources)+len(registry.Repositories))
	for index, source := range registry.Sources {
		context := fmt.Sprintf("sources[%d] %q", index, source.ID)
		validationErrors = append(validationErrors, validateSource(source, context, suites, now)...)
		if _, exists := identifiers[source.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate id", context))
		}
		identifiers[source.ID] = struct{}{}
	}

	nodeIDs := make(map[string]struct{}, len(registry.Repositories))
	repositoryNames := make(map[string]struct{}, len(registry.Repositories))
	for index, repository := range registry.Repositories {
		context := fmt.Sprintf("repositories[%d] %q", index, repository.ID)
		validationErrors = append(validationErrors, validateRepository(repository, context, suites, now)...)
		if _, exists := identifiers[repository.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate id", context))
		}
		identifiers[repository.ID] = struct{}{}
		if _, exists := nodeIDs[repository.NodeID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate repository node id", context))
		}
		nodeIDs[repository.NodeID] = struct{}{}
		fullName := strings.ToLower(repository.RepositoryOwner + "/" + repository.RepositoryName)
		if _, exists := repositoryNames[fullName]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate repository", context))
		}
		repositoryNames[fullName] = struct{}{}
	}

	endpoints := registry.Endpoints()
	if len(endpoints) < minimumEndpoints {
		validationErrors = append(validationErrors, fmt.Errorf("registry has %d endpoints; requires at least %d", len(endpoints), minimumEndpoints))
	}
	endpointIDs := make(map[string]struct{}, len(endpoints))
	endpointURLs := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if _, exists := endpointIDs[endpoint.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("endpoint %q has a duplicate id", endpoint.ID))
		}
		endpointIDs[endpoint.ID] = struct{}{}
		if _, exists := endpointURLs[endpoint.URL]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("endpoint %q has a duplicate URL", endpoint.ID))
		}
		endpointURLs[endpoint.URL] = struct{}{}
	}
	return validationErrors
}

func validateSource(source Source, context string, suites map[string]Connector, now time.Time) []error {
	validationErrors := validateCommon(
		context,
		source.ID,
		source.Name,
		source.TrustTier,
		source.URL,
		source.Topics,
		source.PollInterval.Duration,
		source.Priority,
		source.ContentLicense,
		source.Owner,
		source.Origin,
		source.ReviewedAt.Time,
		source.MaxResponseBytes,
		now,
	)
	if !slices.Contains(approvedConnectors, source.Connector) {
		validationErrors = append(validationErrors, fmt.Errorf("%s: unsupported connector %q", context, source.Connector))
	}
	if source.Connector == ConnectorGitHubReleases || source.Connector == ConnectorGitHubAdvisories {
		validationErrors = append(validationErrors, fmt.Errorf("%s: GitHub connectors require a repository watch", context))
	}
	if source.RobotsPolicy != "feed" && source.RobotsPolicy != "page" && source.RobotsPolicy != "api" {
		validationErrors = append(validationErrors, fmt.Errorf("%s: invalid robots policy %q", context, source.RobotsPolicy))
	}
	validationErrors = append(validationErrors, validateContentTypes(context, source.ExpectedContentTypes)...)
	if maximum := maximumBytesFor(source.Connector); source.MaxResponseBytes > maximum {
		validationErrors = append(validationErrors, fmt.Errorf("%s: max_response_bytes exceeds connector limit %d", context, maximum))
	}
	if connector, exists := suites[source.FixtureSuite]; !exists {
		validationErrors = append(validationErrors, fmt.Errorf("%s: fixture suite %q does not exist", context, source.FixtureSuite))
	} else if connector != source.Connector {
		validationErrors = append(validationErrors, fmt.Errorf("%s: fixture suite %q is for %s", context, source.FixtureSuite, connector))
	}
	return validationErrors
}

func validateRepository(repository Repository, context string, suites map[string]Connector, now time.Time) []error {
	validationErrors := validateCommon(
		context,
		repository.ID,
		repository.Name,
		repository.TrustTier,
		repository.URL,
		repository.Topics,
		repository.PollInterval.Duration,
		repository.Priority,
		repository.ContentLicense,
		repository.Owner,
		repository.Origin,
		repository.ReviewedAt.Time,
		repository.MaxResponseBytes,
		now,
	)
	if repository.NodeID == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s: node_id is required", context))
	}
	if !repositoryPattern.MatchString(repository.RepositoryOwner) || !repositoryPattern.MatchString(repository.RepositoryName) {
		validationErrors = append(validationErrors, fmt.Errorf("%s: invalid repository owner or name", context))
	}
	expectedURL := "https://api.github.com/repos/" + repository.RepositoryOwner + "/" + repository.RepositoryName
	if repository.URL != expectedURL {
		validationErrors = append(validationErrors, fmt.Errorf("%s: URL must be %q", context, expectedURL))
	}
	if repository.MaxResponseBytes > maximumJSONBytes {
		validationErrors = append(validationErrors, fmt.Errorf("%s: max_response_bytes exceeds GitHub API limit %d", context, maximumJSONBytes))
	}
	if len(repository.EnabledEvents) == 0 {
		validationErrors = append(validationErrors, fmt.Errorf("%s: enabled_events is required", context))
	}
	seenEvents := make(map[RepositoryEvent]struct{}, len(repository.EnabledEvents))
	for _, event := range repository.EnabledEvents {
		connector, _ := connectorForEvent(event)
		if connector == "" {
			validationErrors = append(validationErrors, fmt.Errorf("%s: unsupported event %q", context, event))
			continue
		}
		if _, exists := seenEvents[event]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate event %q", context, event))
		}
		seenEvents[event] = struct{}{}
		suiteID := repository.FixtureSuites[event]
		if suiteID == "" {
			validationErrors = append(validationErrors, fmt.Errorf("%s: event %q requires a fixture suite", context, event))
		} else if suites[suiteID] != connector {
			validationErrors = append(validationErrors, fmt.Errorf("%s: fixture suite %q does not match event %q", context, suiteID, event))
		}
	}
	if repository.Priority == PriorityCritical {
		if _, exists := seenEvents[RepositoryEventReleases]; !exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: critical repository requires releases", context))
		}
		if _, exists := seenEvents[RepositoryEventSecurityAdvisories]; !exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: critical repository requires security advisories", context))
		}
	}
	return validationErrors
}

func validateCommon(
	context string,
	id string,
	name string,
	trustTier TrustTier,
	rawURL string,
	topics []string,
	pollInterval time.Duration,
	priority Priority,
	contentLicense string,
	owner string,
	origin string,
	reviewedAt time.Time,
	maxResponseBytes int64,
	now time.Time,
) []error {
	var validationErrors []error
	if !idPattern.MatchString(id) {
		validationErrors = append(validationErrors, fmt.Errorf("%s: invalid id", context))
	}
	if strings.TrimSpace(name) == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s: name is required", context))
	}
	if trustTier != TrustTierZero && trustTier != TrustTierOne {
		validationErrors = append(validationErrors, fmt.Errorf("%s: trust tier must be T0 or T1", context))
	}
	if err := validateHTTPSURL(rawURL); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("%s: %w", context, err))
	}
	validationErrors = append(validationErrors, validateTopics(context, topics)...)
	if pollInterval < 5*time.Minute || pollInterval > 7*24*time.Hour {
		validationErrors = append(validationErrors, fmt.Errorf("%s: poll interval must be from 5m through 168h", context))
	}
	if priority != PriorityCritical && priority != PriorityHigh && priority != PriorityNormal {
		validationErrors = append(validationErrors, fmt.Errorf("%s: invalid priority %q", context, priority))
	}
	if contentLicense != "link-and-excerpt" && contentLicense != "metadata-only" {
		validationErrors = append(validationErrors, fmt.Errorf("%s: invalid content license %q", context, contentLicense))
	}
	if owner != "system" || origin != "system" {
		validationErrors = append(validationErrors, fmt.Errorf("%s: built-in entries must be owned and originated by system", context))
	}
	if reviewedAt.IsZero() {
		validationErrors = append(validationErrors, fmt.Errorf("%s: reviewed_at is required", context))
	} else {
		reviewedAt = reviewedAt.UTC()
		now = now.UTC()
		if reviewedAt.After(now.Add(24 * time.Hour)) {
			validationErrors = append(validationErrors, fmt.Errorf("%s: reviewed_at is in the future", context))
		}
		if now.Sub(reviewedAt) > maximumReviewAge {
			validationErrors = append(validationErrors, fmt.Errorf("%s: review is older than 90 days", context))
		}
	}
	if maxResponseBytes <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("%s: max_response_bytes must be positive", context))
	}
	return validationErrors
}

func validateHTTPSURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("URL must be absolute HTTPS")
	}
	if parsed.User != nil {
		return errors.New("URL must not contain user information")
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return errors.New("URL may use only the default HTTPS port")
	}
	if parsed.Fragment != "" {
		return errors.New("URL must not contain a fragment")
	}
	return nil
}

func validateTopics(context string, topics []string) []error {
	if len(topics) == 0 {
		return []error{fmt.Errorf("%s: at least one topic is required", context)}
	}
	var validationErrors []error
	seen := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		if !topicPattern.MatchString(topic) {
			validationErrors = append(validationErrors, fmt.Errorf("%s: invalid topic %q", context, topic))
		}
		if _, exists := seen[topic]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("%s: duplicate topic %q", context, topic))
		}
		seen[topic] = struct{}{}
	}
	return validationErrors
}

func validateContentTypes(context string, contentTypes []string) []error {
	if len(contentTypes) == 0 {
		return []error{fmt.Errorf("%s: expected_content_types is required", context)}
	}
	var validationErrors []error
	for _, contentType := range contentTypes {
		if _, _, err := mime.ParseMediaType(contentType); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("%s: invalid content type %q", context, contentType))
		}
	}
	return validationErrors
}

func maximumBytesFor(connector Connector) int64 {
	switch connector {
	case ConnectorAtom, ConnectorRSS:
		return maximumFeedBytes
	case ConnectorPage:
		return maximumPageBytes
	default:
		return maximumJSONBytes
	}
}

func validateFixtures(catalog FixtureCatalog) []error {
	var validationErrors []error
	if catalog.Version != FixtureVersion {
		validationErrors = append(validationErrors, fmt.Errorf("fixture version must be %d", FixtureVersion))
	}
	if len(catalog.Profiles) != 1 {
		validationErrors = append(validationErrors, errors.New("fixtures require exactly one HTTP scenario profile"))
	}
	profiles := make(map[string]FixtureProfile, len(catalog.Profiles))
	for _, profile := range catalog.Profiles {
		if profile.ID == "" {
			validationErrors = append(validationErrors, errors.New("fixture profile id is required"))
		}
		if _, exists := profiles[profile.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("duplicate fixture profile %q", profile.ID))
		}
		profiles[profile.ID] = profile
		validationErrors = append(validationErrors, validateFixtureProfile(profile)...)
	}

	suiteIDs := make(map[string]struct{}, len(catalog.Suites))
	coveredConnectors := make(map[Connector]struct{}, len(catalog.Suites))
	for _, suite := range catalog.Suites {
		if !idPattern.MatchString(suite.ID) {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q has an invalid id", suite.ID))
		}
		if _, exists := suiteIDs[suite.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("duplicate fixture suite %q", suite.ID))
		}
		suiteIDs[suite.ID] = struct{}{}
		if !slices.Contains(approvedConnectors, suite.Connector) {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q has unsupported connector %q", suite.ID, suite.Connector))
		}
		coveredConnectors[suite.Connector] = struct{}{}
		profile, exists := profiles[suite.Profile]
		if !exists {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q references unknown profile %q", suite.ID, suite.Profile))
			continue
		}
		if _, _, err := mime.ParseMediaType(suite.ContentType); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q has invalid content type", suite.ID))
		}
		validationErrors = append(validationErrors, validateFixturePayloads(suite, profile)...)
	}
	for _, connector := range approvedConnectors {
		if _, exists := coveredConnectors[connector]; !exists {
			validationErrors = append(validationErrors, fmt.Errorf("connector %q lacks a fixture suite", connector))
		}
	}
	return validationErrors
}

func validateFixtureProfile(profile FixtureProfile) []error {
	var validationErrors []error
	cases := make(map[string]FixtureCase, len(profile.Cases))
	for _, fixtureCase := range profile.Cases {
		if _, exists := cases[fixtureCase.ID]; exists {
			validationErrors = append(validationErrors, fmt.Errorf("fixture profile %q has duplicate case %q", profile.ID, fixtureCase.ID))
		}
		cases[fixtureCase.ID] = fixtureCase
		if fixtureCase.Status < 100 || fixtureCase.Status > 599 {
			validationErrors = append(validationErrors, fmt.Errorf("fixture case %q has invalid status", fixtureCase.ID))
		}
		if fixtureCase.Outcome == "" {
			validationErrors = append(validationErrors, fmt.Errorf("fixture case %q requires an outcome", fixtureCase.ID))
		}
		if fixtureCase.Payload != "" && fixtureCase.Generator != nil {
			validationErrors = append(validationErrors, fmt.Errorf("fixture case %q cannot have payload and generator", fixtureCase.ID))
		}
		if fixtureCase.Generator != nil {
			validationErrors = append(validationErrors, validateGenerator(fixtureCase.ID, *fixtureCase.Generator)...)
		}
	}
	for _, required := range requiredCases {
		fixtureCase, exists := cases[required]
		if !exists {
			validationErrors = append(validationErrors, fmt.Errorf("fixture profile %q lacks case %q", profile.ID, required))
			continue
		}
		if fixtureCase.Status != requiredCaseStatuses[required] {
			validationErrors = append(validationErrors, fmt.Errorf("fixture case %q must use status %d", required, requiredCaseStatuses[required]))
		}
	}
	payloadCases := []string{
		"normal",
		"empty",
		"malformed",
		"changed_revision",
		"duplicate_story",
		"prompt_injection",
		"character_encoding",
	}
	for _, caseID := range payloadCases {
		if fixtureCase, exists := cases[caseID]; exists && fixtureCase.Payload != caseID {
			validationErrors = append(validationErrors, fmt.Errorf("fixture case %q must reference payload %q", caseID, caseID))
		}
	}
	if oversized, exists := cases["oversized"]; exists {
		if oversized.Generator == nil || oversized.Generator.Repeat <= maximumPageBytes || oversized.Generator.Compression != "" {
			validationErrors = append(validationErrors, errors.New("oversized fixture requires an uncompressed body larger than the maximum page limit"))
		}
	}
	if compressionBomb, exists := cases["compression_bomb"]; exists {
		if compressionBomb.Generator == nil || compressionBomb.Generator.Repeat < maximumPageBytes || compressionBomb.Generator.Compression != "gzip" {
			validationErrors = append(validationErrors, errors.New("compression-bomb fixture requires a page-sized gzip generator"))
		}
	}
	if redirect := cases["redirect"]; redirect.Headers["Location"] == "" {
		validationErrors = append(validationErrors, errors.New("redirect fixture requires Location"))
	}
	if rateLimited := cases["rate_limited"]; rateLimited.Headers["Retry-After"] == "" {
		validationErrors = append(validationErrors, errors.New("rate-limited fixture requires Retry-After"))
	}
	return validationErrors
}

func validateGenerator(caseID string, generator BodyGenerator) []error {
	var validationErrors []error
	if generator.Kind != "repeated-byte" || len(generator.Byte) != 1 || generator.Repeat <= 0 {
		validationErrors = append(validationErrors, fmt.Errorf("fixture case %q has invalid body generator", caseID))
	}
	if generator.Compression != "" && generator.Compression != "gzip" {
		validationErrors = append(validationErrors, fmt.Errorf("fixture case %q has unsupported compression", caseID))
	}
	return validationErrors
}

func validateFixturePayloads(suite FixtureSuite, profile FixtureProfile) []error {
	var validationErrors []error
	for _, fixtureCase := range profile.Cases {
		if fixtureCase.Payload == "" {
			continue
		}
		payload, exists := suite.Payloads[fixtureCase.Payload]
		if !exists {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q lacks payload %q", suite.ID, fixtureCase.Payload))
			continue
		}
		if (payload.Body == nil) == (payload.BodyBase64 == nil) {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q payload %q requires exactly one body encoding", suite.ID, fixtureCase.Payload))
		}
		if payload.BodyBase64 != nil {
			if _, err := base64.StdEncoding.DecodeString(*payload.BodyBase64); err != nil {
				validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q payload %q is not valid base64", suite.ID, fixtureCase.Payload))
			}
		}
		if fixtureCase.ID != "empty" && fixturePayloadLength(payload) == 0 {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q payload %q must not be empty", suite.ID, fixtureCase.Payload))
		}
		contentType := payload.ContentType
		if contentType == "" {
			contentType = suite.ContentType
		}
		if _, _, err := mime.ParseMediaType(contentType); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q payload %q has invalid content type", suite.ID, fixtureCase.Payload))
		}
	}
	if normal, normalExists := suite.Payloads["normal"]; normalExists {
		if changed, changedExists := suite.Payloads["changed_revision"]; changedExists && equalPayloads(normal, changed) {
			validationErrors = append(validationErrors, fmt.Errorf("fixture suite %q changed revision matches normal", suite.ID))
		}
	}
	return validationErrors
}

func fixturePayloadLength(payload FixturePayload) int {
	if payload.Body != nil {
		return len(*payload.Body)
	}
	if payload.BodyBase64 != nil {
		decoded, err := base64.StdEncoding.DecodeString(*payload.BodyBase64)
		if err == nil {
			return len(decoded)
		}
	}
	return 0
}

func equalPayloads(first FixturePayload, second FixturePayload) bool {
	if first.Body != nil && second.Body != nil {
		return *first.Body == *second.Body
	}
	if first.BodyBase64 != nil && second.BodyBase64 != nil {
		return *first.BodyBase64 == *second.BodyBase64
	}
	return false
}
