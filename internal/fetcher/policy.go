package fetcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var domainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

var deniedPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

type FixtureTarget struct {
	Host string
	Port uint16
}

type Policy struct {
	resolver       Resolver
	allowedHosts   map[string]struct{}
	fixtureTargets map[string]map[uint16]struct{}
}

type ResolvedTarget struct {
	Host      string
	Port      uint16
	Addresses []netip.Addr
	Fixture   bool
}

type allowedHostsContextKey struct{}

func NewPolicy(resolver Resolver, allowedHosts []string, fixtureTargets []FixtureTarget) (*Policy, error) {
	if resolver == nil {
		return nil, errors.New("DNS resolver is required")
	}
	policy := &Policy{
		resolver:       resolver,
		allowedHosts:   make(map[string]struct{}, len(allowedHosts)),
		fixtureTargets: make(map[string]map[uint16]struct{}, len(fixtureTargets)),
	}
	for _, rawHost := range allowedHosts {
		host, err := normalizeHost(rawHost)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed host %q: %w", rawHost, err)
		}
		policy.allowedHosts[host] = struct{}{}
	}
	if len(policy.allowedHosts) == 0 {
		return nil, errors.New("at least one allowed host is required")
	}
	for _, target := range fixtureTargets {
		host, err := normalizeHost(target.Host)
		if err != nil {
			return nil, fmt.Errorf("invalid fixture host %q: %w", target.Host, err)
		}
		if target.Port == 0 {
			return nil, fmt.Errorf("fixture host %q requires a port", target.Host)
		}
		if policy.fixtureTargets[host] == nil {
			policy.fixtureTargets[host] = make(map[uint16]struct{})
		}
		policy.fixtureTargets[host][target.Port] = struct{}{}
	}
	return policy, nil
}

func (policy *Policy) ValidateURL(ctx context.Context, targetURL *url.URL) (ResolvedTarget, error) {
	if targetURL == nil || targetURL.Opaque != "" || targetURL.Host == "" || targetURL.User != nil || targetURL.Fragment != "" {
		return ResolvedTarget{}, newFetchError(ErrorInvalidURL, false, errors.New("URL must be an absolute origin without userinfo or fragment"))
	}
	host, err := normalizeHost(targetURL.Hostname())
	if err != nil {
		return ResolvedTarget{}, newFetchError(ErrorInvalidURL, false, err)
	}
	port, err := urlPort(targetURL)
	if err != nil {
		return ResolvedTarget{}, newFetchError(ErrorInvalidURL, false, err)
	}
	fixture := policy.isFixture(host, port)
	if targetURL.Scheme != "https" && !(targetURL.Scheme == "http" && fixture) {
		return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, errors.New("HTTPS is required outside explicit local fixtures"))
	}
	if !fixture && port != 80 && port != 443 {
		return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, fmt.Errorf("port %d is not allowed", port))
	}
	return policy.resolve(ctx, host, port, fixture)
}

func (policy *Policy) ResolveDialTarget(ctx context.Context, host string, port uint16) (ResolvedTarget, error) {
	normalized, err := normalizeHost(host)
	if err != nil {
		return ResolvedTarget{}, newFetchError(ErrorInvalidURL, false, err)
	}
	fixture := policy.isFixture(normalized, port)
	if !fixture && port != 80 && port != 443 {
		return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, fmt.Errorf("port %d is not allowed", port))
	}
	return policy.resolve(ctx, normalized, port, fixture)
}

func (policy *Policy) resolve(ctx context.Context, host string, port uint16, fixture bool) (ResolvedTarget, error) {
	if _, allowed := policy.allowedHosts[host]; !allowed {
		return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, fmt.Errorf("host %q is not in the endpoint allowlist", host))
	}
	if requestHosts, ok := ctx.Value(allowedHostsContextKey{}).(map[string]struct{}); ok {
		if _, allowed := requestHosts[host]; !allowed {
			return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, fmt.Errorf("host %q is not allowed for this endpoint", host))
		}
	}
	addresses := make([]netip.Addr, 0, 4)
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, literal.Unmap())
	} else {
		resolved, resolveErr := policy.resolver.LookupNetIP(ctx, "ip", host)
		if resolveErr != nil {
			return ResolvedTarget{}, newFetchError(ErrorDNS, true, fmt.Errorf("resolve %q: %w", host, resolveErr))
		}
		for _, address := range resolved {
			addresses = append(addresses, address.Unmap())
		}
	}
	if len(addresses) == 0 {
		return ResolvedTarget{}, newFetchError(ErrorDNS, true, fmt.Errorf("resolve %q: no addresses", host))
	}
	if !fixture {
		for _, address := range addresses {
			if deniedAddress(address) {
				return ResolvedTarget{}, newFetchError(ErrorDestinationDenied, false, fmt.Errorf("destination address %s is denied", address))
			}
		}
	}
	return ResolvedTarget{Host: host, Port: port, Addresses: addresses, Fixture: fixture}, nil
}

func withAllowedHosts(ctx context.Context, rawHosts []string) (context.Context, error) {
	hosts := make(map[string]struct{}, len(rawHosts))
	for _, rawHost := range rawHosts {
		host, err := normalizeHost(rawHost)
		if err != nil {
			return nil, fmt.Errorf("invalid endpoint host %q: %w", rawHost, err)
		}
		hosts[host] = struct{}{}
	}
	if len(hosts) == 0 {
		return nil, errors.New("endpoint requires at least one allowed host")
	}
	return context.WithValue(ctx, allowedHostsContextKey{}, hosts), nil
}

func (policy *Policy) isFixture(host string, port uint16) bool {
	_, allowed := policy.fixtureTargets[host][port]
	return allowed
}

func deniedAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func normalizeHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" || strings.HasSuffix(host, ".") {
		return "", errors.New("host must be a non-empty canonical name")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return "", errors.New("scoped IP addresses are not allowed")
		}
		return address.Unmap().String(), nil
	}
	if len(host) > 253 || !domainPattern.MatchString(host) {
		return "", errors.New("host must be an ASCII DNS name or IP literal")
	}
	return host, nil
}

func urlPort(targetURL *url.URL) (uint16, error) {
	if rawPort := targetURL.Port(); rawPort != "" {
		parsed, err := strconv.ParseUint(rawPort, 10, 16)
		if err != nil || parsed == 0 {
			return 0, fmt.Errorf("invalid URL port %q", rawPort)
		}
		return uint16(parsed), nil
	}
	switch targetURL.Scheme {
	case "https":
		return 443, nil
	case "http":
		return 80, nil
	default:
		return 0, fmt.Errorf("unsupported URL scheme %q", targetURL.Scheme)
	}
}

func mustPrefixes(raw ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(raw))
	for index, value := range raw {
		prefixes[index] = netip.MustParsePrefix(value)
	}
	return prefixes
}

func newFetchError(code ErrorCode, retryable bool, err error) *FetchError {
	return &FetchError{Code: code, Retryable: retryable, Err: err}
}

var _ Resolver = net.DefaultResolver
