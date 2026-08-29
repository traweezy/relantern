package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/traweezy/relantern/internal/sources"
)

func TestEndpointConfigurationOmitsRepositoryDataForOrdinarySource(t *testing.T) {
	configuration, err := endpointConfiguration(sources.Endpoint{ID: "go-blog"})
	if err != nil {
		t.Fatalf("endpointConfiguration() error = %v", err)
	}
	if string(configuration) != "{}" {
		t.Fatalf("configuration = %s", configuration)
	}
}

func TestEndpointConfigurationIncludesStableRepositoryIdentity(t *testing.T) {
	configuration, err := endpointConfiguration(sources.Endpoint{
		ID:               "github-react-react-releases",
		RepositoryNodeID: "MDEwOlJlcG9zaXRvcnkxMDI3MDI1MA==",
		RepositoryOwner:  "react",
		RepositoryName:   "react",
		RepositoryEvent:  sources.RepositoryEventReleases,
	})
	if err != nil {
		t.Fatalf("endpointConfiguration() error = %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(configuration, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["nodeId"] != "MDEwOlJlcG9zaXRvcnkxMDI3MDI1MA==" {
		t.Fatalf("nodeId = %q", decoded["nodeId"])
	}
	if decoded["event"] != "releases" {
		t.Fatalf("event = %q", decoded["event"])
	}
}

func TestSyncMirrorsEveryReviewedRecord(t *testing.T) {
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	executor := &recordingExecutor{}

	if err := (Store{}).Sync(context.Background(), executor, registry); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	wantCalls := 2 + len(registry.Sources) + (2 * len(registry.Repositories)) + len(registry.Endpoints())
	if len(executor.arguments) != wantCalls {
		t.Fatalf("Exec() calls = %d, want %d", len(executor.arguments), wantCalls)
	}
	if got := executor.arguments[2][5]; got != "paused" {
		t.Fatalf("first source validation state = %v", got)
	}
}

func TestSyncStopsAtDatabaseError(t *testing.T) {
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	executor := &recordingExecutor{err: errors.New("database unavailable")}

	err = (Store{}).Sync(context.Background(), executor, registry)
	if err == nil || err.Error() != "pause existing system sources: database unavailable" {
		t.Fatalf("Sync() error = %v", err)
	}
}

func TestSyncStopsWhenSourceUpsertFails(t *testing.T) {
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	executor := &recordingExecutor{err: errors.New("database unavailable"), failAt: 3}

	err = (Store{}).Sync(context.Background(), executor, registry)
	if err == nil || err.Error() != "upsert source \"go-blog\": database unavailable" {
		t.Fatalf("Sync() error = %v", err)
	}
}

func TestSyncMarksSourcesActiveWhenRegistryFuseIsEnabled(t *testing.T) {
	registry, err := sources.LoadRegistry("../../../sources/registry.yaml")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	registry.Enabled = true
	executor := &recordingExecutor{}

	if err := (Store{}).Sync(context.Background(), executor, registry); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if got := executor.arguments[2][5]; got != "active" {
		t.Fatalf("first source validation state = %v", got)
	}
}

type recordingExecutor struct {
	arguments [][]any
	err       error
	failAt    int
}

func (executor *recordingExecutor) Exec(_ context.Context, _ string, arguments ...any) (pgconn.CommandTag, error) {
	executor.arguments = append(executor.arguments, arguments)
	if executor.err != nil && (executor.failAt == 0 || executor.failAt == len(executor.arguments)) {
		return pgconn.CommandTag{}, executor.err
	}
	return pgconn.CommandTag{}, nil
}
