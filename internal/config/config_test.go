package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/research"
)

func TestProductionRejectsFixedClock(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("CLOCK_MODE", "fixed")
	t.Setenv("TEST_NOW", "2026-08-29T12:00:00Z")

	if _, err := config.LoadCommon(); err == nil {
		t.Fatal("LoadCommon() succeeded with a fixed production clock")
	}
}

func TestTestEnvironmentLoadsFixedClock(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("CLOCK_MODE", "fixed")
	t.Setenv("TEST_NOW", "2026-08-29T12:00:00Z")

	common, err := config.LoadCommon()
	if err != nil {
		t.Fatalf("LoadCommon() error = %v", err)
	}
	want := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	if got := common.Clock.Now(); !got.Equal(want) {
		t.Fatalf("Clock.Now() = %s, want %s", got, want)
	}
}

func TestLoadSourcesUsesExplicitPaths(t *testing.T) {
	t.Setenv("SOURCE_REGISTRY_PATH", "/tmp/registry.yaml")
	t.Setenv("SOURCE_FIXTURES_PATH", "/tmp/fixtures.yaml")

	settings := config.LoadSources()
	if settings.RegistryPath != "/tmp/registry.yaml" {
		t.Fatalf("RegistryPath = %q", settings.RegistryPath)
	}
	if settings.FixturesPath != "/tmp/fixtures.yaml" {
		t.Fatalf("FixturesPath = %q", settings.FixturesPath)
	}
}

func TestLoadLocalOAuthStubIsBoundedToLocalOwner(t *testing.T) {
	t.Setenv("LOCAL_OAUTH_STUB_SECRET", strings.Repeat("s", 32))
	t.Setenv("AUTH_ALLOWED_GITHUB_USER_ID", "5276132")

	settings, err := config.LoadLocalOAuthStub(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadLocalOAuthStub() error = %v", err)
	}
	if settings.ClientSecret != strings.Repeat("s", 32) || settings.OwnerGitHubUserID != "5276132" {
		t.Fatalf("LoadLocalOAuthStub() = %+v", settings)
	}
	if _, err := config.LoadLocalOAuthStub(config.EnvironmentProduction); err == nil {
		t.Fatal("LoadLocalOAuthStub() accepted a production environment")
	}
}

func TestLoadLocalOAuthStubRejectsWeakOrMalformedIdentity(t *testing.T) {
	t.Setenv("LOCAL_OAUTH_STUB_SECRET", "short")
	t.Setenv("AUTH_ALLOWED_GITHUB_USER_ID", "5276132")
	if _, err := config.LoadLocalOAuthStub(config.EnvironmentLocal); err == nil {
		t.Fatal("LoadLocalOAuthStub() accepted a weak secret")
	}

	t.Setenv("LOCAL_OAUTH_STUB_SECRET", strings.Repeat("s", 32))
	t.Setenv("AUTH_ALLOWED_GITHUB_USER_ID", "owner")
	if _, err := config.LoadLocalOAuthStub(config.EnvironmentLocal); err == nil {
		t.Fatal("LoadLocalOAuthStub() accepted a non-numeric owner ID")
	}
}

func TestLoadWorkerRequiresOneMinuteReconciliation(t *testing.T) {
	t.Setenv("SCHEDULER_RECONCILE_INTERVAL", "5s")

	if _, err := config.LoadWorker(); err == nil {
		t.Fatal("LoadWorker() accepted a non-minute reconciliation interval")
	}
}

func TestLoadDeliveryDefaultsToLocalCapture(t *testing.T) {
	t.Setenv("FAKE_DELIVERY_URL", "http://127.0.0.1:8092/capture")
	settings, err := config.LoadDelivery(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadDelivery() error = %v", err)
	}
	if settings.Mode != "log" || settings.AllowLive || settings.CaptureURL != "http://127.0.0.1:8092/capture" ||
		settings.DiscordEnabled || settings.ResendEnabled || settings.RequestTimeout != 10*time.Second {
		t.Fatalf("LoadDelivery() = %+v", settings)
	}
}

