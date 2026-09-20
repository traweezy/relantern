package parsing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/traweezy/relantern/internal/sources"
)

const maximumEntries = 500
const maximumEntryBytes = 1 << 20
const maximumAdvisoryVulnerabilities = 100

// Entry is a bounded, independently versioned child of one fetched document.
// Payload is a deterministic JSON representation of the untrusted entry fields.
type Entry struct {
	ExternalID string
	URL        string
	Payload    []byte
}

// ValidateEntry checks a child before it crosses into durable storage.
func ValidateEntry(sourceURL string, entry Entry) error {
	if entry.ExternalID == "" || len(entry.ExternalID) > 2048 || len(entry.Payload) == 0 || len(entry.Payload) > maximumEntryBytes {
		return parserError(ErrorInvalidDocument, "source entry identity or payload is invalid")
	}
	base, err := url.Parse(sourceURL)
	if err != nil || base.Hostname() == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return parserError(ErrorInvalidURL, "source entry parent URL is invalid")
	}
	var payload sourceEntryPayload
	if err := decodeSingleJSON(entry.Payload, &payload); err != nil {
		return err
	}
	if payload.ID != entry.ExternalID || payload.URL != entry.URL {
		return parserError(ErrorInvalidDocument, "source entry identity or URL differs from its payload")
	}
	if err := validateEntryAdvisory(payload); err != nil {
		return err
	}
	link := entry.URL
	if payload.ExternalURL != "" {
		link = payload.ExternalURL
	}
	validatedURL, err := entryURL(base, link, entry.ExternalID)
	if err != nil {
		return err
	}
	if payload.ExternalURL != "" {
		if validatedURL != payload.ExternalURL {
			return parserError(ErrorInvalidURL, "source entry external URL is not canonical")
		}
		validatedURL = fallbackEntryURL(base, entry.ExternalID)
	}
	if validatedURL != entry.URL {
		return parserError(ErrorInvalidURL, "source entry URL is not canonical")
	}
	parsedURL, err := url.Parse(entry.URL)
	if err != nil || !reviewedEntryOrigin(base, parsedURL) {
		return parserError(ErrorInvalidURL, "source entry canonical URL leaves its reviewed origin")
	}
	return nil
}

type sourceEntryPayload struct {
	ID          string                 `json:"id"`
	URL         string                 `json:"url"`
	ExternalURL string                 `json:"externalUrl,omitempty"`
	Title       string                 `json:"title"`
	Author      string                 `json:"author,omitempty"`
	ContentHTML string                 `json:"contentHtml,omitempty"`
	ContentText string                 `json:"contentText,omitempty"`
	PublishedAt string                 `json:"publishedAt,omitempty"`
	UpdatedAt   string                 `json:"updatedAt,omitempty"`
	Advisory    *sourceAdvisoryPayload `json:"advisory,omitempty"`
}

// Advisory fields retain only bounded source-declared facts. They do not
// classify an advisory as urgent or establish an affected owner dependency.
type sourceAdvisoryPayload struct {
	ID              string                        `json:"id"`
	Type            string                        `json:"type,omitempty"`
	Severity        string                        `json:"severity,omitempty"`
	State           string                        `json:"state,omitempty"`
	PublishedAt     string                        `json:"publishedAt,omitempty"`
	ReviewedAt      string                        `json:"reviewedAt,omitempty"`
	WithdrawnAt     string                        `json:"withdrawnAt,omitempty"`
	Vulnerabilities []sourceAdvisoryVulnerability `json:"vulnerabilities,omitempty"`
}

type sourceAdvisoryVulnerability struct {
	Ecosystem      string `json:"ecosystem"`
	PackageName    string `json:"packageName,omitempty"`
	VersionRange   string `json:"versionRange,omitempty"`
	PatchedVersion string `json:"patchedVersion,omitempty"`
}

func validateEntryAdvisory(entry sourceEntryPayload) error {
	if entry.Advisory == nil {
		return nil
	}
	if !strings.HasPrefix(entry.ID, string(sources.ConnectorGitHubAdvisories)+":") {
		return parserError(ErrorInvalidDocument, "advisory metadata requires a GitHub advisory entry")
	}
	return validateAdvisory(entry.Advisory)
}

