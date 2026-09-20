package ingestion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/fetcher"
	"github.com/traweezy/relantern/internal/storage"
)

const userAgent = "Relantern/1.0 (+https://github.com/traweezy/relantern)"
const robotsLimit = int64(256 << 10)

var errRobotsDenied = errors.New("robots policy disallows source endpoint")

type NetworkFetcher struct {
	objects            storage.RawStore
	limiter            fetcher.RequestLimiter
	clock              clock.Clock
	allowLocalFixtures bool
}

func NewNetworkFetcher(objects storage.RawStore, limiter fetcher.RequestLimiter, configuredClock clock.Clock, allowLocalFixtures bool) (*NetworkFetcher, error) {
	if objects == nil || limiter == nil || configuredClock == nil {
		return nil, errors.New("source network fetcher requires storage, limiter, and clock")
	}
	return &NetworkFetcher{objects: objects, limiter: limiter, clock: configuredClock, allowLocalFixtures: allowLocalFixtures}, nil
}

func (network *NetworkFetcher) Fetch(ctx context.Context, endpoint Endpoint) (fetcher.Result, error) {
	target, err := url.Parse(endpoint.URL)
	if err != nil || target.Hostname() == "" {
		return network.failure(fetcher.ErrorInvalidURL, false, fmt.Errorf("invalid source endpoint URL"))
	}
	var fixtures []fetcher.FixtureTarget
	if target.Scheme == "http" {
		if !network.allowLocalFixtures || target.Hostname() != "fake-source" || target.Port() != "8090" {
			return network.failure(fetcher.ErrorDestinationDenied, false, errors.New("HTTP source endpoint is not an approved fixture"))
		}
		fixtures = []fetcher.FixtureTarget{{Host: "fake-source", Port: 8090}}
	} else if target.Scheme != "https" {
		return network.failure(fetcher.ErrorInvalidURL, false, errors.New("source endpoint must use HTTPS"))
	}
	policy, err := fetcher.NewPolicy(net.DefaultResolver, []string{target.Hostname()}, fixtures)
	if err != nil {
		return network.failure(fetcher.ErrorInvalidURL, false, err)
	}
	if _, err := policy.ValidateURL(ctx, target); err != nil {
		return network.failure(fetcher.ErrorDestinationDenied, false, err)
	}
	client, err := fetcher.NewSecureHTTPClient(policy, nil, fetcher.DefaultNetworkLimits(), 3)
	if err != nil {
		return network.failure(fetcher.ErrorTransport, false, err)
	}
	if err := checkRobots(ctx, client, target); err != nil {
		return network.failure(fetcher.ErrorDestinationDenied, !errors.Is(err, errRobotsDenied), err)
	}
	configuration := fetcher.DefaultConfig(userAgent)
	configuration.RetryAttempts = 2
	configuration.Now = network.clock.Now
	configuredFetcher, err := fetcher.New(configuration, policy, client, network.limiter, network.objects)
	if err != nil {
		return network.failure(fetcher.ErrorTransport, false, err)
	}
	result, fetchErr := configuredFetcher.Fetch(ctx, fetcher.Endpoint{
		ID: endpoint.RegistryID, SourceID: endpoint.SourceID, URL: endpoint.URL,
		AllowedHosts:         []string{target.Hostname()},
		ExpectedContentTypes: endpoint.ExpectedContentTypes,
		MaxBodyBytes:         endpoint.MaxResponseBytes,
		ContentPolicy:        endpoint.ContentPolicy,
	}, endpoint.Checkpoint)
	if len(result.Attempts) == 0 {
		var typed *fetcher.FetchError
		if errors.As(fetchErr, &typed) {
			return network.failure(typed.Code, typed.Retryable, fetchErr)
		}
		if fetchErr != nil {
			return network.failure(fetcher.ErrorTransport, true, fetchErr)
		}
		return network.failure(fetcher.ErrorTransport, false, errors.New("fetch returned no attempts"))
	}
	if result.Outcome == fetcher.OutcomeNotModified && endpoint.Checkpoint.ETag == "" && endpoint.Checkpoint.LastModified == "" {
		result.Outcome = fetcher.OutcomeFailed
		result.Attempts[len(result.Attempts)-1].ErrorCode = fetcher.ErrorUnexpectedStatus
		return result, &fetcher.FetchError{
			Code: fetcher.ErrorUnexpectedStatus, Retryable: true,
			Err: errors.New("source returned 304 without a conditional checkpoint"),
		}
	}
	return result, fetchErr
}