func TestLoadDeliveryIsFailClosedAcrossEnvironments(t *testing.T) {
	t.Run("local live is forbidden", func(t *testing.T) {
		t.Setenv("DELIVERY_MODE", "live")
		t.Setenv("ALLOW_LIVE_DELIVERY", "true")
		if _, err := config.LoadDelivery(config.EnvironmentLocal); err == nil {
			t.Fatal("LoadDelivery() accepted local live delivery")
		}
	})
	t.Run("hosted defaults disabled without secrets", func(t *testing.T) {
		settings, err := config.LoadDelivery(config.EnvironmentStaging)
		if err != nil || settings.Mode != "disabled" || settings.AllowLive {
			t.Fatalf("LoadDelivery() = %+v, %v", settings, err)
		}
	})
	t.Run("live requires the fuse", func(t *testing.T) {
		t.Setenv("DELIVERY_MODE", "live")
		t.Setenv("DISCORD_ENABLED", "true")
		if _, err := config.LoadDelivery(config.EnvironmentProduction); err == nil {
			t.Fatal("LoadDelivery() accepted live delivery without the fuse")
		}
	})
	t.Run("Discord is host allowlisted", func(t *testing.T) {
		t.Setenv("DELIVERY_MODE", "live")
		t.Setenv("ALLOW_LIVE_DELIVERY", "true")
		t.Setenv("DISCORD_ENABLED", "true")
		t.Setenv("PUBLIC_BASE_URL", "https://app.relantern.example")
		t.Setenv("DISCORD_WEBHOOK_URL", "https://example.com/api/webhooks/123/token")
		if _, err := config.LoadDelivery(config.EnvironmentStaging); err == nil {
			t.Fatal("LoadDelivery() accepted an untrusted Discord host")
		}
	})
}

func TestLoadDeliveryAcceptsExplicitHostedDiscord(t *testing.T) {
	t.Setenv("DELIVERY_MODE", "live")
	t.Setenv("ALLOW_LIVE_DELIVERY", "true")
	t.Setenv("DISCORD_ENABLED", "true")
	t.Setenv("PUBLIC_BASE_URL", "https://app.relantern.example")
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/123/token")
	settings, err := config.LoadDelivery(config.EnvironmentStaging)
	if err != nil {
		t.Fatalf("LoadDelivery() error = %v", err)
	}
	if !settings.DiscordEnabled || settings.DiscordWebhookURL == "" || settings.Mode != "live" {
		t.Fatalf("LoadDelivery() = %+v", settings)
	}
}

func TestLoadObjectStorageAllowsExplicitLocalHTTPOrigin(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_ENDPOINT", "http://minio:9000")
	t.Setenv("OBJECT_STORAGE_BUCKET", "relantern-test")
	t.Setenv("OBJECT_STORAGE_ACCESS_KEY", "fixture-access")
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", "fixture-secret")

	settings, err := config.LoadObjectStorage(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadObjectStorage() error = %v", err)
	}
	if settings.Endpoint != "http://minio:9000" || settings.Bucket != "relantern-test" {
		t.Fatalf("LoadObjectStorage() = %+v", settings)
	}
}

func TestLoadObjectStorageRequiresHTTPSOutsideLocalAndTest(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_ENDPOINT", "http://storage.internal:9000")
	t.Setenv("OBJECT_STORAGE_ACCESS_KEY", "fixture-access")
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", "fixture-secret")

	if _, err := config.LoadObjectStorage(config.EnvironmentProduction); err == nil {
		t.Fatal("LoadObjectStorage() accepted production HTTP")
	}
}

func TestLoadDedupeUsesEvaluatedDefaultsAndRejectsInvalidBounds(t *testing.T) {
	settings, err := config.LoadDedupe()
	if err != nil {
		t.Fatalf("LoadDedupe() error = %v", err)
	}
	if settings.SimHashDistance != dedupe.EvaluatedSimHashDistance ||
		settings.EmbeddingSimilarity != dedupe.EvaluatedEmbeddingSimilarity ||
		settings.ClusterMaxAge != 30*24*time.Hour {
		t.Fatalf("LoadDedupe() = %+v", settings)
	}

	t.Setenv("DEDUPE_SIMHASH_DISTANCE", "65")
	if _, err := config.LoadDedupe(); err == nil {
		t.Fatal("LoadDedupe() accepted an invalid SimHash distance")
	}
	t.Setenv("DEDUPE_SIMHASH_DISTANCE", "17")
	t.Setenv("DEDUPE_EMBEDDING_THRESHOLD", "0")
	if _, err := config.LoadDedupe(); err == nil {
		t.Fatal("LoadDedupe() accepted an invalid embedding threshold")
	}
	t.Setenv("DEDUPE_EMBEDDING_THRESHOLD", "0.86")
	t.Setenv("CLUSTER_MAX_AGE", "8761h")
	if _, err := config.LoadDedupe(); err == nil {
		t.Fatal("LoadDedupe() accepted an excessive cluster age")
	}
}

