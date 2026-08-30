package retention_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/retention"
)

type repositoryFixture struct {
	completed  retention.Counts
	failedWith retention.Counts
	failed     bool
	raw        []retention.ObjectCandidate
	normalized []retention.ObjectCandidate
}

func (fixture *repositoryFixture) Start(context.Context, string, retention.Policy, time.Time) (string, retention.Counts, bool, error) {
	return "run-1", retention.Counts{}, true, nil
}
func (fixture *repositoryFixture) RawCandidates(context.Context, time.Time, int) ([]retention.ObjectCandidate, error) {
	candidates := fixture.raw
	fixture.raw = nil
	return candidates, nil
}
func (fixture *repositoryFixture) MarkRawPruned(context.Context, retention.ObjectCandidate, time.Time) (bool, error) {
	return true, nil
}
func (fixture *repositoryFixture) NormalizedCandidates(context.Context, time.Time, int) ([]retention.ObjectCandidate, error) {
	candidates := fixture.normalized
	fixture.normalized = nil
	return candidates, nil
}
func (fixture *repositoryFixture) MarkNormalizedPruned(context.Context, retention.ObjectCandidate, time.Time) (bool, error) {
	return true, nil
}
func (*repositoryFixture) PruneOperational(context.Context, retention.Policy, time.Time) (retention.Counts, error) {
	return retention.Counts{FetchAttemptsPruned: 2, OutboxEventsPruned: 3}, nil
}
func (fixture *repositoryFixture) Complete(_ context.Context, _ string, counts retention.Counts, _ time.Time) error {
	fixture.completed = counts
	return nil
}
func (fixture *repositoryFixture) Fail(_ context.Context, _ string, _ string, counts retention.Counts, _ time.Time) error {
	fixture.failed = true
	fixture.failedWith = counts
	return nil
}

type objectFixture struct {
	deleted []string
	err     error
	failOn  string
}

func (fixture *objectFixture) Delete(_ context.Context, key string) error {
	fixture.deleted = append(fixture.deleted, key)
	if fixture.err != nil && (fixture.failOn == "" || fixture.failOn == key) {
		return fixture.err
	}
	return nil
}

func TestRunnerPrunesBoundedObjectsAndOperationalRows(t *testing.T) {
	repository := &repositoryFixture{
		raw:        []retention.ObjectCandidate{{ID: "raw", Key: "raw/source/body"}},
		normalized: []retention.ObjectCandidate{{ID: "revision", Key: "normalized/source/body"}},
	}
	objects := &objectFixture{}
	runner, err := retention.NewRunner(repository, objects, retention.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	counts, err := runner.Run(context.Background(), time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects.deleted) != 2 || counts.RawObjectsPruned != 1 || counts.NormalizedObjectsPruned != 1 ||
		counts.FetchAttemptsPruned != 2 || counts.OutboxEventsPruned != 3 || repository.completed != counts {
		t.Fatalf("Run() = %+v, deleted = %v", counts, objects.deleted)
	}
}

func TestRunnerRecordsFailureWithoutContinuing(t *testing.T) {
	repository := &repositoryFixture{
		raw:        []retention.ObjectCandidate{{ID: "raw", Key: "raw/source/body"}},
		normalized: []retention.ObjectCandidate{{ID: "revision", Key: "normalized/source/body"}},
	}
	objects := &objectFixture{}
	runner, err := retention.NewRunner(repository, objects, retention.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	objects.err = errors.New("unavailable")
	objects.failOn = "normalized/source/body"
	if _, err := runner.Run(context.Background(), time.Now()); err == nil || !repository.failed {
		t.Fatalf("Run() error = %v, failed recorded = %t", err, repository.failed)
	}
	if repository.failedWith.RawObjectsPruned != 1 || repository.failedWith.NormalizedObjectsPruned != 0 {
		t.Fatalf("failure counts = %+v, want one completed raw prune", repository.failedWith)
	}
}

func TestPolicyRejectsUnboundedBatches(t *testing.T) {
	policy := retention.DefaultPolicy()
	policy.BatchSize = 1001
	if _, err := retention.NewRunner(&repositoryFixture{}, &objectFixture{}, policy); err == nil {
		t.Fatal("NewRunner() accepted an unbounded batch")
	}
}
