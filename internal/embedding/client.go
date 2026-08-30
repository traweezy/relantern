package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maximumResponseBytes = 1 << 20

type Client struct {
	endpoint   *url.URL
	httpClient *http.Client
	apiKey     string
	projectID  string
	orgID      string
	modelID    string
	dimensions int
}

type ClientConfig struct {
	BaseURL      string
	APIKey       string
	ProjectID    string
	Organization string
	ModelID      string
	Dimensions   int
	Timeout      time.Duration
	Hosted       bool
	HTTPClient   *http.Client
}

type embedRequest struct {
	Input      string `json:"input"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

type embedResponse struct {
	Data []struct {
		Embedding Vector `json:"embedding"`
		Index     int    `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
}

func NewClient(config ClientConfig) (*Client, error) {
	parsedURL, err := embeddingBaseURL(config.BaseURL, config.Hosted)
	if err != nil {
		return nil, err
	}
	if config.Hosted && strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("hosted embedding requests require an OpenAI API key")
	}
	if strings.TrimSpace(config.ModelID) == "" || len(config.ModelID) > 255 {
		return nil, errors.New("embedding model ID must contain between 1 and 255 characters")
	}
	if config.Dimensions < 1 || config.Dimensions > 4096 {
		return nil, errors.New("embedding dimensions must be between 1 and 4096")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("embedding request timeout must be positive")
	}
	parsedURL.Path = "/v1/embeddings"
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	boundedHTTPClient := *httpClient
	boundedHTTPClient.Timeout = config.Timeout
	return &Client{
		endpoint:   parsedURL,
		httpClient: &boundedHTTPClient,
		apiKey:     strings.TrimSpace(config.APIKey),
		projectID:  strings.TrimSpace(config.ProjectID),
		orgID:      strings.TrimSpace(config.Organization),
		modelID:    config.ModelID,
		dimensions: config.Dimensions,
	}, nil
}

func embeddingBaseURL(raw string, hosted bool) (*url.URL, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, errors.New("embedding base URL must be an absolute trusted origin")
	}
	path := strings.TrimSuffix(parsedURL.EscapedPath(), "/")
	if path != "" && path != "/v1" {
		return nil, errors.New("embedding base URL path must be empty or /v1")
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hosted {
		if parsedURL.Scheme != "https" || hostname != "api.openai.com" {
			return nil, errors.New("hosted embedding requests are restricted to https://api.openai.com")
		}
		return parsedURL, nil
	}
	if parsedURL.Scheme != "http" ||
		(hostname != "fake-openai" && hostname != "127.0.0.1" && hostname != "localhost") {
		return nil, errors.New("local embedding requests are restricted to fake-openai or loopback HTTP")
	}
	return parsedURL, nil
}

func (client *Client) Embed(ctx context.Context, input string) (Vector, error) {
	if err := ValidateInput(input); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(embedRequest{
		Input:      strings.TrimSpace(input),
		Model:      client.modelID,
		Dimensions: client.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if client.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+client.apiKey)
	}
	if client.projectID != "" {
		request.Header.Set("OpenAI-Project", client.projectID)
	}
	if client.orgID != "" {
		request.Header.Set("OpenAI-Organization", client.orgID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request embedding provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
		return nil, fmt.Errorf("embedding provider returned HTTP %d", response.StatusCode)
	}
	var decoded embedResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes))
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if decoded.Model != client.modelID || len(decoded.Data) != 1 || decoded.Data[0].Index != 0 {
		return nil, errors.New("fake embedding response does not match the requested model and input")
	}
	if err := ValidateVector(decoded.Data[0].Embedding, client.dimensions); err != nil {
		return nil, fmt.Errorf("validate embedding response: %w", err)
	}
	return append(Vector(nil), decoded.Data[0].Embedding...), nil
}
