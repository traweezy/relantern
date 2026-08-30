package fakeprovider

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/httpx"
	"github.com/traweezy/relantern/internal/research"
	"github.com/traweezy/relantern/prompts"
)

var fakeVersionPattern = regexp.MustCompile(`\bv?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][a-z0-9.-]+)?\b`)
var fakeDatePattern = regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b`)
var fakeCVEPattern = regexp.MustCompile(`(?i)\bCVE-[0-9]{4}-[0-9]{4,7}\b`)

type Kind string

const (
	KindDelivery Kind = "delivery"
	KindOpenAI   Kind = "openai"
	KindSource   Kind = "source"
)

type captureStore struct {
	mu       sync.RWMutex
	keys     map[string]struct{}
	captures []Capture
}

type fakeResponseRequest struct {
	Background        bool     `json:"background"`
	Include           []string `json:"include"`
	Model             string   `json:"model"`
	Instructions      string   `json:"instructions"`
	Input             string   `json:"input"`
	MaxOutputTokens   int      `json:"max_output_tokens"`
	MaxToolCalls      int      `json:"max_tool_calls"`
	ParallelToolCalls bool     `json:"parallel_tool_calls"`
	PromptCacheKey    string   `json:"prompt_cache_key"`
	Reasoning         struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
	Store bool `json:"store"`
	Text  struct {
		Verbosity string `json:"verbosity"`
		Format    struct {
			Name   string         `json:"name"`
			Schema map[string]any `json:"schema"`
			Strict bool           `json:"strict"`
			Type   string         `json:"type"`
		} `json:"format"`
	} `json:"text"`
	Tools      []json.RawMessage `json:"tools"`
	Truncation string            `json:"truncation"`
}

func New(kind Kind, logger *slog.Logger) (http.Handler, error) {
	return NewWithOptions(kind, logger, Options{})
}

type Options struct {
	LocalOAuth *LocalOAuthOptions
}

func NewWithOptions(kind Kind, logger *slog.Logger, options Options) (http.Handler, error) {
	if kind != KindDelivery && kind != KindOpenAI && kind != KindSource {
		return nil, fmt.Errorf("unsupported fake provider kind %q", kind)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(response, http.StatusOK, map[string]string{"provider": string(kind), "status": "ok"})
	})

	switch kind {
	case KindDelivery:
		registerDelivery(mux)
	case KindOpenAI:
		registerOpenAI(mux)
	case KindSource:
		registerSource(mux)
		if options.LocalOAuth != nil {
			if err := registerLocalOAuth(mux, *options.LocalOAuth); err != nil {
				return nil, err
			}
		}
	}

	return httpx.SecurityHeaders(httpx.Recovery(logger)(httpx.AccessLog(logger)(mux))), nil
}

func registerDelivery(mux *http.ServeMux) {
	store := &captureStore{keys: make(map[string]struct{})}
	mux.HandleFunc("POST /capture", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload Capture
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload.IdempotencyKey == "" || payload.LocalDate == "" || payload.Message == "" || payload.ScheduledFor.IsZero() {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid capture", "The capture body must be valid JSON.")
			return
		}
		store.mu.Lock()
		if _, exists := store.keys[payload.IdempotencyKey]; !exists {
			store.keys[payload.IdempotencyKey] = struct{}{}
			store.captures = append(store.captures, payload)
		}
		store.mu.Unlock()
		httpx.WriteJSON(response, http.StatusAccepted, map[string]string{"status": "captured"})
	})
	mux.HandleFunc("GET /captures", func(response http.ResponseWriter, _ *http.Request) {
		store.mu.RLock()
		captures := append([]Capture{}, store.captures...)
		store.mu.RUnlock()
		httpx.WriteJSON(response, http.StatusOK, map[string]any{"count": len(captures), "captures": captures})
	})
}

func registerOpenAI(mux *http.ServeMux) {
	backgroundResponses := struct {
		sync.RWMutex
		values map[string]map[string]any
	}{values: make(map[string]map[string]any)}
	mux.HandleFunc("POST /v1/embeddings", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload struct {
			Input      string `json:"input"`
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 128<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil ||
			strings.TrimSpace(payload.Input) == "" ||
			strings.TrimSpace(payload.Model) == "" ||
			payload.Dimensions < 1 ||
			payload.Dimensions > 4096 {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid embedding request", "The embedding body must contain bounded input, model, and dimensions.")
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]any{
			"object": "list",
			"model":  payload.Model,
			"data": []any{
				map[string]any{
					"object":    "embedding",
					"index":     0,
					"embedding": deterministicEmbedding(payload.Input, payload.Dimensions),
				},
			},
			"usage": map[string]int{"prompt_tokens": len(strings.Fields(payload.Input)), "total_tokens": len(strings.Fields(payload.Input))},
		})
	})
	mux.HandleFunc("POST /v1/responses", func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload fakeResponseRequest
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 512<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid request", "The fake Responses API request is invalid.")
			return
		}
		if payload.Instructions == prompts.ResearchSynthesisV1() {
			queued, completed, err := deterministicResearchResponse(payload)
			if err != nil {
				httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid research request", err.Error())
				return
			}
			responseID, _ := queued["id"].(string)
			backgroundResponses.Lock()
			backgroundResponses.values[responseID] = completed
			backgroundResponses.Unlock()
			httpx.WriteJSON(response, http.StatusOK, queued)
			return
		}
		expectedSchema, schemaError := extraction.StructuredOutputSchema()
		if schemaError != nil {
			httpx.WriteProblem(response, request, http.StatusInternalServerError, "Fake provider failure", "The registered schema could not be loaded.")
			return
		}
		if strings.TrimSpace(payload.Model) == "" ||
			payload.Instructions != prompts.StructuredExtractionV1() ||
			!strings.HasPrefix(payload.Input, "UNTRUSTED_EVIDENCE\n") ||
			payload.MaxOutputTokens < 256 ||
			payload.PromptCacheKey != "relantern:structured-extraction:v1" ||
			!fakeReasoningAllowed(payload.Reasoning.Effort) ||
			!fakeVerbosityAllowed(payload.Text.Verbosity) ||
			payload.ParallelToolCalls ||
			payload.Store ||
			len(payload.Tools) != 0 ||
			payload.Text.Format.Type != "json_schema" ||
			payload.Text.Format.Name != extraction.SchemaName ||
			!payload.Text.Format.Strict ||
			!reflect.DeepEqual(payload.Text.Format.Schema, expectedSchema) ||
			payload.Truncation != "disabled" {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid request", "The fake Responses API requires a bounded, tool-free strict structured-output request.")
			return
		}
		output, err := deterministicExtraction(payload.Input)
		if err != nil {
			httpx.WriteProblem(response, request, http.StatusBadRequest, "Invalid evidence", err.Error())
			return
		}
		structuredOutput, err := json.Marshal(output)
		if err != nil {
			httpx.WriteProblem(response, request, http.StatusInternalServerError, "Fake provider failure", "The deterministic output could not be encoded.")
			return
		}
		responseDigest := sha256.Sum256([]byte(payload.Input))
		inputTokens := (len(payload.Instructions) + len(payload.Input) + 3) / 4
		outputTokens := (len(structuredOutput) + 3) / 4
		httpx.WriteJSON(response, http.StatusOK, map[string]any{
			"id":     fmt.Sprintf("resp_fake_%x", responseDigest[:8]),
			"model":  payload.Model,
			"object": "response",
			"status": "completed",
			"output": []any{
				map[string]any{
					"id":     "msg_fake_extraction",
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"content": []any{
						map[string]any{
							"type":        "output_text",
							"text":        string(structuredOutput),
							"annotations": []any{},
						},
					},
				},
			},
			"tools": []any{},
			"usage": map[string]any{
				"input_tokens": inputTokens,
				"input_tokens_details": map[string]int{
					"cache_write_tokens": 0,
					"cached_tokens":      0,
				},
				"output_tokens": outputTokens,
				"output_tokens_details": map[string]int{
					"reasoning_tokens": 0,
				},
				"total_tokens": inputTokens + outputTokens,
			},
		})
	})
	mux.HandleFunc("GET /v1/responses/{responseID}", func(response http.ResponseWriter, request *http.Request) {
		responseID := request.PathValue("responseID")
		backgroundResponses.RLock()
		stored, exists := backgroundResponses.values[responseID]
		backgroundResponses.RUnlock()
		if !exists {
			httpx.WriteProblem(response, request, http.StatusNotFound, "Not Found", "The fake background response does not exist.")
			return
		}
		httpx.WriteJSON(response, http.StatusOK, stored)
	})
}

func deterministicResearchResponse(payload fakeResponseRequest) (map[string]any, map[string]any, error) {
	expectedSchema, err := research.StructuredOutputSchema()
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(payload.Model) == "" || !strings.HasPrefix(payload.Input, "VALIDATED_FACTS\n") ||
		payload.MaxOutputTokens < 256 || payload.MaxToolCalls < 1 || payload.MaxToolCalls > 10 ||
		payload.PromptCacheKey != "relantern:research-synthesis:v1" ||
		!fakeReasoningAllowed(payload.Reasoning.Effort) || !fakeVerbosityAllowed(payload.Text.Verbosity) ||
		payload.ParallelToolCalls || !payload.Store || !payload.Background || len(payload.Tools) != 1 ||
		payload.Truncation != "disabled" {
		return nil, nil, errors.New("fake research requires background mode, one bounded web-search tool, sources, and strict structured output")
	}
	if payload.Text.Format.Type != "json_schema" || payload.Text.Format.Name != research.SchemaName ||
		!payload.Text.Format.Strict || !reflect.DeepEqual(payload.Text.Format.Schema, expectedSchema) {
		return nil, nil, errors.New("fake research requires the registered strict JSON schema")
	}
	if len(payload.Include) != 1 || payload.Include[0] != "web_search_call.action.sources" {
		return nil, nil, errors.New("fake research requires the complete returned web-search source list")
	}
	var tool struct {
		Type              string `json:"type"`
		ExternalWebAccess bool   `json:"external_web_access"`
		SearchContextSize string `json:"search_context_size"`
		Filters           struct {
			AllowedDomains []string `json:"allowed_domains"`
			BlockedDomains []string `json:"blocked_domains"`
		} `json:"filters"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload.Tools[0])))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tool); err != nil || tool.Type != "web_search" || !tool.ExternalWebAccess ||
		tool.SearchContextSize != "low" || len(tool.Filters.AllowedDomains) == 0 || len(tool.Filters.BlockedDomains) == 0 {
		return nil, nil, errors.New("fake research web-search policy is incomplete")
	}
	var facts struct {
		ClusterID string               `json:"cluster_id"`
		Title     string               `json:"title"`
		Claims    []research.ClaimFact `json:"validated_claims"`
	}
	factsDecoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(payload.Input, "VALIDATED_FACTS\n")))
	factsDecoder.DisallowUnknownFields()
	if err := factsDecoder.Decode(&facts); err != nil || facts.ClusterID == "" || facts.Title == "" || len(facts.Claims) == 0 {
		return nil, nil, errors.New("fake research input must contain validated claims")
	}
	sourceURL := "https://go.dev/doc/devel/release"
	if !containsDomain(tool.Filters.AllowedDomains, "go.dev") {
		sourceURL = "https://github.com/openai/openai-go"
	}
	output := research.Output{
		Headline:          facts.Title,
		Summary:           facts.Claims[0].ClaimText,
		WhyItMatters:      "The evidence-verified change may affect the owner's watched development stack.",
		RecommendedAction: "Review the cited primary evidence before choosing whether to change the stack.",
		Confidence:        facts.Claims[0].Confidence,
		Assertions: []research.Assertion{{
			Text: facts.Claims[0].ClaimText, Material: facts.Claims[0].Material,
			ClaimIDs: []string{facts.Claims[0].ID}, SourceURLs: []string{sourceURL},
		}},
		Uncertainties: []string{},
	}
	structuredOutput, err := json.Marshal(output)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256([]byte(payload.Input))
	responseID := fmt.Sprintf("resp_fake_research_%x", digest[:8])
	inputTokens := (len(payload.Instructions) + len(payload.Input) + 3) / 4
	outputTokens := (len(structuredOutput) + 3) / 4
	queued := map[string]any{
		"id": responseID, "model": payload.Model, "object": "response", "status": "queued",
		"background": true, "output": []any{}, "tools": []any{},
		"usage": fakeUsage(0, 0),
	}
	completed := map[string]any{
		"id": responseID, "model": payload.Model, "object": "response", "status": "completed",
		"background": true,
		"output": []any{
			map[string]any{
				"id": "search_fake_research", "type": "web_search_call", "status": "completed",
				"action": map[string]any{
					"type": "search", "queries": []string{facts.Title},
					"sources": []any{map[string]any{"type": "url", "url": sourceURL}},
				},
			},
			map[string]any{
				"id": "msg_fake_research", "type": "message", "status": "completed", "role": "assistant",
				"content": []any{map[string]any{
					"type": "output_text", "text": string(structuredOutput), "annotations": []any{},
				}},
			},
		},
		"tools": []any{}, "usage": fakeUsage(inputTokens, outputTokens),
	}
	return queued, completed, nil
}