func IsCollectionConnector(connector sources.Connector) bool {
	switch connector {
	case sources.ConnectorAtom, sources.ConnectorRSS, sources.ConnectorJSONFeed,
		sources.ConnectorGitHubReleases, sources.ConnectorGitHubAdvisories,
		sources.ConnectorStructuredAPI:
		return true
	default:
		return false
	}
}

func SplitEntries(ctx context.Context, connector sources.Connector, sourceURL string, raw []byte) ([]Entry, error) {
	if !IsCollectionConnector(connector) {
		return nil, fmt.Errorf("connector %q does not contain independent entries", connector)
	}
	parsedURL, err := url.Parse(sourceURL)
	if err != nil || parsedURL.Hostname() == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return nil, errors.New("source entry extraction requires an absolute HTTP(S) URL")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var candidates []sourceEntryPayload
	switch connector {
	case sources.ConnectorAtom, sources.ConnectorRSS:
		candidates, err = feedEntries(connector, raw)
	case sources.ConnectorJSONFeed:
		candidates, err = jsonFeedEntries(raw)
	case sources.ConnectorGitHubReleases, sources.ConnectorGitHubAdvisories:
		candidates, err = githubEntries(connector, raw)
	case sources.ConnectorStructuredAPI:
		candidates, err = structuredEntries(raw)
	}
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []Entry{}, nil
	}
	if len(candidates) > maximumEntries {
		return nil, parserError(ErrorBodyTooLarge, "source contains %d entries; maximum is %d", len(candidates), maximumEntries)
	}
	entries := make([]Entry, 0, len(candidates))
	seen := make(map[string][sha256.Size]byte, len(candidates))
	for index, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := buildEntry(connector, parsedURL, candidate)
		if err != nil {
			return nil, fmt.Errorf("source entry %d: %w", index, err)
		}
		digest := sha256.Sum256(entry.Payload)
		if previous, exists := seen[entry.ExternalID]; exists {
			if previous != digest {
				return nil, parserError(ErrorInvalidDocument, "source repeats external ID %q with conflicting content", entry.ExternalID)
			}
			continue
		}
		seen[entry.ExternalID] = digest
		entries = append(entries, entry)
	}
	return entries, nil
}

func buildEntry(connector sources.Connector, sourceURL *url.URL, candidate sourceEntryPayload) (Entry, error) {
	identity := strings.TrimSpace(candidate.ID)
	if identity == "" {
		identity = strings.TrimSpace(candidate.URL)
	}
	if identity == "" {
		fallback := sha256.Sum256([]byte(candidate.Title + "\x00" + candidate.PublishedAt + "\x00" + candidate.ContentText + candidate.ContentHTML))
		identity = "content-sha256:" + hex.EncodeToString(fallback[:])
	}
	externalID := string(connector) + ":" + identity
	if len(externalID) > 2048 {
		digest := sha256.Sum256([]byte(externalID))
		externalID = string(connector) + ":sha256:" + hex.EncodeToString(digest[:])
	}
	canonicalURL, err := entryURL(sourceURL, candidate.URL, externalID)
	if err != nil {
		return Entry{}, err
	}
	if resolved, err := url.Parse(canonicalURL); err == nil && !reviewedEntryOrigin(sourceURL, resolved) {
		candidate.ExternalURL = canonicalURL
		canonicalURL = fallbackEntryURL(sourceURL, externalID)
	}
	candidate.ID = externalID
	candidate.URL = canonicalURL
	payload, err := json.Marshal(candidate)
	if err != nil {
		return Entry{}, fmt.Errorf("encode source entry: %w", err)
	}
	if len(payload) > maximumEntryBytes {
		return Entry{}, parserError(ErrorBodyTooLarge, "source entry exceeds %d bytes", maximumEntryBytes)
	}
	return Entry{ExternalID: externalID, URL: canonicalURL, Payload: payload}, nil
}

func sameEntryOrigin(first, second *url.URL) bool {
	return strings.EqualFold(first.Hostname(), second.Hostname()) && first.Port() == second.Port() && first.Scheme == second.Scheme
}

