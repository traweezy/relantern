package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/research"
	"github.com/traweezy/relantern/internal/search"
)

type Environment string

const (
	EnvironmentLocal      Environment = "local"
	EnvironmentTest       Environment = "test"
	EnvironmentStaging    Environment = "staging"
	EnvironmentProduction Environment = "production"
)

type Common struct {
	Environment Environment
	Version     string
	GitSHA      string
	LogLevel    string
	Clock       clock.Clock
}

type Database struct {
	URL      string
	MaxConns int32
	MinConns int32
}

type HTTP struct {
	Port            uint16
	ShutdownTimeout time.Duration
}

type Worker struct {
	ReconcileInterval time.Duration
	RequestTimeout    time.Duration
}

type Delivery struct {
	Mode              string
	AllowLive         bool
	CaptureURL        string
	DiscordEnabled    bool
	DiscordWebhookURL string
	ResendEnabled     bool
	ResendAPIURL      string
	ResendAPIKey      string
	EmailFrom         string
	EmailTo           string
	PublicBaseURL     string
	RequestTimeout    time.Duration
}

type Sources struct {
	RegistryPath string
	FixturesPath string
}

type ObjectStorage struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

type Dedupe struct {
	SimHashDistance     int
	EmbeddingSimilarity float64
	ClusterMaxAge       time.Duration
}

type EmbeddingSearch struct {
	BaseURL        string
	ModelID        string
	Dimensions     int
	RRFK           int
	HybridEnabled  bool
	RequestTimeout time.Duration
}

type OpenAIExtraction struct {
	Enabled            bool
	BaseURL            string
	APIKey             string
	ProjectID          string
	OrganizationID     string
	ModelID            string
	Reasoning          string
	Verbosity          string
	MaxOutputTokens    int
	MonthlySoftUSD     extraction.USD
	MonthlyHardUSD     extraction.USD
	MaximumDocumentAge time.Duration
	RequestTimeout     time.Duration
}

type OpenAIResearch struct {
	Enabled             bool
	BaseURL             string
	APIKey              string
	ProjectID           string
	OrganizationID      string
	ModelID             string
	Reasoning           string
	Verbosity           string
	MaxOutputTokens     int
	MaxToolCalls        int
	AllowedDomains      []string
	BlockedDomains      []string
	Background          bool
	DailyWebSearchLimit int
	MonthlySoftUSD      extraction.USD
	MonthlyHardUSD      extraction.USD
	RequestTimeout      time.Duration
}

type OpenAIWebhookInternal struct {
	ServiceToken string
}

type LocalOAuthStub struct {
	ClientSecret      string
	OwnerGitHubUserID string
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var githubUserIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)

func LoadCommon() (Common, error) {
	environment := Environment(valueOrDefault("APP_ENV", string(EnvironmentLocal)))
	if err := validateEnvironment(environment); err != nil {
		return Common{}, err
	}

	configuredClock, err := loadClock(environment)
	if err != nil {
		return Common{}, err
	}

	return Common{
		Environment: environment,
		Version:     valueOrDefault("APP_VERSION", "0.0.0-dev"),
		GitSHA:      valueOrDefault("GIT_SHA", "unknown"),
		LogLevel:    valueOrDefault("LOG_LEVEL", "info"),
		Clock:       configuredClock,
	}, nil
}

func LoadDatabase() (Database, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		var err error
		databaseURL, err = localDatabaseURL()
		if err != nil {
			return Database{}, err
		}
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return Database{}, errors.New("DATABASE_URL must be a valid absolute PostgreSQL URL")
	}
	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		return Database{}, errors.New("DATABASE_URL must use postgres or postgresql scheme")
	}

	maxConns, err := int32Value("DATABASE_MAX_CONNS", 20)
	if err != nil {
		return Database{}, err
	}
	minConns, err := int32Value("DATABASE_MIN_CONNS", 2)
	if err != nil {
		return Database{}, err
	}
	if minConns < 0 || maxConns < 1 || minConns > maxConns {
		return Database{}, errors.New("database pool sizes must satisfy 0 <= min <= max and max >= 1")
	}

	return Database{URL: databaseURL, MaxConns: maxConns, MinConns: minConns}, nil
}

