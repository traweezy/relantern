package dedupe

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var trackingQueryParameters = map[string]struct{}{
	"dclid":     {},
	"fbclid":    {},
	"gclid":     {},
	"igshid":    {},
	"mc_cid":    {},
	"mc_eid":    {},
	"msclkid":   {},
	"ref_src":   {},
	"vero_conv": {},
	"vero_id":   {},
	"yclid":     {},
}

type HostAlias struct {
	From string
	To   string
}

func CanonicalizeURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("canonical URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return "", errors.New("canonical URL may not contain user information")
	}
	rawHostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	hostname := ""
	if address := net.ParseIP(rawHostname); address != nil {
		hostname = address.String()
	} else {
		hostname, err = idna.Lookup.ToASCII(rawHostname)
	}
	if err != nil || hostname == "" {
		return "", errors.New("canonical URL requires a valid host")
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = hostname
	if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		_, tracked := trackingQueryParameters[lower]
		if tracked || strings.HasPrefix(lower, "utm_") {
			query.Del(key)
			continue
		}
		sort.Strings(query[key])
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func ResolveDeclaredCanonical(
	fetchedURL string,
	declaredURL string,
	aliases []HostAlias,
) (string, bool, error) {
	fetched, err := CanonicalizeURL(fetchedURL)
	if err != nil {
		return "", false, fmt.Errorf("canonicalize fetched URL: %w", err)
	}
	if strings.TrimSpace(declaredURL) == "" {
		return fetched, false, nil
	}
	declared, err := CanonicalizeURL(declaredURL)
	if err != nil {
		return fetched, false, nil
	}
	fetchedParsed, _ := url.Parse(fetched)
	declaredParsed, _ := url.Parse(declared)
	if sameRegistrableDomain(fetchedParsed.Hostname(), declaredParsed.Hostname()) || aliasAllowed(fetchedParsed.Hostname(), declaredParsed.Hostname(), aliases) {
		return declared, true, nil
	}
	return fetched, false, nil
}

func sameRegistrableDomain(first string, second string) bool {
	if strings.EqualFold(first, second) {
		return true
	}
	if net.ParseIP(first) != nil || net.ParseIP(second) != nil {
		return false
	}
	firstDomain, firstError := publicsuffix.EffectiveTLDPlusOne(first)
	secondDomain, secondError := publicsuffix.EffectiveTLDPlusOne(second)
	return firstError == nil && secondError == nil && strings.EqualFold(firstDomain, secondDomain)
}

func aliasAllowed(first string, second string, aliases []HostAlias) bool {
	for _, alias := range aliases {
		if (strings.EqualFold(first, alias.From) && strings.EqualFold(second, alias.To)) ||
			(strings.EqualFold(first, alias.To) && strings.EqualFold(second, alias.From)) {
			return true
		}
	}
	return false
}