func reviewedEntryOrigin(sourceURL, entryURL *url.URL) bool {
	return sameEntryOrigin(sourceURL, entryURL) || githubRepositoryEntryURL(sourceURL, entryURL)
}

// GitHub's REST endpoint and its web pages are separate origins. This permits
// only item pages under the exact repository named by a reviewed API endpoint.
func githubRepositoryEntryURL(sourceURL, entryURL *url.URL) bool {
	if sourceURL.Scheme != "https" || !strings.EqualFold(sourceURL.Hostname(), "api.github.com") || sourceURL.Port() != "" ||
		entryURL.Scheme != "https" || !strings.EqualFold(entryURL.Hostname(), "github.com") || entryURL.Port() != "" ||
		entryURL.User != nil || entryURL.RawQuery != "" {
		return false
	}
	apiParts := strings.Split(strings.Trim(sourceURL.EscapedPath(), "/"), "/")
	webParts := strings.Split(strings.Trim(entryURL.EscapedPath(), "/"), "/")
	if len(apiParts) != 4 || apiParts[0] != "repos" || apiParts[1] == "" || apiParts[2] == "" ||
		len(webParts) != 5 || !strings.EqualFold(apiParts[1], webParts[0]) ||
		!strings.EqualFold(apiParts[2], webParts[1]) || webParts[4] == "" {
		return false
	}
	switch apiParts[3] {
	case "releases":
		return webParts[2] == "releases" && webParts[3] == "tag"
	case "security-advisories":
		return webParts[2] == "security" && webParts[3] == "advisories" &&
			strings.HasPrefix(strings.ToUpper(webParts[4]), "GHSA-")
	default:
		return false
	}
}

func fallbackEntryURL(sourceURL *url.URL, externalID string) string {
	fallback := *sourceURL
	query := fallback.Query()
	digest := sha256.Sum256([]byte(externalID))
	query.Set("relantern_entry", hex.EncodeToString(digest[:]))
	fallback.RawQuery = query.Encode()
	fallback.Fragment = ""
	return fallback.String()
}

func entryURL(sourceURL *url.URL, raw string, externalID string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return fallbackEntryURL(sourceURL, externalID), nil
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", parserError(ErrorInvalidURL, "invalid source entry link")
	}
	resolved := sourceURL.ResolveReference(parsed)
	if resolved.User != nil || resolved.Hostname() == "" || len(resolved.String()) > 4096 {
		return "", parserError(ErrorInvalidURL, "source entry link has an invalid host or user information")
	}
	if resolved.Scheme != "https" && !(sourceURL.Scheme == "http" && resolved.Scheme == "http" && resolved.Hostname() == sourceURL.Hostname()) {
		return "", parserError(ErrorInvalidURL, "source entry link must use HTTPS")
	}
	if port := resolved.Port(); port != "" && port != "443" && !(sourceURL.Scheme == "http" && resolved.Host == sourceURL.Host) {
		return "", parserError(ErrorInvalidURL, "source entry link uses a nonstandard port")
	}
	if host := strings.TrimSuffix(strings.ToLower(resolved.Hostname()), "."); net.ParseIP(host) != nil ||
		host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".localdomain") {
		return "", parserError(ErrorInvalidURL, "source entry link targets a local address")
	}
	resolved.Fragment = ""
	resolved.RawFragment = ""
	return resolved.String(), nil
}

func feedEntries(connector sources.Connector, raw []byte) ([]sourceEntryPayload, error) {
	expected := gofeed.FeedTypeAtom
	if connector == sources.ConnectorRSS {
		expected = gofeed.FeedTypeRSS
	}
	if gofeed.DetectFeedType(bytes.NewReader(raw)) != expected {
		return nil, parserError(ErrorInvalidDocument, "%s parser received a different feed format", connector)
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, parserError(ErrorInvalidDocument, "parse %s entries: %v", connector, err)
	}
	if feed == nil {
		return nil, parserError(ErrorInvalidDocument, "source feed is missing")
	}
	entries := make([]sourceEntryPayload, 0, len(feed.Items))
	for _, item := range feed.Items {
		if item == nil {
			return nil, parserError(ErrorInvalidDocument, "source feed contains a nil item")
		}
		content := item.Content
		if strings.TrimSpace(content) == "" {
			content = item.Description
		}
		entries = append(entries, sourceEntryPayload{
			ID: item.GUID, URL: item.Link, Title: item.Title,
			Author: peopleNames(item.Authors), ContentHTML: content,
			PublishedAt: formatEntryTime(item.PublishedParsed), UpdatedAt: formatEntryTime(item.UpdatedParsed),
		})
	}
	return entries, nil
}

type jsonFeedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	ExternalURL   string `json:"external_url"`
	Title         string `json:"title"`
	ContentHTML   string `json:"content_html"`
	ContentText   string `json:"content_text"`
	Summary       string `json:"summary"`
	DatePublished string `json:"date_published"`
	DateModified  string `json:"date_modified"`
	Author        struct {
		Name string `json:"name"`
	} `json:"author"`
}

func jsonFeedEntries(raw []byte) ([]sourceEntryPayload, error) {
	var feed struct {
		Items []jsonFeedItem `json:"items"`
	}
	if err := decodeSingleJSON(raw, &feed); err != nil {
		return nil, err
	}
	if feed.Items == nil {
		return nil, parserError(ErrorInvalidDocument, "JSON Feed items must be an array")
	}
	entries := make([]sourceEntryPayload, 0, len(feed.Items))
	for _, item := range feed.Items {
		link := item.URL
		if link == "" {
			link = item.ExternalURL
		}
		content := item.ContentText
		if content == "" {
			content = item.Summary
		}
		entries = append(entries, sourceEntryPayload{
			ID: item.ID, URL: link, Title: item.Title, Author: item.Author.Name,
			ContentHTML: item.ContentHTML, ContentText: content,
			PublishedAt: item.DatePublished, UpdatedAt: item.DateModified,
		})
	}
	return entries, nil
}

func githubEntries(connector sources.Connector, raw []byte) ([]sourceEntryPayload, error) {
	var items []map[string]any
	if err := decodeSingleJSON(raw, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, parserError(ErrorInvalidDocument, "GitHub response must be an array")
	}
	entries := make([]sourceEntryPayload, 0, len(items))
	for _, item := range items {
		if item == nil {
			return nil, parserError(ErrorInvalidDocument, "GitHub response contains a null entry")
		}
		identity := firstString(item, "node_id", "ghsa_id", "id")
		title := firstString(item, "name", "summary", "tag_name", "ghsa_id")
		body := firstString(item, "body", "description")
		var advisory *sourceAdvisoryPayload
		if connector == sources.ConnectorGitHubAdvisories {
			var err error
			advisory, err = parseAdvisory(item)
			if err != nil {
				return nil, err
			}
			if severity := firstString(item, "severity"); severity != "" {
				body = "Severity: " + severity + "\n\n" + body
			}
		}
		entries = append(entries, sourceEntryPayload{
			ID: identity, URL: firstString(item, "html_url", "url"), Title: title,
			ContentText: body,
			PublishedAt: firstString(item, "published_at", "created_at"),
			UpdatedAt:   firstString(item, "updated_at"),
			Advisory:    advisory,
		})
	}
	return entries, nil
}

