package sources

import (
	"errors"
	"net/url"
	"strings"
)

const (
	maximumGlobalAdvisoryPageURLBytes = 2048
	maximumGlobalAdvisoryCursorBytes  = 1024
	maximumGlobalAdvisoryLinkBytes    = 8192
)

var errInvalidGlobalAdvisoryLink = errors.New("invalid reviewed global advisory pagination link")

// IsGlobalAdvisoryPageURL accepts the pinned first page or one continuation
// cursor without extending the reviewed source's host, path, or filters.
func IsGlobalAdvisoryPageURL(raw string) bool {
	if raw == GlobalReviewedAdvisoriesURL {
		return true
	}
	if len(raw) > maximumGlobalAdvisoryPageURLBytes || strings.Contains(raw, "#") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.github.com" ||
		parsed.User != nil || parsed.Opaque != "" || parsed.Path != "/advisories" ||
		parsed.RawPath != "" || parsed.RawFragment != "" || parsed.ForceQuery {
		return false
	}
	// Reject encoded query names and empty query pairs before ParseQuery decodes
	// values. Otherwise several spellings can hide the same parameter.
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		key, _, found := strings.Cut(pair, "=")
		if !found || !globalAdvisoryQueryKey(key) {
			return false
		}
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query) != 5 ||
		!singleQueryValue(query, "type", "reviewed") ||
		!singleQueryValue(query, "sort", "updated") ||
		!singleQueryValue(query, "direction", "desc") ||
		!singleQueryValue(query, "per_page", "100") || len(query["after"]) != 1 {
		return false
	}
	cursor := query.Get("after")
	if len(cursor) == 0 || len(cursor) > maximumGlobalAdvisoryCursorBytes {
		return false
	}
	for index := range len(cursor) {
		if cursor[index] < '!' || cursor[index] > '~' {
			return false
		}
	}
	return true
}

func globalAdvisoryQueryKey(key string) bool {
	switch key {
	case "type", "sort", "direction", "per_page", "after":
		return true
	default:
		return false
	}
}

func singleQueryValue(query url.Values, key, want string) bool {
	values := query[key]
	return len(values) == 1 && values[0] == want
}

// GlobalAdvisoryNextPage reads only rel=next from a bounded Link header.
// Other relation targets are never fetched, and an unsafe or repeated next
// page makes the entire header invalid.
func GlobalAdvisoryNextPage(currentURL, linkHeader string) (string, error) {
	if !IsGlobalAdvisoryPageURL(currentURL) {
		return "", errInvalidGlobalAdvisoryLink
	}
	if linkHeader == "" {
		return "", nil
	}
	if len(linkHeader) > maximumGlobalAdvisoryLinkBytes {
		return "", errInvalidGlobalAdvisoryLink
	}
	links, err := splitGlobalAdvisoryLinks(linkHeader)
	if err != nil {
		return "", errInvalidGlobalAdvisoryLink
	}
	var nextURL string
	for _, link := range links {
		target, next, parseErr := globalAdvisoryLinkRelation(link)
		if parseErr != nil {
			return "", errInvalidGlobalAdvisoryLink
		}
		if !next {
			continue
		}
		if nextURL != "" || !IsGlobalAdvisoryPageURL(target) || target == GlobalReviewedAdvisoriesURL ||
			sameGlobalAdvisoryCursor(currentURL, target) {
			return "", errInvalidGlobalAdvisoryLink
		}
		nextURL = target
	}
	return nextURL, nil
}

func sameGlobalAdvisoryCursor(first, second string) bool {
	firstURL, firstErr := url.Parse(first)
	secondURL, secondErr := url.Parse(second)
	return firstErr == nil && secondErr == nil &&
		firstURL.Query().Get("after") == secondURL.Query().Get("after")
}