func LoadHTTP(defaultPort uint16) (HTTP, error) {
	portValue := valueOrDefault("HTTP_PORT", strconv.FormatUint(uint64(defaultPort), 10))
	port, err := strconv.ParseUint(portValue, 10, 16)
	if err != nil || port == 0 {
		return HTTP{}, fmt.Errorf("HTTP_PORT must be an integer from 1 through 65535: %q", portValue)
	}

	shutdownTimeout, err := time.ParseDuration(valueOrDefault("SHUTDOWN_TIMEOUT", "15s"))
	if err != nil || shutdownTimeout <= 0 {
		return HTTP{}, errors.New("SHUTDOWN_TIMEOUT must be a positive duration")
	}

	return HTTP{Port: uint16(port), ShutdownTimeout: shutdownTimeout}, nil
}

func LoadLocalOAuthStub(environment Environment) (LocalOAuthStub, error) {
	if environment != EnvironmentLocal && environment != EnvironmentTest {
		return LocalOAuthStub{}, errors.New("the OAuth stub is restricted to local and test environments")
	}
	clientSecret, err := secretValue("LOCAL_OAUTH_STUB_SECRET", "LOCAL_OAUTH_STUB_SECRET_FILE")
	if err != nil {
		return LocalOAuthStub{}, err
	}
	if len(clientSecret) < 32 {
		return LocalOAuthStub{}, errors.New("LOCAL_OAUTH_STUB_SECRET must contain at least 32 characters")
	}
	ownerGitHubUserID := strings.TrimSpace(os.Getenv("AUTH_ALLOWED_GITHUB_USER_ID"))
	if !githubUserIDPattern.MatchString(ownerGitHubUserID) {
		return LocalOAuthStub{}, errors.New("AUTH_ALLOWED_GITHUB_USER_ID must be a positive numeric GitHub user ID")
	}
	return LocalOAuthStub{
		ClientSecret:      clientSecret,
		OwnerGitHubUserID: ownerGitHubUserID,
	}, nil
}

func LoadWorker() (Worker, error) {
	reconcileInterval, err := positiveDuration("SCHEDULER_RECONCILE_INTERVAL", "1m")
	if err != nil {
		return Worker{}, err
	}
	if reconcileInterval != time.Minute {
		return Worker{}, errors.New("SCHEDULER_RECONCILE_INTERVAL must be exactly 1m")
	}
	requestTimeout, err := positiveDuration("PROVIDER_REQUEST_TIMEOUT", "5s")
	if err != nil {
		return Worker{}, err
	}

	return Worker{
		ReconcileInterval: reconcileInterval,
		RequestTimeout:    requestTimeout,
	}, nil
}

