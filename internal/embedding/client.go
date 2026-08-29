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
	modelID    string
	dimensions int
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

func NewClient(baseURL string, modelID string, dimensions int, timeout time.Duration) (*Client, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsedURL.Scheme != "http" || parsedURL.Host == "" || parsedURL.User != nil {
		return nil, errors.New("fake embedding base URL must be an absolute HTTP origin")
	}
	hostname := parsedURL.Hostname()
	if hostname != "fake-openai" && hostname != "127.0.0.1" && hostname != "localhost" {
		return nil, errors.New("embedding requests are restricted to the local fake-openai service")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, errors.New("fake embedding base URL may not contain a query or fragment")
	}
	if strings.TrimSpace(modelID) == "" || len(modelID) > 255 {
		return nil, errors.New("embedding model ID must contain between 1 and 255 characters")
	}
	if dimensions < 1 || dimensions > 4096 {
		return nil, errors.New("embedding dimensions must be between 1 and 4096")
	}
	if timeout <= 0 {
		return nil, errors.New("embedding request timeout must be positive")
	}
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, "/") + "/v1/embeddings"
	return &Client{
		endpoint:   parsedURL,
		httpClient: &http.Client{Timeout: timeout},
		modelID:    modelID,
		dimensions: dimensions,
	}, nil
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
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request fake embedding: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
		return nil, fmt.Errorf("fake embedding returned HTTP %d", response.StatusCode)
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