func TestLoadEmbeddingSearchUsesPinnedDefaultsAndRejectsDrift(t *testing.T) {
	settings, err := config.LoadEmbeddingSearch(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadEmbeddingSearch() error = %v", err)
	}
	if settings.BaseURL != "http://fake-openai:8091" ||
		settings.ModelID != "text-embedding-3-small" || settings.Dimensions != 1536 ||
		settings.RRFK != 60 || !settings.HybridEnabled || settings.Hosted ||
		settings.RequestTimeout != 5*time.Second {
		t.Fatalf("LoadEmbeddingSearch() = %+v", settings)
	}

	for name, value := range map[string]string{
		"EMBEDDING_DIMENSIONS":   "3072",
		"SEARCH_RRF_K":           "0",
		"SEARCH_HYBRID_ENABLED":  "sometimes",
		"OPENAI_EMBEDDING_MODEL": strings.Repeat("x", 256),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := config.LoadEmbeddingSearch(config.EnvironmentTest); err == nil {
				t.Fatalf("LoadEmbeddingSearch() accepted %s=%q", name, value)
			}
		})
	}
}

func TestLoadEmbeddingSearchRequiresPinnedHostedCredentials(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fixture-hosted-key")
	t.Setenv("OPENAI_PROJECT_ID", "project-fixture")
	settings, err := config.LoadEmbeddingSearch(config.EnvironmentStaging)
	if err != nil {
		t.Fatalf("LoadEmbeddingSearch() error = %v", err)
	}
	if !settings.Hosted || settings.BaseURL != "https://api.openai.com" ||
		settings.APIKey != "fixture-hosted-key" || settings.ProjectID != "project-fixture" {
		t.Fatalf("LoadEmbeddingSearch() = %+v", settings)
	}

	t.Setenv("OPENAI_BASE_URL", "https://example.com")
	if _, err := config.LoadEmbeddingSearch(config.EnvironmentStaging); err == nil {
		t.Fatal("LoadEmbeddingSearch() accepted an untrusted hosted provider")
	}
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := config.LoadEmbeddingSearch(config.EnvironmentStaging); err == nil {
		t.Fatal("LoadEmbeddingSearch() accepted hosted hybrid search without an API key")
	}
}

func TestLoadOpenAIExtractionUsesDisconnectedPinnedLocalDefaults(t *testing.T) {
	t.Setenv("FAKE_OPENAI_URL", "http://127.0.0.1:8091")

	settings, err := config.LoadOpenAIExtraction(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadOpenAIExtraction() error = %v", err)
	}
	if !settings.Enabled || settings.BaseURL != "http://127.0.0.1:8091" ||
		settings.APIKey != "relantern-local-fake-provider" ||
		settings.ModelID != extraction.DefaultFastModelID ||
		settings.Reasoning != "low" || settings.Verbosity != "low" ||
		settings.MaxOutputTokens != 4096 || settings.MonthlySoftUSD != "25.00" ||
		settings.MonthlyHardUSD != "50.00" || settings.MaximumDocumentAge != 30*24*time.Hour ||
		settings.RequestTimeout != 10*time.Minute {
		t.Fatalf("LoadOpenAIExtraction() = %+v", settings)
	}
}