func LoadDelivery(environment Environment) (Delivery, error) {
	local := environment == EnvironmentLocal || environment == EnvironmentTest
	defaultMode := "disabled"
	if local {
		defaultMode = "log"
	}
	mode := valueOrDefault("DELIVERY_MODE", defaultMode)
	if !stringAllowed(mode, "disabled", "log", "live") {
		return Delivery{}, errors.New("DELIVERY_MODE must be disabled, log, or live")
	}
	allowLive, err := strconv.ParseBool(valueOrDefault("ALLOW_LIVE_DELIVERY", "false"))
	if err != nil {
		return Delivery{}, errors.New("ALLOW_LIVE_DELIVERY must be true or false")
	}
	if local && (mode == "live" || allowLive) {
		return Delivery{}, errors.New("live delivery is forbidden in local and test environments")
	}
	if !local && mode == "log" {
		return Delivery{}, errors.New("hosted environments may not use the local capture delivery mode")
	}
	if mode == "live" && !allowLive {
		return Delivery{}, errors.New("DELIVERY_MODE=live requires ALLOW_LIVE_DELIVERY=true")
	}

	requestTimeout, err := positiveDuration("DELIVERY_REQUEST_TIMEOUT", "10s")
	if err != nil || requestTimeout > 30*time.Second {
		return Delivery{}, errors.New("DELIVERY_REQUEST_TIMEOUT must be positive and at most 30 seconds")
	}
	captureURL := strings.TrimSpace(os.Getenv("FAKE_DELIVERY_URL"))
	if mode == "log" {
		if err := validateLocalCaptureURL(captureURL); err != nil {
			return Delivery{}, err
		}
	}

	discordEnabled, err := strconv.ParseBool(valueOrDefault("DISCORD_ENABLED", "false"))
	if err != nil {
		return Delivery{}, errors.New("DISCORD_ENABLED must be true or false")
	}
	resendEnabled, err := strconv.ParseBool(valueOrDefault("RESEND_ENABLED", "false"))
	if err != nil {
		return Delivery{}, errors.New("RESEND_ENABLED must be true or false")
	}
	if mode != "live" && (discordEnabled || resendEnabled) {
		return Delivery{}, errors.New("external delivery channels require DELIVERY_MODE=live")
	}
	if mode == "live" && !discordEnabled && !resendEnabled {
		return Delivery{}, errors.New("live delivery requires at least one enabled external channel")
	}

	publicBaseURL := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	if mode == "live" {
		if err := validateHTTPSOrigin("PUBLIC_BASE_URL", publicBaseURL, ""); err != nil {
			return Delivery{}, err
		}
	}
	discordURL := ""
	if discordEnabled {
		discordURL, err = secretValue("DISCORD_WEBHOOK_URL", "DISCORD_WEBHOOK_URL_FILE")
		if err != nil {
			return Delivery{}, err
		}
		if err := validateDiscordWebhookURL(discordURL); err != nil {
			return Delivery{}, err
		}
	}

	resendURL := valueOrDefault("RESEND_API_URL", "https://api.resend.com/emails")
	resendAPIKey := ""
	emailFrom := ""
	emailTo := ""
	if resendEnabled {
		if resendURL != "https://api.resend.com/emails" {
			return Delivery{}, errors.New("RESEND_API_URL must be exactly https://api.resend.com/emails")
		}
		resendAPIKey, err = secretValue("RESEND_API_KEY", "RESEND_API_KEY_FILE")
		if err != nil {
			return Delivery{}, err
		}
		emailFrom = strings.TrimSpace(os.Getenv("DELIVERY_EMAIL_FROM"))
		emailTo = strings.TrimSpace(os.Getenv("DELIVERY_EMAIL_TO"))
		if resendAPIKey == "" || !validEmailAddress(emailFrom) || !validEmailAddress(emailTo) {
			return Delivery{}, errors.New("Resend delivery requires an API key and valid DELIVERY_EMAIL_FROM and DELIVERY_EMAIL_TO values")
		}
	}

	return Delivery{
		Mode: mode, AllowLive: allowLive, CaptureURL: captureURL,
		DiscordEnabled: discordEnabled, DiscordWebhookURL: discordURL,
		ResendEnabled: resendEnabled, ResendAPIURL: resendURL, ResendAPIKey: resendAPIKey,
		EmailFrom: emailFrom, EmailTo: emailTo, PublicBaseURL: strings.TrimSuffix(publicBaseURL, "/"),
		RequestTimeout: requestTimeout,
	}, nil
}

func LoadSources() Sources {
	return Sources{
		RegistryPath: valueOrDefault("SOURCE_REGISTRY_PATH", "sources/registry.yaml"),
		FixturesPath: valueOrDefault("SOURCE_FIXTURES_PATH", "sources/fixtures.yaml"),
	}
}

func LoadObjectStorage(environment Environment) (ObjectStorage, error) {
	endpoint := valueOrDefault("OBJECT_STORAGE_ENDPOINT", "http://minio:9000")
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Host == "" || parsedEndpoint.User != nil || parsedEndpoint.RawQuery != "" || parsedEndpoint.Fragment != "" || parsedEndpoint.Path != "" && parsedEndpoint.Path != "/" {
		return ObjectStorage{}, errors.New("OBJECT_STORAGE_ENDPOINT must be an absolute HTTP(S) origin")
	}
	if parsedEndpoint.Scheme != "https" && !(parsedEndpoint.Scheme == "http" && (environment == EnvironmentLocal || environment == EnvironmentTest)) {
		return ObjectStorage{}, errors.New("OBJECT_STORAGE_ENDPOINT must use HTTPS outside local and test")
	}
	bucket := valueOrDefault("OBJECT_STORAGE_BUCKET", "relantern-local")
	if !bucketPattern.MatchString(bucket) || strings.Contains(bucket, "..") {
		return ObjectStorage{}, errors.New("OBJECT_STORAGE_BUCKET must be a valid DNS-style bucket name")
	}
	accessKey, err := secretValue("OBJECT_STORAGE_ACCESS_KEY", "OBJECT_STORAGE_ACCESS_KEY_FILE")
	if err != nil {
		return ObjectStorage{}, err
	}
	secretKey, err := secretValue("OBJECT_STORAGE_SECRET_KEY", "OBJECT_STORAGE_SECRET_KEY_FILE")
	if err != nil {
		return ObjectStorage{}, err
	}
	if accessKey == "" || secretKey == "" {
		return ObjectStorage{}, errors.New("object-storage access and secret keys are required")
	}
	return ObjectStorage{
		Endpoint:  strings.TrimSuffix(endpoint, "/"),
		Bucket:    bucket,
		Region:    valueOrDefault("OBJECT_STORAGE_REGION", "us-east-1"),
		AccessKey: accessKey,
		SecretKey: secretKey,
	}, nil
}