func parseAdvisory(item map[string]any) (*sourceAdvisoryPayload, error) {
	id, err := advisoryString(item, "ghsa_id", 19)
	if err != nil {
		return nil, err
	}
	if id == "" {
		for _, field := range []string{"type", "severity", "state", "published_at", "github_reviewed_at", "withdrawn_at", "vulnerabilities"} {
			if value, present := item[field]; present && value != nil {
				return nil, parserError(ErrorInvalidDocument, "GitHub advisory metadata requires ghsa_id")
			}
		}
		return nil, nil
	}
	advisory := &sourceAdvisoryPayload{ID: id}
	if advisory.Type, err = advisoryString(item, "type", 16); err != nil {
		return nil, err
	}
	if advisory.Severity, err = advisoryString(item, "severity", 16); err != nil {
		return nil, err
	}
	if advisory.State, err = advisoryString(item, "state", 16); err != nil {
		return nil, err
	}
	if advisory.PublishedAt, err = advisoryString(item, "published_at", 64); err != nil {
		return nil, err
	}
	if advisory.ReviewedAt, err = advisoryString(item, "github_reviewed_at", 64); err != nil {
		return nil, err
	}
	if advisory.WithdrawnAt, err = advisoryString(item, "withdrawn_at", 64); err != nil {
		return nil, err
	}
	if raw, present := item["vulnerabilities"]; present && raw != nil {
		vulnerabilities, ok := raw.([]any)
		if !ok || len(vulnerabilities) > maximumAdvisoryVulnerabilities {
			return nil, parserError(ErrorInvalidDocument, "GitHub advisory vulnerabilities are malformed or exceed %d", maximumAdvisoryVulnerabilities)
		}
		advisory.Vulnerabilities = make([]sourceAdvisoryVulnerability, 0, len(vulnerabilities))
		for _, rawVulnerability := range vulnerabilities {
			vulnerability, ok := rawVulnerability.(map[string]any)
			if !ok {
				return nil, parserError(ErrorInvalidDocument, "GitHub advisory vulnerability is malformed")
			}
			packageObject, ok := vulnerability["package"].(map[string]any)
			if !ok {
				return nil, parserError(ErrorInvalidDocument, "GitHub advisory package is malformed")
			}
			var parsed sourceAdvisoryVulnerability
			if parsed.Ecosystem, err = advisoryString(packageObject, "ecosystem", 32); err != nil {
				return nil, err
			}
			if parsed.PackageName, err = advisoryString(packageObject, "name", 255); err != nil {
				return nil, err
			}
			if parsed.VersionRange, err = advisoryString(vulnerability, "vulnerable_version_range", 512); err != nil {
				return nil, err
			}
			patchedVersions, patchErr := advisoryString(vulnerability, "patched_versions", 255)
			if patchErr != nil {
				return nil, patchErr
			}
			firstPatchedVersion, patchErr := advisoryString(vulnerability, "first_patched_version", 255)
			if patchErr != nil {
				return nil, patchErr
			}
			if patchedVersions != "" && firstPatchedVersion != "" && patchedVersions != firstPatchedVersion {
				return nil, parserError(ErrorInvalidDocument, "GitHub advisory patch versions conflict")
			}
			parsed.PatchedVersion = patchedVersions
			if parsed.PatchedVersion == "" {
				parsed.PatchedVersion = firstPatchedVersion
			}
			advisory.Vulnerabilities = append(advisory.Vulnerabilities, parsed)
		}
	}
	if err := validateAdvisory(advisory); err != nil {
		return nil, err
	}
	return advisory, nil
}

func advisoryString(object map[string]any, key string, maximum int) (string, error) {
	value, present := object[key]
	if !present || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", parserError(ErrorInvalidDocument, "GitHub advisory %s must be text or null", key)
	}
	if !validAdvisoryText(text, maximum) {
		return "", parserError(ErrorInvalidDocument, "GitHub advisory %s is invalid or exceeds %d bytes", key, maximum)
	}
	return text, nil
}

func validateAdvisory(advisory *sourceAdvisoryPayload) error {
	if advisory == nil || !validGHSAID(advisory.ID) {
		return parserError(ErrorInvalidDocument, "GitHub advisory ID is invalid")
	}
	if !oneOfOrEmpty(advisory.Type, "reviewed", "unreviewed", "malware") ||
		!oneOfOrEmpty(advisory.Severity, "critical", "high", "medium", "low", "unknown") ||
		!oneOfOrEmpty(advisory.State, "triage", "draft", "published", "closed") {
		return parserError(ErrorInvalidDocument, "GitHub advisory type, severity, or state is invalid")
	}
	for _, timestamp := range []string{advisory.PublishedAt, advisory.ReviewedAt, advisory.WithdrawnAt} {
		if timestamp != "" {
			if !validAdvisoryText(timestamp, 64) {
				return parserError(ErrorInvalidDocument, "GitHub advisory timestamp is invalid")
			}
			if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
				return parserError(ErrorInvalidDocument, "GitHub advisory timestamp is invalid")
			}
		}
	}
	if len(advisory.Vulnerabilities) > maximumAdvisoryVulnerabilities {
		return parserError(ErrorInvalidDocument, "GitHub advisory has too many vulnerabilities")
	}
	for _, vulnerability := range advisory.Vulnerabilities {
		if !oneOfOrEmpty(vulnerability.Ecosystem,
			"rubygems", "npm", "pip", "maven", "nuget", "composer", "go", "rust",
			"erlang", "actions", "pub", "other", "swift") ||
			vulnerability.Ecosystem == "" ||
			!validAdvisoryText(vulnerability.PackageName, 255) ||
			!validAdvisoryText(vulnerability.VersionRange, 512) ||
			!validAdvisoryText(vulnerability.PatchedVersion, 255) {
			return parserError(ErrorInvalidDocument, "GitHub advisory vulnerability metadata is invalid")
		}
	}
	return nil
}

