package research

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/traweezy/relantern/prompts"
)

type Client struct {
	service responses.ResponseService
}

type ClientConfig struct {
	BaseURL      string
	APIKey       string
	ProjectID    string
	Organization string
	Timeout      time.Duration
}

func NewClient(config ClientConfig) (*Client, error) {
	normalizedBaseURL, err := validatedBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OpenAI research client API key is required")
	}
	if config.Timeout <= 0 || config.Timeout > 30*time.Minute {
		return nil, errors.New("OpenAI research timeout must be positive and at most 30 minutes")
	}
	options := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithBaseURL(normalizedBaseURL),
		option.WithHTTPClient(&http.Client{Timeout: config.Timeout}),
		option.WithMaxRetries(0),
	}
	if projectID := strings.TrimSpace(config.ProjectID); projectID != "" {
		options = append(options, option.WithProject(projectID))
	}
	if organization := strings.TrimSpace(config.Organization); organization != "" {
		options = append(options, option.WithOrganization(organization))
	}
	return &Client{service: responses.NewResponseService(options...)}, nil
}

func (client *Client) Start(ctx context.Context, request ProviderRequest) (ProviderResponse, error) {
	if client == nil {
		return ProviderResponse{}, errors.New("OpenAI research client is nil")
	}
	if err := validateProviderRequest(request); err != nil {
		return ProviderResponse{}, err
	}
	schema, err := StructuredOutputSchema()
	if err != nil {
		return ProviderResponse{}, err
	}
	format := responses.ResponseFormatTextConfigParamOfJSONSchema(SchemaName, schema)
	format.OfJSONSchema.Strict = openai.Bool(true)
	webSearch := responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch)
	webSearch.OfWebSearch.ExternalWebAccess = openai.Bool(true)
	webSearch.OfWebSearch.Filters.AllowedDomains = append([]string(nil), request.AllowedDomains...)
	webSearch.OfWebSearch.Filters.SetExtraFields(map[string]any{
		"blocked_domains": append([]string(nil), request.BlockedDomains...),
	})
	webSearch.OfWebSearch.SearchContextSize = responses.WebSearchToolSearchContextSizeLow
	response, err := client.service.New(ctx, responses.ResponseNewParams{
		Background:        openai.Bool(request.Background),
		Instructions:      openai.String(request.Prompt),
		Input:             responses.ResponseNewParamsInputUnion{OfString: openai.String(request.Input)},
		Include:           []responses.ResponseIncludable{responses.ResponseIncludableWebSearchCallActionSources},
		MaxOutputTokens:   openai.Int(int64(request.MaxOutputTokens)),
		MaxToolCalls:      openai.Int(int64(request.MaxToolCalls)),
		Model:             request.ModelID,
		ParallelToolCalls: openai.Bool(false),
		PromptCacheKey:    openai.String("relantern:research-synthesis:v1"),
		Reasoning:         shared.ReasoningParam{Effort: shared.ReasoningEffort(request.Reasoning)},
		Store:             openai.Bool(true),
		Text: responses.ResponseTextConfigParam{
			Format:    format,
			Verbosity: responses.ResponseTextConfigVerbosity(request.Verbosity),
		},
		Tools:      []responses.ToolUnionParam{webSearch},
		Truncation: responses.ResponseNewParamsTruncationDisabled,
	})
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("create background OpenAI research response: %w", err)
	}
	return decodeProviderResponse(response)
}

func (client *Client) Get(ctx context.Context, responseID string) (ProviderResponse, error) {
	if client == nil {
		return ProviderResponse{}, errors.New("OpenAI research client is nil")
	}
	if strings.TrimSpace(responseID) == "" || len(responseID) > 255 {
		return ProviderResponse{}, errors.New("OpenAI research response ID is required and bounded")
	}
	response, err := client.service.Get(ctx, responseID, responses.ResponseGetParams{
		Include: []responses.ResponseIncludable{responses.ResponseIncludableWebSearchCallActionSources},
	})
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("retrieve background OpenAI research response: %w", err)
	}
	return decodeProviderResponse(response)
}

func validateProviderRequest(request ProviderRequest) error {
	if strings.TrimSpace(request.ModelID) == "" || len(request.ModelID) > 255 {
		return errors.New("research requires a bounded model ID")
	}
	if !allowedValue(request.Reasoning, "none", "low", "medium", "high", "xhigh", "max") {
		return fmt.Errorf("unsupported research reasoning effort %q", request.Reasoning)
	}
	if !allowedValue(request.Verbosity, "low", "medium", "high") {
		return fmt.Errorf("unsupported research verbosity %q", request.Verbosity)
	}
	if request.MaxOutputTokens < 256 || request.MaxOutputTokens > 128_000 {
		return errors.New("research max output tokens must be between 256 and 128000")
	}
	if request.MaxToolCalls < 1 || request.MaxToolCalls > 10 {
		return errors.New("research max tool calls must be between 1 and 10")
	}
	if request.Prompt != prompts.ResearchSynthesisV1() {
		return errors.New("research requires the registered prompt version")
	}
	if !strings.HasPrefix(request.Input, "VALIDATED_FACTS\n{") || len(request.Input) > MaximumInputBytes {
		return errors.New("research input must use the bounded validated-facts envelope")
	}
	if !request.Background {
		return errors.New("research must use background mode")
	}
	if err := validateDomainFilters(request.AllowedDomains, request.BlockedDomains); err != nil {
		return err
	}
	return nil
}