func LoadDedupe() (Dedupe, error) {
	distanceValue := valueOrDefault("DEDUPE_SIMHASH_DISTANCE", strconv.Itoa(dedupe.EvaluatedSimHashDistance))
	distance, err := strconv.Atoi(distanceValue)
	if err != nil || distance < 0 || distance > 64 {
		return Dedupe{}, errors.New("DEDUPE_SIMHASH_DISTANCE must be an integer from 0 through 64")
	}
	similarityValue := valueOrDefault(
		"DEDUPE_EMBEDDING_THRESHOLD",
		strconv.FormatFloat(dedupe.EvaluatedEmbeddingSimilarity, 'f', -1, 64),
	)
	similarity, err := strconv.ParseFloat(similarityValue, 64)
	if err != nil || similarity <= 0 || similarity > 1 {
		return Dedupe{}, errors.New("DEDUPE_EMBEDDING_THRESHOLD must be greater than 0 and at most 1")
	}
	maximumAge, err := positiveDuration("CLUSTER_MAX_AGE", "720h")
	if err != nil {
		return Dedupe{}, err
	}
	if maximumAge > 365*24*time.Hour {
		return Dedupe{}, errors.New("CLUSTER_MAX_AGE may not exceed 365 days")
	}
	return Dedupe{
		SimHashDistance:     distance,
		EmbeddingSimilarity: similarity,
		ClusterMaxAge:       maximumAge,
	}, nil
}

func LoadEmbeddingSearch() (EmbeddingSearch, error) {
	dimensionsValue := valueOrDefault("EMBEDDING_DIMENSIONS", strconv.Itoa(embedding.DefaultDimensions))
	dimensions, err := strconv.Atoi(dimensionsValue)
	if err != nil || dimensions != embedding.DefaultDimensions {
		return EmbeddingSearch{}, fmt.Errorf("EMBEDDING_DIMENSIONS must be exactly %d", embedding.DefaultDimensions)
	}
	rrfValue := valueOrDefault("SEARCH_RRF_K", strconv.Itoa(search.DefaultRRFK))
	rrfK, err := strconv.Atoi(rrfValue)
	if err != nil || rrfK < 1 || rrfK > 1000 {
		return EmbeddingSearch{}, errors.New("SEARCH_RRF_K must be an integer from 1 through 1000")
	}
	hybridEnabled, err := strconv.ParseBool(valueOrDefault("SEARCH_HYBRID_ENABLED", "true"))
	if err != nil {
		return EmbeddingSearch{}, errors.New("SEARCH_HYBRID_ENABLED must be true or false")
	}
	requestTimeout, err := positiveDuration("PROVIDER_REQUEST_TIMEOUT", "5s")
	if err != nil {
		return EmbeddingSearch{}, err
	}
	modelID := strings.TrimSpace(valueOrDefault("OPENAI_EMBEDDING_MODEL", embedding.DefaultModelID))
	if modelID == "" || len(modelID) > 255 {
		return EmbeddingSearch{}, errors.New("OPENAI_EMBEDDING_MODEL must contain between 1 and 255 characters")
	}
	return EmbeddingSearch{
		BaseURL:        valueOrDefault("FAKE_OPENAI_URL", "http://fake-openai:8091"),
		ModelID:        modelID,
		Dimensions:     dimensions,
		RRFK:           rrfK,
		HybridEnabled:  hybridEnabled,
		RequestTimeout: requestTimeout,
	}, nil
}

