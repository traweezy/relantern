package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/clock"
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
	DeliveryURL       string
	ReconcileInterval time.Duration
	RequestTimeout    time.Duration
}

type Sources struct {
	RegistryPath string
	FixturesPath string
}

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

func LoadWorker() (Worker, error) {
	deliveryURL := strings.TrimSpace(os.Getenv("FAKE_DELIVERY_URL"))
	if deliveryURL == "" {
		return Worker{}, errors.New("FAKE_DELIVERY_URL is required in the PR 0 safe profile")
	}
	parsedURL, err := url.Parse(deliveryURL)
	if err != nil || parsedURL.Scheme != "http" || parsedURL.Host == "" {
		return Worker{}, errors.New("FAKE_DELIVERY_URL must be an absolute HTTP URL")
	}
	if parsedURL.Hostname() != "fake-delivery" && parsedURL.Hostname() != "127.0.0.1" && parsedURL.Hostname() != "localhost" {
		return Worker{}, errors.New("PR 0 delivery is restricted to the local fake-delivery service")
	}

	reconcileInterval, err := positiveDuration("SCHEDULER_RECONCILE_INTERVAL", "1m")
	if err != nil {
		return Worker{}, err
	}
	requestTimeout, err := positiveDuration("PROVIDER_REQUEST_TIMEOUT", "5s")
	if err != nil {
		return Worker{}, err
	}

	return Worker{
		DeliveryURL:       deliveryURL,
		ReconcileInterval: reconcileInterval,
		RequestTimeout:    requestTimeout,
	}, nil
}

func LoadSources() Sources {
	return Sources{
		RegistryPath: valueOrDefault("SOURCE_REGISTRY_PATH", "sources/registry.yaml"),
		FixturesPath: valueOrDefault("SOURCE_FIXTURES_PATH", "sources/fixtures.yaml"),
	}
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

func valueOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