func (network *NetworkFetcher) failure(code fetcher.ErrorCode, retryable bool, err error) (fetcher.Result, error) {
	now := network.clock.Now().UTC()
	return fetcher.Result{
		Outcome:  fetcher.OutcomeFailed,
		Attempts: []fetcher.Attempt{{AttemptedAt: now, CompletedAt: now, ErrorCode: code}},
	}, &fetcher.FetchError{Code: code, Retryable: retryable, Err: err}
}

func checkRobots(ctx context.Context, client *http.Client, target *url.URL) error {
	robotsURL := *target
	robotsURL.Path = "/robots.txt"
	robotsURL.RawPath = ""
	robotsURL.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create robots request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/plain")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request robots policy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return errRobotsDenied
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("robots policy returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, robotsLimit+1))
	if err != nil {
		return fmt.Errorf("read robots policy: %w", err)
	}
	if int64(len(payload)) > robotsLimit {
		return errors.New("robots policy exceeds 256 KiB")
	}
	path := target.EscapedPath()
	if target.RawQuery != "" {
		path += "?" + target.RawQuery
	}
	if robotsDisallows(string(payload), path) {
		return errRobotsDenied
	}
	return nil
}

func robotsDisallows(document string, path string) bool {
	type robotRule struct {
		pattern string
		allow   bool
	}
	type robotGroup struct {
		agents []string
		rules  []robotRule
	}
	groups := make([]robotGroup, 0)
	current := robotGroup{}
	hasRules := false
	for _, line := range strings.Split(document, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "user-agent":
			if hasRules {
				groups = append(groups, current)
				current = robotGroup{}
				hasRules = false
			}
			current.agents = append(current.agents, strings.ToLower(value))
		case "allow", "disallow":
			if len(current.agents) == 0 || value == "" {
				continue
			}
			current.rules = append(current.rules, robotRule{pattern: value, allow: strings.EqualFold(strings.TrimSpace(name), "allow")})
			hasRules = true
		}
	}
	if len(current.agents) > 0 {
		groups = append(groups, current)
	}
	bestAgentLength := -1
	var matchedRules []robotRule
	for _, group := range groups {
		groupLength := -1
		for _, agent := range group.agents {
			if agent == "*" && groupLength < 0 {
				groupLength = 0
			}
			if agent != "" && agent != "*" && strings.HasPrefix(strings.ToLower(userAgent), agent) && len(agent) > groupLength {
				groupLength = len(agent)
			}
		}
		if groupLength > bestAgentLength {
			bestAgentLength = groupLength
			matchedRules = group.rules
		} else if groupLength == bestAgentLength && groupLength >= 0 {
			matchedRules = append(matchedRules, group.rules...)
		}
	}
	bestRuleLength := -1
	allowed := true
	for _, rule := range matchedRules {
		if !robotPatternMatches(rule.pattern, path) {
			continue
		}
		length := len(strings.TrimSuffix(strings.ReplaceAll(rule.pattern, "*", ""), "$"))
		if length > bestRuleLength || (length == bestRuleLength && rule.allow) {
			bestRuleLength = length
			allowed = rule.allow
		}
	}
	return !allowed
}

func robotPatternMatches(pattern string, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*")
	if anchored {
		expression += "$"
	}
	compiled, err := regexp.Compile(expression)
	return err == nil && compiled.MatchString(path)
}
