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
