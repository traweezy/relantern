package discovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
)

type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Body    opmlBody `xml:"body"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr"`
	Type     string        `xml:"type,attr"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	HTMLURL  string        `xml:"htmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

func ParseOPML(payload []byte) ([]ImportCandidate, [sha256.Size]byte, error) {
	if len(payload) == 0 || len(payload) > MaximumOPMLBytes {
		return nil, [sha256.Size]byte{}, fmt.Errorf("%w: OPML must contain between 1 byte and %d bytes", ErrInvalid, MaximumOPMLBytes)
	}
	digest := sha256.Sum256(payload)
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	decoder.Strict = true
	decoder.CharsetReader = func(_ string, _ io.Reader) (io.Reader, error) {
		return nil, errors.New("OPML must use UTF-8")
	}
	var document opmlDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("%w: decode OPML: %v", ErrInvalid, err)
	}
	if document.XMLName.Local != "opml" {
		return nil, [sha256.Size]byte{}, fmt.Errorf("%w: document root must be opml", ErrInvalid)
	}
	flat := make([]opmlOutline, 0)
	for _, outline := range document.Body.Outlines {
		flattenOutlines(outline, &flat)
		if len(flat) > 500 {
			return nil, [sha256.Size]byte{}, fmt.Errorf("%w: OPML may contain at most 500 source outlines", ErrInvalid)
		}
	}
	if len(flat) == 0 {
		return nil, [sha256.Size]byte{}, fmt.Errorf("%w: OPML contains no source outlines", ErrInvalid)
	}

	byURL := make(map[string]ImportCandidate, len(flat))
	for _, outline := range flat {
		candidate := candidateFromOutline(outline)
		if !candidate.Valid {
			candidate.ID = candidateID(strings.TrimSpace(outline.XMLURL))
			byURL["invalid:"+candidate.ID] = candidate
			continue
		}
		if _, exists := byURL[candidate.URL]; exists {
			continue
		}
		byURL[candidate.URL] = candidate
	}
	result := make([]ImportCandidate, 0, len(byURL))
	for _, candidate := range byURL {
		result = append(result, candidate)
	}
	sort.Slice(result, func(left int, right int) bool {
		return result[left].URL < result[right].URL
	})
	return result, digest, nil
}

func flattenOutlines(outline opmlOutline, target *[]opmlOutline) {
	if strings.TrimSpace(outline.XMLURL) != "" {
		*target = append(*target, outline)
	}
	for _, nested := range outline.Outlines {
		flattenOutlines(nested, target)
	}
}

func candidateFromOutline(outline opmlOutline) ImportCandidate {
	rawURL := strings.TrimSpace(outline.XMLURL)
	name := strings.TrimSpace(outline.Title)
	if name == "" {
		name = strings.TrimSpace(outline.Text)
	}
	if name == "" {
		name = "Imported source"
	}
	if len(name) > 255 {
		name = name[:255]
	}
	canonicalURL, err := canonicalSourceURL(rawURL)
	connector, connectorErr := detectConnector(outline.Type, canonicalURL)
	candidate := ImportCandidate{
		ID: candidateID(rawURL), Name: name, URL: rawURL, Connector: connector,
	}
	if err != nil {
		candidate.Explanation = err.Error()
		return candidate
	}
	candidate.URL = canonicalURL
	candidate.ID = candidateID(canonicalURL)
	if connectorErr != nil {
		candidate.Explanation = connectorErr.Error()
		return candidate
	}
	candidate.Valid = true
	candidate.Explanation = "Ready for bounded validation; approval creates a disabled pending source."
	return candidate
}

func canonicalSourceURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("source URL must be an absolute HTTPS URL without credentials or a fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", errors.New("source URL must use the default HTTPS port")
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Hostname())
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func CanonicalCaptureURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: URL must be absolute and may not contain credentials or a fragment", ErrInvalid)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && parsed.Hostname() == "fake-source" && parsed.Port() == "8090") {
		return "", fmt.Errorf("%w: HTTPS is required outside the local fake-source fixture", ErrInvalid)
	}
	if parsed.Scheme == "https" && parsed.Port() != "" && parsed.Port() != "443" {
		return "", fmt.Errorf("%w: URL must use the default HTTPS port", ErrInvalid)
	}
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	if len(parsed.String()) > 4096 {
		return "", fmt.Errorf("%w: URL exceeds 4096 characters", ErrInvalid)
	}
	return parsed.String(), nil
}

func detectConnector(rawType string, canonicalURL string) (string, error) {
	typeName := strings.ToLower(strings.TrimSpace(rawType))
	switch typeName {
	case "rss", "rss2", "rss20":
		return "rss", nil
	case "atom":
		return "atom", nil
	case "json", "jsonfeed", "json_feed":
		return "json_feed", nil
	case "":
		parsed, _ := url.Parse(canonicalURL)
		path := strings.ToLower(parsed.Path)
		switch {
		case strings.HasSuffix(path, ".atom"):
			return "atom", nil
		case strings.HasSuffix(path, ".json"):
			return "json_feed", nil
		case strings.HasSuffix(path, ".rss") || strings.HasSuffix(path, ".xml") || strings.Contains(path, "feed"):
			return "rss", nil
		default:
			return "", errors.New("connector could not be detected from the OPML type or URL")
		}
	default:
		return "", fmt.Errorf("connector type %q is not an approved generic connector", rawType)
	}
}

func candidateID(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(digest[:12])
}