func LoadOpenAIExtraction(environment Environment) (OpenAIExtraction, error) {
	defaultEnabled := environment == EnvironmentLocal || environment == EnvironmentTest
	enabled, err := strconv.ParseBool(valueOrDefault("OPENAI_FAST_ENABLED", strconv.FormatBool(defaultEnabled)))
	if err != nil {
		return OpenAIExtraction{}, errors.New("OPENAI_FAST_ENABLED must be true or false")
	}
	baseURL := valueOrDefault("OPENAI_BASE_URL", "https://api.openai.com")
	apiKey := ""
	if environment == EnvironmentLocal || environment == EnvironmentTest {
		baseURL = valueOrDefault("FAKE_OPENAI_URL", "http://fake-openai:8091")
		apiKey = "relantern-local-fake-provider"
	} else if enabled {
		apiKey, err = secretValue("OPENAI_API_KEY", "OPENAI_API_KEY_FILE")
		if err != nil {
			return OpenAIExtraction{}, err
		}
		if apiKey == "" {
			return OpenAIExtraction{}, errors.New("OPENAI_API_KEY or OPENAI_API_KEY_FILE is required when hosted extraction is enabled")
		}
	}
	if err := validateOpenAIBaseURL(baseURL, environment); err != nil {
		return OpenAIExtraction{}, err
	}
	modelID := strings.TrimSpace(valueOrDefault("OPENAI_MODEL_FAST", extraction.DefaultFastModelID))
	if modelID == "" || len(modelID) > 255 {
		return OpenAIExtraction{}, errors.New("OPENAI_MODEL_FAST must contain between 1 and 255 characters")
	}
	reasoning := valueOrDefault("OPENAI_FAST_REASONING", extraction.DefaultFastReasoning)
	if !stringAllowed(reasoning, "none", "low", "medium", "high", "xhigh", "max") {
		return OpenAIExtraction{}, errors.New("OPENAI_FAST_REASONING must be none, low, medium, high, xhigh, or max")
	}
	verbosity := valueOrDefault("OPENAI_VERBOSITY", extraction.DefaultVerbosity)
	if !stringAllowed(verbosity, "low", "medium", "high") {
		return OpenAIExtraction{}, errors.New("OPENAI_VERBOSITY must be low, medium, or high")
	}
	maxOutputTokens, err := strconv.Atoi(valueOrDefault("OPENAI_FAST_MAX_OUTPUT_TOKENS", strconv.Itoa(extraction.DefaultMaximumOutputTokens)))
	if err != nil || maxOutputTokens < 256 || maxOutputTokens > 128_000 {
		return OpenAIExtraction{}, errors.New("OPENAI_FAST_MAX_OUTPUT_TOKENS must be an integer from 256 through 128000")
	}
	softBudget, err := extraction.ParseUSD(valueOrDefault("OPENAI_MONTHLY_SOFT_USD", "25.00"))
	if err != nil {
		return OpenAIExtraction{}, fmt.Errorf("OPENAI_MONTHLY_SOFT_USD: %w", err)
	}
	hardBudget, err := extraction.ParseUSD(valueOrDefault("OPENAI_MONTHLY_HARD_USD", "50.00"))
	if err != nil {
		return OpenAIExtraction{}, fmt.Errorf("OPENAI_MONTHLY_HARD_USD: %w", err)
	}
	if err := extraction.ValidateBudgetRange(softBudget, hardBudget); err != nil {
		return OpenAIExtraction{}, fmt.Errorf("invalid OpenAI monthly budgets: %w", err)
	}
	maximumAge, err := positiveDuration("OPENAI_MAX_DOCUMENT_AGE", "720h")
	if err != nil {
		return OpenAIExtraction{}, err
	}
	if maximumAge > 365*24*time.Hour {
		return OpenAIExtraction{}, errors.New("OPENAI_MAX_DOCUMENT_AGE may not exceed 365 days")
	}
	requestTimeout, err := positiveDuration("RIVER_AI_TIMEOUT", "10m")
	if err != nil {
		return OpenAIExtraction{}, err
	}
	if requestTimeout > 10*time.Minute {
		return OpenAIExtraction{}, errors.New("RIVER_AI_TIMEOUT may not exceed 10 minutes")
	}
	return OpenAIExtraction{
		Enabled:            enabled,
		BaseURL:            strings.TrimSuffix(baseURL, "/"),
		APIKey:             apiKey,
		ProjectID:          strings.TrimSpace(os.Getenv("OPENAI_PROJECT_ID")),
		OrganizationID:     strings.TrimSpace(os.Getenv("OPENAI_ORG_ID")),
		ModelID:            modelID,
		Reasoning:          reasoning,
		Verbosity:          verbosity,
		MaxOutputTokens:    maxOutputTokens,
		MonthlySoftUSD:     softBudget,
		MonthlyHardUSD:     hardBudget,
		MaximumDocumentAge: maximumAge,
		RequestTimeout:     requestTimeout,
	}, nil
}

