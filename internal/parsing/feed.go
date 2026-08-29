package parsing

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/traweezy/relantern/internal/sources"
)

func parseFeed(connector sources.Connector, sourceURL *url.URL, payload []byte) (extractedDocument, error) {
	expectedType := gofeed.FeedTypeAtom
	switch connector {
	case sources.ConnectorAtom:
		expectedType = gofeed.FeedTypeAtom
	case sources.ConnectorRSS:
		expectedType = gofeed.FeedTypeRSS
	case sources.ConnectorJSONFeed:
		expectedType = gofeed.FeedTypeJSON
	}
	if detected := gofeed.DetectFeedType(bytes.NewReader(payload)); detected != expectedType {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "%s parser received a different feed format", connector)
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(payload))
	if err != nil {
		return extractedDocument{}, parserError(ErrorInvalidDocument, "parse %s feed: %v", connector, err)
	}
	if feed == nil || len(feed.Items) == 0 {
		return extractedDocument{}, parserError(ErrorEmptyDocument, "%s feed contains no items", connector)
	}

	document := extractedDocument{
		ParserName:        "gofeed-" + string(connector),
		Title:             feed.Title,
		Author:            peopleNames(feed.Authors),
		Language:          feed.Language,
		CanonicalURL:      sourceURL.String(),
		SourcePublishedAt: timeValue(feed.PublishedParsed),
		SourceUpdatedAt:   timeValue(feed.UpdatedParsed),
		Blocks:            make([]extractedBlock, 0, (len(feed.Items)*2)+1),
	}
	if canonical := sameHostURL(sourceURL, feed.Link); canonical != "" {
		document.CanonicalURL = canonical
	}
	if feed.Title != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 1, Anchor: "/feed/title", Text: feed.Title})
	}
	if description, warnings := cleanUntrustedFragment(feed.Description, "/feed/description"); description != "" {
		document.Blocks = append(document.Blocks, extractedBlock{Kind: "paragraph", Anchor: "/feed/description", Text: description})
		document.Warnings = append(document.Warnings, warnings...)
	}

	seenIdentifiers := make(map[string]struct{}, len(feed.Items))
	for index, item := range feed.Items {
		if item == nil {
			document.Warnings = append(document.Warnings, Warning{Code: "nil_feed_item", Detail: "A nil feed item was ignored."})
			continue
		}
		anchor := fmt.Sprintf("/feed/items/%d", index)
		identifier := strings.TrimSpace(item.GUID)
		if identifier == "" {
			identifier = strings.TrimSpace(item.Link)
		}
		if identifier != "" {
			if _, exists := seenIdentifiers[identifier]; exists {
				document.Warnings = append(document.Warnings, Warning{Code: "duplicate_external_id", Detail: "A duplicate feed item identifier was retained for later deduplication.", Anchor: anchor})
			}
			seenIdentifiers[identifier] = struct{}{}
		}
		if item.Title != "" {
			document.Blocks = append(document.Blocks, extractedBlock{Kind: "heading", Level: 2, Anchor: anchor + "/title", Text: item.Title})
		}
		content := item.Content
		if strings.TrimSpace(content) == "" {
			content = item.Description
		}
		contentBlocks, warnings := extractUntrustedFragmentBlocks(content, anchor+"/content")
		document.Warnings = append(document.Warnings, warnings...)
		document.Blocks = append(document.Blocks, contentBlocks...)
		if document.Author == "" {
			document.Author = peopleNames(item.Authors)
		}
		if document.SourcePublishedAt.IsZero() {
			document.SourcePublishedAt = timeValue(item.PublishedParsed)
		}
		if updated := timeValue(item.UpdatedParsed); updated.After(document.SourceUpdatedAt) {
			document.SourceUpdatedAt = updated
		}
	}
	return document, nil
}

func peopleNames(people []*gofeed.Person) string {
	names := make([]string, 0, len(people))
	for _, person := range people {
		if person != nil && strings.TrimSpace(person.Name) != "" {
			names = append(names, strings.TrimSpace(person.Name))
		}
	}
	return strings.Join(names, ", ")
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