func decodeProviderResponse(response *responses.Response) (ProviderResponse, error) {
	if response == nil || strings.TrimSpace(response.ID) == "" || len(response.ID) > 255 {
		return ProviderResponse{}, errors.New("OpenAI returned a missing or invalid research response ID")
	}
	status := string(response.Status)
	if !allowedValue(status, "queued", "in_progress", "completed", "failed", "incomplete", "cancelled") {
		return ProviderResponse{}, fmt.Errorf("OpenAI research response %q has unknown state %q", response.ID, status)
	}
	result := ProviderResponse{ID: response.ID, Status: status}
	if status == "queued" || status == "in_progress" {
		return result, nil
	}
	result.Usage = Usage{
		InputTokens:       response.Usage.InputTokens,
		CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
		OutputTokens:      response.Usage.OutputTokens,
	}
	if result.Usage.InputTokens < 0 || result.Usage.CachedInputTokens < 0 ||
		result.Usage.CachedInputTokens > result.Usage.InputTokens || result.Usage.OutputTokens < 0 {
		return ProviderResponse{}, errors.New("OpenAI research response contains invalid usage counters")
	}
	sources := make(map[string]Source)
	for _, item := range response.Output {
		switch item.Type {
		case "message", "reasoning":
		case "web_search_call":
			result.Usage.ToolCalls++
			call := item.AsWebSearchCall()
			if call.Status == responses.ResponseFunctionWebSearchStatusFailed {
				return ProviderResponse{}, errors.New("OpenAI web search tool call failed")
			}
			for _, rawSource := range call.Action.Sources {
				parsed, err := url.Parse(rawSource.URL)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
					return ProviderResponse{}, fmt.Errorf("OpenAI returned invalid web source %q", rawSource.URL)
				}
				domain := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
				sources[rawSource.URL] = Source{URL: rawSource.URL, Domain: domain}
			}
		default:
			return ProviderResponse{}, fmt.Errorf("OpenAI research response used forbidden output tool type %q", item.Type)
		}
	}
	if result.Usage.ToolCalls > 10 {
		return ProviderResponse{}, errors.New("OpenAI research response exceeded the absolute tool-call bound")
	}
	result.Sources = make([]Source, 0, len(sources))
	for _, source := range sources {
		result.Sources = append(result.Sources, source)
	}
	sort.Slice(result.Sources, func(left int, right int) bool { return result.Sources[left].URL < result.Sources[right].URL })
	if status == "completed" {
		result.Output = response.OutputText()
		if strings.TrimSpace(result.Output) == "" {
			return ProviderResponse{}, errors.New("completed OpenAI research response contains no structured output")
		}
	}
	return result, nil
}

func validateDomainFilters(allowed []string, blocked []string) error {
	if len(allowed) == 0 || len(allowed) > 100 || len(blocked) > 100 {
		return errors.New("research requires 1 through 100 allowed domains and at most 100 blocked domains")
	}
	for name, values := range map[string][]string{"allowed": allowed, "blocked": blocked} {
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			normalized := strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
			if normalized == "" || len(normalized) > 253 || strings.ContainsAny(normalized, "/:@?#") || net.ParseIP(normalized) != nil {
				return fmt.Errorf("research %s domain %q is invalid", name, value)
			}
			if _, duplicate := seen[normalized]; duplicate {
				return fmt.Errorf("research %s domain %q is duplicated", name, normalized)
			}
			seen[normalized] = struct{}{}
		}
	}
	return nil
}

func validatedBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OpenAI research base URL must be an absolute trusted origin")
	}
	hostname := strings.ToLower(parsed.Hostname())
	parsedIP := net.ParseIP(hostname)
	local := hostname == "fake-openai" || hostname == "localhost" || parsedIP != nil && parsedIP.IsLoopback()
	official := parsed.Scheme == "https" && hostname == "api.openai.com"
	if !(local && parsed.Scheme == "http") && !official {
		return "", errors.New("OpenAI research base URL is restricted to the official API or local fake provider")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path != "" && path != "/v1" {
		return "", errors.New("OpenAI research base URL path must be empty or /v1")
	}
	parsed.Path = "/v1/"
	parsed.RawPath = ""
	return parsed.String(), nil
}