func LoadOpenAIResearch(environment Environment) (OpenAIResearch, error) {
	defaultEnabled := environment == EnvironmentLocal || environment == EnvironmentTest
	enabled, err := strconv.ParseBool(valueOrDefault("OPENAI_RESEARCH_ENABLED", strconv.FormatBool(defaultEnabled)))
	if err != nil {
		return OpenAIResearch{}, errors.New("OPENAI_RESEARCH_ENABLED must be true or false")
	}
	background, err := strconv.ParseBool(valueOrDefault("OPENAI_BACKGROUND_ENABLED", "true"))
	if err != nil {
		return OpenAIResearch{}, errors.New("OPENAI_BACKGROUND_ENABLED must be true or false")
	}
	if enabled && !background {
		return OpenAIResearch{}, errors.New("enabled research requires OPENAI_BACKGROUND_ENABLED=true")
	}
	baseURL := valueOrDefault("OPENAI_BASE_URL", "https://api.openai.com")
	apiKey := ""
	if environment == EnvironmentLocal || environment == EnvironmentTest {
		baseURL = valueOrDefault("FAKE_OPENAI_URL", "http://fake-openai:8091")
		apiKey = "relantern-local-fake-provider"
	} else if enabled {
		apiKey, err = secretValue("OPENAI_API_KEY", "OPENAI_API_KEY_FILE")
		if err != nil {
			return OpenAIResearch{}, err
		}
		if apiKey == "" {
			return OpenAIResearch{}, errors.New("OPENAI_API_KEY or OPENAI_API_KEY_FILE is required when hosted research is enabled")
		}
	}
	if err := validateOpenAIBaseURL(baseURL, environment); err != nil {
		return OpenAIResearch{}, err
	}
	modelID := strings.TrimSpace(valueOrDefault("OPENAI_MODEL_RESEARCH", research.DefaultModelID))
	if modelID == "" || len(modelID) > 255 {
		return OpenAIResearch{}, errors.New("OPENAI_MODEL_RESEARCH must contain between 1 and 255 characters")
	}
	reasoning := valueOrDefault("OPENAI_RESEARCH_REASONING", research.DefaultReasoning)
	if !stringAllowed(reasoning, "none", "low", "medium", "high", "xhigh", "max") {
		return OpenAIResearch{}, errors.New("OPENAI_RESEARCH_REASONING must be none, low, medium, high, xhigh, or max")
	}
	verbosity := valueOrDefault("OPENAI_VERBOSITY", research.DefaultVerbosity)
	if !stringAllowed(verbosity, "low", "medium", "high") {
		return OpenAIResearch{}, errors.New("OPENAI_VERBOSITY must be low, medium, or high")
	}
	maxOutputTokens, err := strconv.Atoi(valueOrDefault("OPENAI_RESEARCH_MAX_OUTPUT_TOKENS", strconv.Itoa(research.DefaultMaximumOutputTokens)))
	if err != nil || maxOutputTokens < 256 || maxOutputTokens > 128_000 {
		return OpenAIResearch{}, errors.New("OPENAI_RESEARCH_MAX_OUTPUT_TOKENS must be an integer from 256 through 128000")
	}
	maxToolCalls, err := strconv.Atoi(valueOrDefault("OPENAI_RESEARCH_MAX_TOOL_CALLS", strconv.Itoa(research.DefaultMaximumToolCalls)))
	if err != nil || maxToolCalls < 1 || maxToolCalls > 10 {
		return OpenAIResearch{}, errors.New("OPENAI_RESEARCH_MAX_TOOL_CALLS must be an integer from 1 through 10")
	}
	dailyLimit, err := strconv.Atoi(valueOrDefault("OPENAI_DAILY_WEB_SEARCH_LIMIT", "100"))
	if err != nil || dailyLimit < maxToolCalls || dailyLimit > 100_000 {
		return OpenAIResearch{}, errors.New("OPENAI_DAILY_WEB_SEARCH_LIMIT must cover one research run and be at most 100000")
	}
	allowedDomains, err := commaSeparatedDomains(
		"OPENAI_RESEARCH_ALLOWED_DOMAINS",
		"go.dev,github.com,github.blog,nodejs.org,react.dev,nextjs.org,typescriptlang.org,microsoft.com,postgresql.org,docker.com,kubernetes.io,openai.com,developers.openai.com,railway.com,biomejs.dev,pnpm.io,tailwindcss.com,tanstack.com,radix-ui.com,ui.shadcn.com",
		true,
	)
	if err != nil {
		return OpenAIResearch{}, err
	}
	blockedDomains, err := commaSeparatedDomains(
		"OPENAI_RESEARCH_BLOCKED_DOMAINS",
		"bit.ly,gist.github.com,pastebin.com,tinyurl.com",
		false,
	)
	if err != nil {
		return OpenAIResearch{}, err
	}
	softBudget, err := extraction.ParseUSD(valueOrDefault("OPENAI_MONTHLY_SOFT_USD", "25.00"))
	if err != nil {
		return OpenAIResearch{}, fmt.Errorf("OPENAI_MONTHLY_SOFT_USD: %w", err)
	}
	hardBudget, err := extraction.ParseUSD(valueOrDefault("OPENAI_MONTHLY_HARD_USD", "50.00"))
	if err != nil {
		return OpenAIResearch{}, fmt.Errorf("OPENAI_MONTHLY_HARD_USD: %w", err)
	}
	if err := extraction.ValidateBudgetRange(softBudget, hardBudget); err != nil {
		return OpenAIResearch{}, fmt.Errorf("invalid OpenAI monthly budgets: %w", err)
	}
	requestTimeout, err := positiveDuration("RIVER_RESEARCH_TIMEOUT", "30m")
	if err != nil {
		return OpenAIResearch{}, err
	}
	if requestTimeout > 30*time.Minute {
		return OpenAIResearch{}, errors.New("RIVER_RESEARCH_TIMEOUT may not exceed 30 minutes")
	}
	return OpenAIResearch{
		Enabled: enabled, BaseURL: strings.TrimSuffix(baseURL, "/"), APIKey: apiKey,
		ProjectID:      strings.TrimSpace(os.Getenv("OPENAI_PROJECT_ID")),
		OrganizationID: strings.TrimSpace(os.Getenv("OPENAI_ORG_ID")),
		ModelID:        modelID, Reasoning: reasoning, Verbosity: verbosity,
		MaxOutputTokens: maxOutputTokens, MaxToolCalls: maxToolCalls,
		AllowedDomains: allowedDomains, BlockedDomains: blockedDomains,
		Background: background, DailyWebSearchLimit: dailyLimit,
		MonthlySoftUSD: softBudget, MonthlyHardUSD: hardBudget,
		RequestTimeout: requestTimeout,
	}, nil
}

