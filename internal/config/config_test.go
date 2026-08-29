package config_test

import (
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/config"
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