func fakeUsage(inputTokens int, outputTokens int) map[string]any {
	return map[string]any{
		"input_tokens":          inputTokens,
		"input_tokens_details":  map[string]int{"cache_write_tokens": 0, "cached_tokens": 0},
		"output_tokens":         outputTokens,
		"output_tokens_details": map[string]int{"reasoning_tokens": 0},
		"total_tokens":          inputTokens + outputTokens,
	}
}

func containsDomain(domains []string, wanted string) bool {
	for _, domain := range domains {
		if domain == wanted {
			return true
		}
	}
	return false
}

func fakeReasoningAllowed(value string) bool {
	return value == "none" || value == "low" || value == "medium" ||
		value == "high" || value == "xhigh" || value == "max"
}

func fakeVerbosityAllowed(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func deterministicExtraction(input string) (extraction.Output, error) {
	var evidence struct {
		Title      string                    `json:"title"`
		SourceID   string                    `json:"source_id"`
		SourceTier string                    `json:"source_tier"`
		Spans      []extraction.EvidenceSpan `json:"spans"`
	}
	encoded := strings.TrimPrefix(input, "UNTRUSTED_EVIDENCE\n")
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil || len(evidence.Spans) == 0 {
		return extraction.Output{}, fmt.Errorf("untrusted evidence must contain at least one valid span")
	}
	topicSet := make(map[string]struct{})
	claims := make([]extraction.Claim, 0)
	eventType := "general"
	lifecycle := "unknown"
	for _, span := range evidence.Spans {
		lower := strings.ToLower(span.Text)
		for token, topic := range map[string]string{
			"postgres": "postgresql", "typescript": "typescript", "node": "nodejs",
			"react": "react", "golang": "go", " go ": "go", "openai": "openai",
		} {
			if strings.Contains(" "+lower+" ", token) {
				topicSet[topic] = struct{}{}
			}
		}
		claimText := strings.TrimSpace(span.Text)
		if len(claimText) > 1000 {
			claimText = claimText[:1000]
			for !utf8.ValidString(claimText) {
				claimText = claimText[:len(claimText)-1]
			}
		}
		appendClaim := func(claimType string, normalizedValue string, material bool) {
			if len(claims) >= extraction.MaximumClaims {
				return
			}
			claims = append(claims, extraction.Claim{
				ClaimType:       claimType,
				ClaimText:       claimText,
				NormalizedValue: normalizedValue,
				Confidence:      "high",
				Material:        material,
				EvidenceSpanIDs: []string{span.ID},
			})
		}
		if match := fakeVersionPattern.FindString(span.Text); match != "" {
			appendClaim("version", strings.TrimPrefix(match, "v"), true)
		}
		if match := fakeDatePattern.FindString(span.Text); match != "" {
			appendClaim("date", match, true)
		}
		switch {
		case strings.Contains(lower, "cve-") || strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability"):
			normalized := fakeCVEPattern.FindString(span.Text)
			appendClaim("security", strings.ToUpper(normalized), true)
			eventType = "security"
			topicSet["security"] = struct{}{}
		case strings.Contains(lower, "breaking change") || strings.Contains(lower, "backward incompatible"):
			appendClaim("breaking_change", "true", true)
			if eventType != "security" {
				eventType = "breaking_change"
			}
		case strings.Contains(lower, "deprecat") || strings.Contains(lower, "end of life"):
			appendClaim("deprecation", "true", true)
			if eventType == "general" {
				eventType = "deprecation"
			}
		case strings.Contains(lower, "migration") || strings.Contains(lower, "must upgrade"):
			appendClaim("migration_prerequisite", "", true)
			if eventType == "general" {
				eventType = "migration"
			}
		case strings.Contains(lower, "release") || strings.Contains(lower, "released"):
			appendClaim("release", "", true)
			if eventType == "general" {
				eventType = "release"
			}
		}
		switch {
		case strings.Contains(lower, "release candidate"):
			lifecycle = "release_candidate"
		case strings.Contains(lower, "preview") || strings.Contains(lower, "beta"):
			lifecycle = "preview"
		case strings.Contains(lower, "end of life"):
			lifecycle = "end_of_life"
		case strings.Contains(lower, "deprecat"):
			lifecycle = "deprecated"
		case strings.Contains(lower, "stable"):
			lifecycle = "stable"
		}
	}
	topics := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	uncertainties := []string{}
	if len(claims) == 0 {
		uncertainties = append(uncertainties, "No supported release, version, date, migration, deprecation, breaking-change, or security fact was present.")
	}
	return extraction.Output{
		TopicIDs:                topics,
		EventType:               eventType,
		LifecycleState:          lifecycle,
		DeepExtractionJustified: eventType == "security" || eventType == "breaking_change",
		Claims:                  claims,
		Uncertainties:           uncertainties,
	}, nil
}

func deterministicEmbedding(input string, dimensions int) []float32 {
	vector := make([]float32, dimensions)
	tokens := strings.Fields(strings.ToLower(input))
	for index, token := range tokens {
		token = strings.TrimFunc(token, func(character rune) bool {
			return !unicode.IsLetter(character) && !unicode.IsNumber(character)
		})
		if token == "" {
			continue
		}
		addEmbeddingFeature(vector, canonicalFixtureToken(token), 1)
		if index > 0 {
			addEmbeddingFeature(vector, canonicalFixtureToken(tokens[index-1])+" "+canonicalFixtureToken(token), 0.5)
		}
	}
	var norm float64
	for _, value := range vector {
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		vector[0] = 1
		return vector
	}
	norm = math.Sqrt(norm)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / norm)
	}
	return vector
}