func LoadOpenAIWebhookInternal() (OpenAIWebhookInternal, error) {
	token, err := secretValue("WEB_INTERNAL_SERVICE_TOKEN", "WEB_INTERNAL_SERVICE_TOKEN_FILE")
	if err != nil {
		return OpenAIWebhookInternal{}, err
	}
	if len(token) < 32 || len(token) > 512 {
		return OpenAIWebhookInternal{}, errors.New("WEB_INTERNAL_SERVICE_TOKEN or WEB_INTERNAL_SERVICE_TOKEN_FILE must contain 32 through 512 characters")
	}
	return OpenAIWebhookInternal{ServiceToken: token}, nil
}

func validateOpenAIBaseURL(value string, environment Environment) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("OpenAI provider URL must be an absolute trusted origin")
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path != "" && path != "/v1" {
		return errors.New("OpenAI provider URL path must be empty or /v1")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if environment == EnvironmentLocal || environment == EnvironmentTest {
		parsedIP := net.ParseIP(hostname)
		if parsed.Scheme != "http" ||
			(hostname != "fake-openai" && hostname != "localhost" && (parsedIP == nil || !parsedIP.IsLoopback())) {
			return errors.New("local and test OpenAI traffic is restricted to fake-openai or loopback HTTP")
		}
		return nil
	}
	if parsed.Scheme != "https" || hostname != "api.openai.com" {
		return errors.New("hosted OpenAI traffic is restricted to https://api.openai.com")
	}
	return nil
}

func validateLocalCaptureURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != "/capture" {
		return errors.New("FAKE_DELIVERY_URL must be an absolute local HTTP /capture URL")
	}
	hostname := strings.ToLower(parsed.Hostname())
	parsedIP := net.ParseIP(hostname)
	if hostname != "fake-delivery" && hostname != "localhost" && (parsedIP == nil || !parsedIP.IsLoopback()) {
		return errors.New("FAKE_DELIVERY_URL is restricted to fake-delivery or loopback")
	}
	return nil
}

func validateDiscordWebhookURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || strings.ToLower(parsed.Hostname()) != "discord.com" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("DISCORD_WEBHOOK_URL must be an HTTPS discord.com webhook URL")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "webhooks" || parts[2] == "" || parts[3] == "" {
		return errors.New("DISCORD_WEBHOOK_URL path must be /api/webhooks/{id}/{token}")
	}
	return nil
}