func splitGlobalAdvisoryLinks(header string) ([]string, error) {
	var links []string
	start := 0
	inAngle, inQuote, escaped := false, false, false
	for index := range len(header) {
		character := header[index]
		if character < ' ' && character != '\t' || character == 0x7f {
			return nil, errInvalidGlobalAdvisoryLink
		}
		if escaped {
			escaped = false
			continue
		}
		if inQuote {
			switch character {
			case '\\':
				escaped = true
			case '"':
				inQuote = false
			}
			continue
		}
		switch character {
		case '<':
			if inAngle {
				return nil, errInvalidGlobalAdvisoryLink
			}
			inAngle = true
		case '>':
			if !inAngle {
				return nil, errInvalidGlobalAdvisoryLink
			}
			inAngle = false
		case '"':
			if !inAngle {
				inQuote = true
			}
		case ',':
			if !inAngle {
				link := strings.TrimSpace(header[start:index])
				if link == "" {
					return nil, errInvalidGlobalAdvisoryLink
				}
				links = append(links, link)
				start = index + 1
			}
		}
	}
	last := strings.TrimSpace(header[start:])
	if inAngle || inQuote || escaped || last == "" {
		return nil, errInvalidGlobalAdvisoryLink
	}
	return append(links, last), nil
}

func globalAdvisoryLinkRelation(link string) (string, bool, error) {
	if !strings.HasPrefix(link, "<") {
		return "", false, errInvalidGlobalAdvisoryLink
	}
	closeIndex := strings.IndexByte(link, '>')
	if closeIndex <= 1 {
		return "", false, errInvalidGlobalAdvisoryLink
	}
	target := link[1:closeIndex]
	if strings.ContainsAny(target, "<>") {
		return "", false, errInvalidGlobalAdvisoryLink
	}
	tail := strings.TrimSpace(link[closeIndex+1:])
	seenRel, next := false, false
	for tail != "" {
		if tail[0] != ';' {
			return "", false, errInvalidGlobalAdvisoryLink
		}
		tail = strings.TrimSpace(tail[1:])
		parameter, remainder, err := nextGlobalAdvisoryLinkParameter(tail)
		if err != nil {
			return "", false, errInvalidGlobalAdvisoryLink
		}
		tail = strings.TrimSpace(remainder)
		key, value, found := strings.Cut(parameter, "=")
		key = strings.TrimSpace(key)
		if !found || !globalAdvisoryParameterToken(key) {
			return "", false, errInvalidGlobalAdvisoryLink
		}
		value, err = globalAdvisoryParameterValue(strings.TrimSpace(value))
		if err != nil {
			return "", false, errInvalidGlobalAdvisoryLink
		}
		if strings.EqualFold(key, "rel") {
			if seenRel || value == "" {
				return "", false, errInvalidGlobalAdvisoryLink
			}
			seenRel = true
			for _, relation := range strings.Fields(value) {
				if strings.EqualFold(relation, "next") {
					if next {
						return "", false, errInvalidGlobalAdvisoryLink
					}
					next = true
				}
			}
		}
	}
	return target, next, nil
}

func nextGlobalAdvisoryLinkParameter(tail string) (string, string, error) {
	inQuote, escaped := false, false
	for index := range len(tail) {
		switch character := tail[index]; {
		case escaped:
			escaped = false
		case inQuote && character == '\\':
			escaped = true
		case character == '"':
			inQuote = !inQuote
		case character == ';' && !inQuote:
			if strings.TrimSpace(tail[:index]) == "" {
				return "", "", errInvalidGlobalAdvisoryLink
			}
			return strings.TrimSpace(tail[:index]), tail[index:], nil
		}
	}
	if inQuote || escaped || strings.TrimSpace(tail) == "" {
		return "", "", errInvalidGlobalAdvisoryLink
	}
	return strings.TrimSpace(tail), "", nil
}

func globalAdvisoryParameterToken(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				if character < 'a' || character > 'z' {
					if character != '-' && character != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}

func globalAdvisoryParameterValue(value string) (string, error) {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var unquoted strings.Builder
		for index := 1; index < len(value)-1; index++ {
			if value[index] == '\\' {
				index++
				if index == len(value)-1 {
					return "", errInvalidGlobalAdvisoryLink
				}
			} else if value[index] == '"' {
				return "", errInvalidGlobalAdvisoryLink
			}
			unquoted.WriteByte(value[index])
		}
		return unquoted.String(), nil
	}
	if !globalAdvisoryParameterToken(value) {
		return "", errInvalidGlobalAdvisoryLink
	}
	return value, nil
}
