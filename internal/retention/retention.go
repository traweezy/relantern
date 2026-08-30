package retention

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const maximumBatches = 10

var ErrBusy = errors.New("a retention run is already active")

type Policy struct {
	RawSnapshots       time.Duration
	NormalizedRevision time.Duration
	OperationalRows    time.Duration
	OutboxReplay       time.Duration
	MutationSnapshots  time.Duration
	BatchSize          int
}

func DefaultPolicy() Policy {
	return Policy{
		RawSnapshots:       180 * 24 * time.Hour,
		NormalizedRevision: 365 * 24 * time.Hour,
		OperationalRows:    90 * 24 * time.Hour,
		OutboxReplay:       7 * 24 * time.Hour,
		MutationSnapshots:  30 * 24 * time.Hour,
		BatchSize:          100,
	}
}

func (policy Policy) Validate() error {
	if policy.RawSnapshots < 7*24*time.Hour || policy.RawSnapshots > 10*365*24*time.Hour {
		return errors.New("raw snapshot retention must be between 7 days and 10 years")
	}
	if policy.NormalizedRevision < policy.RawSnapshots || policy.NormalizedRevision > 10*365*24*time.Hour {
		return errors.New("normalized revision retention must be at least raw retention and at most 10 years")
	}
	if policy.OperationalRows < 7*24*time.Hour || policy.OperationalRows > 365*24*time.Hour {
		return errors.New("operational row retention must be between 7 and 365 days")
	}
	if policy.OutboxReplay < 24*time.Hour || policy.OutboxReplay > 30*24*time.Hour {
		return errors.New("outbox replay retention must be between 1 and 30 days")
	}
	if policy.MutationSnapshots < 30*24*time.Hour || policy.MutationSnapshots > 365*24*time.Hour {
		return errors.New("mutation snapshot retention must be between 30 and 365 days")
	}
	if policy.BatchSize < 1 || policy.BatchSize > 1000 {
		return errors.New("retention batch size must be between 1 and 1000")
	}
	return nil
}

type ObjectCandidate struct {
	ID  string
	Key string
}

type Counts struct {
	RawObjectsPruned        int64 `json:"rawObjectsPruned"`
	NormalizedObjectsPruned int64 `json:"normalizedObjectsPruned"`
	FetchAttemptsPruned     int64 `json:"fetchAttemptsPruned"`
	ParseAttemptsPruned     int64 `json:"parseAttemptsPruned"`
	WebhookEventsPruned     int64 `json:"webhookEventsPruned"`
	OutboxEventsPruned      int64 `json:"outboxEventsPruned"`
	MutationStatesCompacted int64 `json:"mutationStatesCompacted"`
}

func (counts Counts) Add(other Counts) Counts {
	counts.RawObjectsPruned += other.RawObjectsPruned
	counts.NormalizedObjectsPruned += other.NormalizedObjectsPruned
	counts.FetchAttemptsPruned += other.FetchAttemptsPruned
	counts.ParseAttemptsPruned += other.ParseAttemptsPruned
	counts.WebhookEventsPruned += other.WebhookEventsPruned
	counts.OutboxEventsPruned += other.OutboxEventsPruned
	counts.MutationStatesCompacted += other.MutationStatesCompacted
	return counts
}

type Repository interface {
	Start(context.Context, string, Policy, time.Time) (string, Counts, bool, error)
	RawCandidates(context.Context, time.Time, int) ([]ObjectCandidate, error)
	MarkRawPruned(context.Context, ObjectCandidate, time.Time) (bool, error)
	NormalizedCandidates(context.Context, time.Time, int) ([]ObjectCandidate, error)
	MarkNormalizedPruned(context.Context, ObjectCandidate, time.Time) (bool, error)
	PruneOperational(context.Context, Policy, time.Time) (Counts, error)
	Complete(context.Context, string, Counts, time.Time) error
	Fail(context.Context, string, string, Counts, time.Time) error
}

type ObjectStore interface {
	Delete(context.Context, string) error
}

type Runner struct {
	repository Repository
	objects    ObjectStore
	policy     Policy
}

func NewRunner(repository Repository, objects ObjectStore, policy Policy) (*Runner, error) {
	if repository == nil || objects == nil {
		return nil, errors.New("retention repository and object store are required")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Runner{repository: repository, objects: objects, policy: policy}, nil
}

func (runner *Runner) Run(ctx context.Context, now time.Time) (Counts, error) {
	now = now.UTC()
	idempotencyKey := "retention:" + now.Format(time.DateOnly)
	runID, counts, execute, err := runner.repository.Start(ctx, idempotencyKey, runner.policy, now)
	if err != nil || !execute {
		return counts, err
	}
	fail := func(cause error) (Counts, error) {
		failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if recordErr := runner.repository.Fail(failureContext, runID, "retention_run_failed", counts, now); recordErr != nil {
			return counts, fmt.Errorf("%w; record retention failure: %v", cause, recordErr)
		}
		return counts, cause
	}
	for batch := 0; batch < maximumBatches; batch++ {
		candidates, candidateErr := runner.repository.RawCandidates(ctx, now.Add(-runner.policy.RawSnapshots), runner.policy.BatchSize)
		if candidateErr != nil {
			return fail(candidateErr)
		}
		for _, candidate := range candidates {
			if deleteErr := runner.objects.Delete(ctx, candidate.Key); deleteErr != nil {
				return fail(fmt.Errorf("delete raw object: %w", deleteErr))
			}
			marked, markErr := runner.repository.MarkRawPruned(ctx, candidate, now)
			if markErr != nil {
				return fail(markErr)
			}
			if marked {
				counts.RawObjectsPruned++
			}
		}
		if len(candidates) < runner.policy.BatchSize {
			break
		}
	}
	for batch := 0; batch < maximumBatches; batch++ {
		candidates, candidateErr := runner.repository.NormalizedCandidates(ctx, now.Add(-runner.policy.NormalizedRevision), runner.policy.BatchSize)
		if candidateErr != nil {
			return fail(candidateErr)
		}
		for _, candidate := range candidates {
			if deleteErr := runner.objects.Delete(ctx, candidate.Key); deleteErr != nil {
				return fail(fmt.Errorf("delete normalized object: %w", deleteErr))
			}
			marked, markErr := runner.repository.MarkNormalizedPruned(ctx, candidate, now)
			if markErr != nil {
				return fail(markErr)
			}
			if marked {
				counts.NormalizedObjectsPruned++
			}
		}
		if len(candidates) < runner.policy.BatchSize {
			break
		}
	}
	operational, err := runner.repository.PruneOperational(ctx, runner.policy, now)
	if err != nil {
		return fail(err)
	}
	counts = counts.Add(operational)
	if err := runner.repository.Complete(ctx, runID, counts, now); err != nil {
		return fail(err)
	}
	return counts, nil
}
