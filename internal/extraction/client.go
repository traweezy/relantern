package extraction

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
		return nil, errors.New("OpenAI client API key is required")
	}
	if config.Timeout <= 0 || config.Timeout > 10*time.Minute {
		return nil, errors.New("OpenAI request timeout must be positive and at most 10 minutes")
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
	service := responses.NewResponseService(options...)
	return &Client{service: service}, nil
}

func (client *Client) Extract(ctx context.Context, request ProviderRequest) (ProviderResponse, error) {
	if client == nil {
		return ProviderResponse{}, errors.New("OpenAI extraction client is nil")
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
	response, err := client.service.New(ctx, responses.ResponseNewParams{
		Model:             request.ModelID,
		Instructions:      openai.String(request.Prompt),
		Input:             responses.ResponseNewParamsInputUnion{OfString: openai.String(request.Input)},
		MaxOutputTokens:   openai.Int(int64(request.MaxOutputTokens)),
		ParallelToolCalls: openai.Bool(false),
		PromptCacheKey:    openai.String("relantern:structured-extraction:v1"),
		Reasoning:         shared.ReasoningParam{Effort: shared.ReasoningEffort(request.Reasoning)},
		Store:             openai.Bool(false),
		Text:              responses.ResponseTextConfigParam{Format: format, Verbosity: responses.ResponseTextConfigVerbosity(request.Verbosity)},
		Tools:             nil,
		Truncation:        responses.ResponseNewParamsTruncationDisabled,
	})
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("create structured OpenAI response: %w", err)
	}
	if response.Status != responses.ResponseStatusCompleted {
		return ProviderResponse{}, fmt.Errorf("OpenAI response %q ended in state %q", response.ID, response.Status)
	}
	output := response.OutputText()
	if strings.TrimSpace(output) == "" {
		return ProviderResponse{}, errors.New("OpenAI response contains no structured output text")
	}
	usage := Usage{
		InputTokens:       response.Usage.InputTokens,
		CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
		OutputTokens:      response.Usage.OutputTokens,
	}
	if usage.InputTokens < 0 || usage.CachedInputTokens < 0 ||
		usage.CachedInputTokens > usage.InputTokens || usage.OutputTokens < 0 {
		return ProviderResponse{}, errors.New("OpenAI response contains invalid usage counters")
	}
	return ProviderResponse{ID: response.ID, Output: output, Usage: usage}, nil
}

func validateProviderRequest(request ProviderRequest) error {
	if strings.TrimSpace(request.ModelID) == "" || len(request.ModelID) > 255 {
		return errors.New("structured extraction requires a bounded model ID")
	}
	if _, allowed := map[string]struct{}{"none": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}}[request.Reasoning]; !allowed {
		return fmt.Errorf("unsupported reasoning effort %q", request.Reasoning)
	}
	if request.Verbosity != "low" && request.Verbosity != "medium" && request.Verbosity != "high" {
		return fmt.Errorf("unsupported response verbosity %q", request.Verbosity)
	}
	if request.MaxOutputTokens < 256 || request.MaxOutputTokens > 128_000 {
		return errors.New("structured extraction max output tokens must be between 256 and 128000")
	}
	if request.Prompt != prompts.StructuredExtractionV1() {
		return errors.New("structured extraction requires the registered prompt version")
	}
	if !strings.HasPrefix(request.Input, "UNTRUSTED_EVIDENCE\n{") {
		return errors.New("structured extraction input must use the untrusted evidence envelope")
	}
	return nil
}

func validatedBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OpenAI base URL must be an absolute trusted origin")
	}
	hostname := strings.ToLower(parsed.Hostname())
	local := hostname == "fake-openai" || hostname == "localhost" || net.ParseIP(hostname) != nil && net.ParseIP(hostname).IsLoopback()
	official := parsed.Scheme == "https" && hostname == "api.openai.com"
	if !(local && parsed.Scheme == "http") && !official {
		return "", errors.New("OpenAI base URL is restricted to the official API or the local fake provider")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path != "" && path != "/v1" {
		return "", errors.New("OpenAI base URL path must be empty or /v1")
	}
	parsed.Path = "/v1/"
	parsed.RawPath = ""
	return parsed.String(), nil
}
