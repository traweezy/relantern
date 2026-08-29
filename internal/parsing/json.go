package parsing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/sources"
)

func parseJSON(connector sources.Connector, sourceURL *url.URL, payload []byte) (extractedDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "parse %s JSON: %v", connector, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return extractedDocument{}, parserError(ErrorInvalidDocument, "parse %s JSON: multiple values are not allowed", connector)
		}
		return extractedDocument{}, parserError(ErrorInvalidDocument, "parse %s JSON trailing data: %v", connector, err)
	}

	switch connector {
	case sources.ConnectorGitHubReleases:
		return parseGitHubArray(sourceURL, decoded, "github-releases-json", "name", "body", "tag_name")
	case sources.ConnectorGitHubAdvisories:
		return parseGitHubArray(sourceURL, decoded, "github-advisories-json", "summary", "description", "severity")
	case sources.ConnectorRegistry:
		return parseRegistry(sourceURL, decoded)
	case sources.ConnectorStructuredAPI:
		return parseStructuredAPI(sourceURL, decoded)
	default:
		return extractedDocument{}, parserError(ErrorUnsupported, "unsupported JSON connector %q", connector)
	}
}

func parseGitHubArray(sourceURL *url.URL, decoded any, parserName string, titleField string, bodyField string, metadataField string) (extractedDocument, error) {
	items, ok := decoded.([]any)
	if !ok {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "%s response must be an array", parserName)
	}
	if len(items) == 0 {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "%s response contains no entries", parserName)
	}
	document := extractedDocument{
		ParserName:   parserName,
		CanonicalURL: sourceURL.String(),
		Blocks:       make([]extractedBlock, 0, len(items)*3),
	}
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return extractedDocument{}, parserError(ErrorInvalidDocument, "%s entry %d must be an object", parserName, index)
		}
		anchor := fmt.Sprintf("/items/%d", index)
		identifier := firstString(object, "node_id", "ghsa_id", "id")
		if identifier != "" {
			if _, exists := seen[identifier]; exists {
				document.Warnings = append(document.Warnings, Warning{Code: "duplicate_external_id", Detail: "A duplicate API identifier was retained for later deduplication.", Anchor: anchor})
			}
			seen[identifier] = struct{}{}
		}
		title := stringValue(object[titleField])
		if title == "" {
			title = firstString(object, "tag_name", "ghsa_id", "cve_id")
		}
		if title != "" {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 2, Anchor: anchor + "/" + titleField, Text: title})
			if document.Title == "" {
				document.Title = title
			}
		}
		if metadata := stringValue(object[metadataField]); metadata != "" {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "metadata", Anchor: anchor + "/" + metadataField, Text: metadataField + ": " + metadata})
		}
		if body := stringValue(object[bodyField]); body != "" {
			bodyBlocks, warnings := extractUntrustedFragmentBlocks(body, anchor+"/"+bodyField)
			document.Warnings = append(document.Warnings, warnings...)
			document.Blocks = append(document.Blocks, bodyBlocks...)
		}
		publishedAt := parseJSONTime(firstString(object, "published_at", "created_at"))
		updatedAt := parseJSONTime(firstString(object, "updated_at"))
		if document.SourcePublishedAt.IsZero() || !publishedAt.IsZero() && publishedAt.Before(document.SourcePublishedAt) {
			document.SourcePublishedAt = publishedAt
		}
		if updatedAt.After(document.SourceUpdatedAt) {
			document.SourceUpdatedAt = updatedAt
		}
	}
	if len(document.Blocks) == 0 {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "%s response has no usable content", parserName)
	}
	return document, nil
}