func TestLoadOpenAIResearchUsesDisconnectedBoundedLocalDefaults(t *testing.T) {
	t.Setenv("FAKE_OPENAI_URL", "http://127.0.0.1:8091")

	settings, err := config.LoadOpenAIResearch(config.EnvironmentTest)
	if err != nil {
		t.Fatalf("LoadOpenAIResearch() error = %v", err)
	}
	if !settings.Enabled || settings.BaseURL != "http://127.0.0.1:8091" ||
		settings.APIKey != "relantern-local-fake-provider" || settings.ModelID != research.DefaultModelID ||
		settings.Reasoning != "medium" || settings.Verbosity != "low" ||
		settings.MaxOutputTokens != 8192 || settings.MaxToolCalls != 4 ||
		!settings.Background || settings.DailyWebSearchLimit != 100 ||
		settings.RequestTimeout != 30*time.Minute || len(settings.AllowedDomains) == 0 ||
		len(settings.BlockedDomains) == 0 {
		t.Fatalf("LoadOpenAIResearch() = %+v", settings)
	}
	for name, value := range map[string]string{
		"OPENAI_BACKGROUND_ENABLED":         "false",
		"OPENAI_RESEARCH_MAX_TOOL_CALLS":    "0",
		"OPENAI_DAILY_WEB_SEARCH_LIMIT":     "3",
		"OPENAI_RESEARCH_ALLOWED_DOMAINS":   "https://go.dev",
		"OPENAI_RESEARCH_BLOCKED_DOMAINS":   "gist.github.com,gist.github.com",
		"OPENAI_RESEARCH_MAX_OUTPUT_TOKENS": "129",
		"RIVER_RESEARCH_TIMEOUT":            "31m",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := config.LoadOpenAIResearch(config.EnvironmentTest); err == nil {
				t.Fatalf("LoadOpenAIResearch() accepted %s=%q", name, value)
			}
		})
	}
}

func TestLoadOpenAIWebhookInternalRequiresStrongSecret(t *testing.T) {
	t.Setenv("WEB_INTERNAL_SERVICE_TOKEN", strings.Repeat("s", 32))
	settings, err := config.LoadOpenAIWebhookInternal()
	if err != nil || settings.ServiceToken != strings.Repeat("s", 32) {
		t.Fatalf("LoadOpenAIWebhookInternal() = %+v, %v", settings, err)
	}
	t.Setenv("WEB_INTERNAL_SERVICE_TOKEN", "short")
	if _, err := config.LoadOpenAIWebhookInternal(); err == nil {
		t.Fatal("LoadOpenAIWebhookInternal() accepted a weak service token")
	}
}

func TestLoadOpenAIExtractionRejectsUnsafeHostedAndBudgetConfiguration(t *testing.T) {
	t.Run("hosted enabled requires key", func(t *testing.T) {
		t.Setenv("OPENAI_FAST_ENABLED", "true")
		if _, err := config.LoadOpenAIExtraction(config.EnvironmentProduction); err == nil {
			t.Fatal("LoadOpenAIExtraction() accepted hosted extraction without a key")
		}
	})
	t.Run("hosted URL is allowlisted", func(t *testing.T) {
		t.Setenv("OPENAI_FAST_ENABLED", "true")
		t.Setenv("OPENAI_API_KEY", "fixture-key")
		t.Setenv("OPENAI_BASE_URL", "https://example.com")
		if _, err := config.LoadOpenAIExtraction(config.EnvironmentStaging); err == nil {
			t.Fatal("LoadOpenAIExtraction() accepted an untrusted hosted origin")
		}
	})
	t.Run("local URL is loopback only", func(t *testing.T) {
		t.Setenv("FAKE_OPENAI_URL", "http://example.com")
		if _, err := config.LoadOpenAIExtraction(config.EnvironmentLocal); err == nil {
			t.Fatal("LoadOpenAIExtraction() accepted external local traffic")
		}
	})
	for name, value := range map[string]string{
		"OPENAI_FAST_ENABLED":           "sometimes",
		"OPENAI_FAST_REASONING":         "extreme",
		"OPENAI_VERBOSITY":              "verbose",
		"OPENAI_FAST_MAX_OUTPUT_TOKENS": "1",
		"OPENAI_MONTHLY_SOFT_USD":       "1e2",
		"OPENAI_MAX_DOCUMENT_AGE":       "8761h",
		"RIVER_AI_TIMEOUT":              "11m",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("FAKE_OPENAI_URL", "http://127.0.0.1:8091")
			t.Setenv(name, value)
			if _, err := config.LoadOpenAIExtraction(config.EnvironmentTest); err == nil {
				t.Fatalf("LoadOpenAIExtraction() accepted %s=%q", name, value)
			}
		})
	}
	t.Run("hard budget below soft", func(t *testing.T) {
		t.Setenv("FAKE_OPENAI_URL", "http://127.0.0.1:8091")
		t.Setenv("OPENAI_MONTHLY_SOFT_USD", "50.00")
		t.Setenv("OPENAI_MONTHLY_HARD_USD", "25.00")
		if _, err := config.LoadOpenAIExtraction(config.EnvironmentTest); err == nil {
			t.Fatal("LoadOpenAIExtraction() accepted hard budget below soft budget")
		}
	})
}