func validGHSAID(id string) bool {
	if len(id) != 19 || !strings.HasPrefix(id, "GHSA-") {
		return false
	}
	for index := 5; index < len(id); index++ {
		if index == 9 || index == 14 {
			if id[index] != '-' {
				return false
			}
			continue
		}
		if !((id[index] >= 'a' && id[index] <= 'z') ||
			(id[index] >= 'A' && id[index] <= 'Z') ||
			(id[index] >= '0' && id[index] <= '9')) {
			return false
		}
	}
	return true
}

func oneOfOrEmpty(value string, choices ...string) bool {
	if value == "" {
		return true
	}
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func validAdvisoryText(value string, maximum int) bool {
	if len(value) > maximum || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func structuredEntries(raw []byte) ([]sourceEntryPayload, error) {
	var object struct {
		Status    string `json:"status"`
		Message   string `json:"message"`
		UpdatedAt string `json:"updated_at"`
		Services  []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			URL    string `json:"url"`
		} `json:"services"`
	}
	if err := decodeSingleJSON(raw, &object); err != nil {
		return nil, err
	}
	if len(object.Services) == 0 && object.Message == "" &&
		(object.Services == nil || strings.TrimSpace(object.Status) == "") {
		return nil, parserError(ErrorInvalidDocument, "structured API response is missing status or services")
	}
	entries := make([]sourceEntryPayload, 0, len(object.Services)+1)
	for _, service := range object.Services {
		identity := service.ID
		if identity == "" {
			identity = service.Name
		}
		entries = append(entries, sourceEntryPayload{
			ID: "service:" + identity, URL: service.URL, Title: service.Name,
			ContentText: strings.TrimSpace("Status: " + service.Status + "\n" + object.Message),
			UpdatedAt:   object.UpdatedAt,
		})
	}
	if len(entries) == 0 && object.Message != "" {
		entries = append(entries, sourceEntryPayload{
			ID: "status-message", Title: object.Status,
			ContentText: object.Message, UpdatedAt: object.UpdatedAt,
		})
	}
	return entries, nil
}

func decodeSingleJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return parserError(ErrorInvalidDocument, "decode source entries: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return parserError(ErrorInvalidDocument, "source entries have trailing JSON")
	}
	return nil
}

func parseSourceEntry(sourceURL *url.URL, raw []byte) (extractedDocument, error) {
	var entry sourceEntryPayload
	if err := decodeSingleJSON(raw, &entry); err != nil {
		return extractedDocument{}, err
	}
	if entry.ID == "" || entry.URL != sourceURL.String() {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "source entry identity or URL is inconsistent")
	}
	if err := validateEntryAdvisory(entry); err != nil {
		return extractedDocument{}, err
	}
	document := extractedDocument{
		ParserName: "source-entry-json", CanonicalURL: sourceURL.String(),
		Title: entry.Title, Author: entry.Author,
		SourcePublishedAt: parseJSONTime(entry.PublishedAt),
		SourceUpdatedAt:   parseJSONTime(entry.UpdatedAt),
	}
	if entry.Title != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 1, Anchor: "/title", Text: entry.Title})
	}
	if entry.ContentHTML != "" {
		blocks, warnings := extractUntrustedFragmentBlocks(entry.ContentHTML, "/contentHtml")
		document.Blocks = append(document.Blocks, blocks...)
		document.Warnings = append(document.Warnings, warnings...)
	} else if entry.ContentText != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "paragraph", Anchor: "/contentText", Text: entry.ContentText})
	}
	return document, nil
}

func formatEntryTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