func addEmbeddingFeature(vector []float32, feature string, weight float32) {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(feature))
	value := hasher.Sum64()
	index := int(value % uint64(len(vector)))
	if value&(1<<63) == 0 {
		vector[index] += weight
		return
	}
	vector[index] -= weight
}

func canonicalFixtureToken(token string) string {
	switch strings.TrimFunc(token, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsNumber(character)
	}) {
	case "postgres", "postgresql", "sql", "database":
		return "database"
	case "replica", "replication", "failover", "availability":
		return "availability"
	case "javascript", "typescript", "node", "nodejs":
		return "javascript"
	case "security", "vulnerability", "cve", "advisory":
		return "security"
	default:
		return token
	}
}

func registerSource(mux *http.ServeMux) {
	mux.HandleFunc("GET /feed.xml", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		response.Header().Set("ETag", `"pr0-source-v1"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Relantern fake source</title><id>urn:relantern:fake-source</id><updated>2026-08-29T00:00:00Z</updated><entry><title>Foundation fixture</title><id>urn:relantern:fixture:foundation</id><updated>2026-08-29T00:00:00Z</updated><content>Static evidence fixture. Ingestion is disabled in PR 0.</content></entry></feed>`))
	})
	mux.HandleFunc("GET /article.html", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("ETag", `"manual-capture-v1"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`<!doctype html><html lang="en"><head><title>Go 1.27 fixture release</title></head><body><main><article><h1>Go 1.27 fixture release</h1><p>Go 1.27 is the stable released version on 2026-08-29.</p><p>This deterministic page validates manual capture without contacting a live source.</p></article></main></body></html>`))
	})
	mux.HandleFunc("GET /robots.txt", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("User-agent: *\nAllow: /\nCrawl-delay: 1\n"))
	})
}

type Capture struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	LocalDate      string    `json:"localDate"`
	Message        string    `json:"message"`
	ScheduledFor   time.Time `json:"scheduledFor"`
}
