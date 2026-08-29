package parsing

import (
	"bytes"
	"errors"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
)

func parsePage(sourceURL *url.URL, payload []byte) (extractedDocument, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "HTML page is empty")
	}
	parser := readability.NewParser()
	parser.MaxElemsToParse = 100_000
	parser.CharThresholds = 20
	parser.DisableJSONLD = true
	article, err := parser.Parse(bytes.NewReader(payload), sourceURL)
	if err != nil {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "extract readable HTML: %v", err)
	}
	if article.Node == nil {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "HTML page has no readable content")
	}
	sanitizeTree(article.Node)
	blocks := extractHTMLBlocks(article.Node, "/article")
	if len(blocks) == 0 {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "HTML page has no readable blocks")
	}
	document := extractedDocument{
		ParserName:   "readeck-readability",
		Title:        article.Title(),
		Author:       article.Byline(),
		Language:     article.Language(),
		CanonicalURL: canonicalPageURL(sourceURL, payload),
		Blocks:       blocks,
		Warnings:     duplicatePageWarnings(payload),
	}
	if publishedAt, publishedError := article.PublishedTime(); publishedError == nil {
		document.SourcePublishedAt = publishedAt.UTC()
	} else if !errors.Is(publishedError, readability.ErrTimestampMissing) {
		document.Warnings = append(document.Warnings, Warning{Code: "invalid_published_time", Detail: "Publisher metadata contained an invalid publication timestamp."})
	}
	if updatedAt, updatedError := article.ModifiedTime(); updatedError == nil {
		document.SourceUpdatedAt = updatedAt.UTC()
	} else if !errors.Is(updatedError, readability.ErrTimestampMissing) {
		document.Warnings = append(document.Warnings, Warning{Code: "invalid_updated_time", Detail: "Publisher metadata contained an invalid modification timestamp."})
	}
	return document, nil
}

func duplicatePageWarnings(payload []byte) []Warning {
	document, err := html.Parse(bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	for node := range document.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		identifier := strings.TrimSpace(nodeAttribute(node, "data-source-id"))
		if identifier == "" {
			continue
		}
		if _, exists := seen[identifier]; exists {
			return []Warning{{Code: "duplicate_external_id", Detail: "A duplicate page source identifier was retained for later deduplication."}}
		}
		seen[identifier] = struct{}{}
	}
	return nil
}

func canonicalPageURL(sourceURL *url.URL, payload []byte) string {
	document, err := html.Parse(bytes.NewReader(payload))
	if err != nil {
		return sourceURL.String()
	}
	for node := range document.Descendants() {
		if node.Type != html.ElementNode || node.Data != "link" {
			continue
		}
		if !containsToken(nodeAttribute(node, "rel"), "canonical") {
			continue
		}
		if canonical := sameHostURL(sourceURL, nodeAttribute(node, "href")); canonical != "" {
			return canonical
		}
	}
	return sourceURL.String()
}

func containsToken(value string, expected string) bool {
	for _, token := range strings.Fields(value) {
		if strings.EqualFold(token, expected) {
			return true
		}
	}
	return false
}