func validateHTTPSOrigin(name string, value string, expectedHost string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimSuffix(parsed.EscapedPath(), "/") != "" {
		return fmt.Errorf("%s must be an absolute HTTPS origin", name)
	}
	if expectedHost != "" && !strings.EqualFold(parsed.Hostname(), expectedHost) {
		return fmt.Errorf("%s must use host %s", name, expectedHost)
	}
	return nil
}

func validEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && strings.EqualFold(address.Address, value)
}

func validateEnvironment(environment Environment) error {
	switch environment {
	case EnvironmentLocal, EnvironmentTest, EnvironmentStaging, EnvironmentProduction:
		return nil
	default:
		return fmt.Errorf("APP_ENV must be local, test, staging, or production: %q", environment)
	}
}

func loadClock(environment Environment) (clock.Clock, error) {
	mode := valueOrDefault("CLOCK_MODE", "system")
	testNow := strings.TrimSpace(os.Getenv("TEST_NOW"))

	if environment == EnvironmentProduction && (mode != "system" || testNow != "") {
		return nil, errors.New("production requires CLOCK_MODE=system and an empty TEST_NOW")
	}

	switch mode {
	case "system":
		if testNow != "" {
			return nil, errors.New("TEST_NOW must be empty when CLOCK_MODE=system")
		}
		return clock.System{}, nil
	case "fixed":
		if environment != EnvironmentLocal && environment != EnvironmentTest {
			return nil, errors.New("CLOCK_MODE=fixed is allowed only in local or test")
		}
		if testNow == "" {
			return nil, errors.New("CLOCK_MODE=fixed requires TEST_NOW")
		}
		now, err := time.Parse(time.RFC3339, testNow)
		if err != nil {
			return nil, fmt.Errorf("TEST_NOW must be RFC3339: %w", err)
		}
		return clock.NewFixed(now), nil
	default:
		return nil, fmt.Errorf("CLOCK_MODE must be system or fixed: %q", mode)
	}
}

func localDatabaseURL() (string, error) {
	password, err := secretValue("DATABASE_PASSWORD", "DATABASE_PASSWORD_FILE")
	if err != nil {
		return "", err
	}
	if password == "" {
		return "", errors.New("DATABASE_URL or DATABASE_PASSWORD_FILE is required")
	}

	result := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(valueOrDefault("DATABASE_USER", "relantern"), password),
		Host:   valueOrDefault("DATABASE_HOST", "postgres:5432"),
		Path:   "/" + valueOrDefault("DATABASE_NAME", "relantern"),
	}
	query := result.Query()
	query.Set("sslmode", valueOrDefault("DATABASE_SSLMODE", "disable"))
	result.RawQuery = query.Encode()
	return result.String(), nil
}

func secretValue(valueName string, fileName string) (string, error) {
	if direct := strings.TrimSpace(os.Getenv(valueName)); direct != "" {
		return direct, nil
	}
	path := strings.TrimSpace(os.Getenv(fileName))
	if path == "" {
		return "", nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileName, err)
	}
	return strings.TrimSpace(string(contents)), nil
}

func int32Value(name string, fallback int32) (int32, error) {
	raw := valueOrDefault(name, strconv.FormatInt(int64(fallback), 10))
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be a 32-bit integer: %w", name, err)
	}
	return int32(value), nil
}

func positiveDuration(name string, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(valueOrDefault(name, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func stringAllowed(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func commaSeparatedDomains(name string, fallback string, required bool) ([]string, error) {
	raw := valueOrDefault(name, fallback)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		domain := strings.ToLower(strings.Trim(strings.TrimSpace(part), "."))
		if domain == "" {
			continue
		}
		if len(domain) > 253 || strings.ContainsAny(domain, "/:@?#") || net.ParseIP(domain) != nil {
			return nil, fmt.Errorf("%s contains invalid domain %q", name, part)
		}
		if _, duplicate := seen[domain]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate domain %q", name, domain)
		}
		seen[domain] = struct{}{}
		result = append(result, domain)
	}
	if required && len(result) == 0 {
		return nil, fmt.Errorf("%s must contain at least one domain", name)
	}
	if len(result) > 100 {
		return nil, fmt.Errorf("%s may contain at most 100 domains", name)
	}
	return result, nil
}

func valueOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