func parseRegistry(sourceURL *url.URL, decoded any) (extractedDocument, error) {
	object, ok := decoded.(map[string]any)
	if !ok {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "registry response must be an object")
	}
	name := stringValue(object["name"])
	description := stringValue(object["description"])
	versions, versionsOK := object["versions"].(map[string]any)
	if name == "" && description == "" && (!versionsOK || len(versions) == 0) {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "registry response contains no package metadata")
	}
	document := extractedDocument{ParserName: "registry-json", Title: name, CanonicalURL: sourceURL.String()}
	if name != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 1, Anchor: "/name", Text: name})
	}
	if description != "" {
		descriptionBlocks, warnings := extractUntrustedFragmentBlocks(description, "/description")
		document.Blocks = append(document.Blocks, descriptionBlocks...)
		document.Warnings = append(document.Warnings, warnings...)
	}
	if tags, ok := object["dist-tags"].(map[string]any); ok {
		keys := sortedKeys(tags)
		for _, key := range keys {
			if value := stringValue(tags[key]); value != "" {
				document.Blocks = append(document.Blocks, extractedBlock{Kind: "metadata", Anchor: "/dist-tags/" + pointerToken(key), Text: key + ": " + value})
			}
		}
	}
	if versionsOK {
		keys := sortedKeys(versions)
		for _, version := range keys {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 2, Anchor: "/versions/" + pointerToken(version), Text: version})
		}
	}
	if duplicateVersions, ok := object["duplicate_versions"].([]any); ok && hasDuplicateScalars(duplicateVersions) {
		document.Warnings = append(document.Warnings, Warning{Code: "duplicate_external_id", Detail: "Duplicate package versions were retained for later deduplication.", Anchor: "/duplicate_versions"})
	}
	if times, ok := object["time"].(map[string]any); ok {
		for _, value := range times {
			if parsed := parseJSONTime(stringValue(value)); parsed.After(document.SourceUpdatedAt) {
				document.SourceUpdatedAt = parsed
			}
		}
	}
	return document, nil
}

func parseStructuredAPI(sourceURL *url.URL, decoded any) (extractedDocument, error) {
	object, ok := decoded.(map[string]any)
	if !ok {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "structured API response must be an object")
	}
	services, servicesOK := object["services"].([]any)
	message := stringValue(object["message"])
	if (!servicesOK || len(services) == 0) && message == "" {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "structured API response contains no service events")
	}
	status := stringValue(object["status"])
	document := extractedDocument{
		ParserName:      "structured-api-json",
		Title:           status,
		CanonicalURL:    sourceURL.String(),
		SourceUpdatedAt: parseJSONTime(stringValue(object["updated_at"])),
	}
	if status != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 1, Anchor: "/status", Text: status})
	}
	if message != "" {
		messageBlocks, warnings := extractUntrustedFragmentBlocks(message, "/message")
		document.Blocks = append(document.Blocks, messageBlocks...)
		document.Warnings = append(document.Warnings, warnings...)
	}
	seen := make(map[string]struct{}, len(services))
	for index, value := range services {
		service, ok := value.(map[string]any)
		if !ok {
			return extractedDocument{}, parserError(ErrorInvalidDocument, "structured API service %d must be an object", index)
		}
		anchor := fmt.Sprintf("/services/%d", index)
		identifier := stringValue(service["id"])
		if identifier != "" {
			if _, exists := seen[identifier]; exists {
				document.Warnings = append(document.Warnings, Warning{Code: "duplicate_external_id", Detail: "A duplicate service identifier was retained for later deduplication.", Anchor: anchor})
			}
			seen[identifier] = struct{}{}
		}
		name := firstString(service, "name", "id")
		if name != "" {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 2, Anchor: anchor + "/name", Text: name})
		}
		if serviceStatus := stringValue(service["status"]); serviceStatus != "" {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "metadata", Anchor: anchor + "/status", Text: "status: " + serviceStatus})
		}
	}
	return document, nil
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func pointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func parseJSONTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func hasDuplicateScalars(values []any) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		scalar := stringValue(value)
		if scalar == "" {
			continue
		}
		if _, exists := seen[scalar]; exists {
			return true
		}
		seen[scalar] = struct{}{}
	}
	return false
}
