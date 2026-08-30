package radar

import (
	"context"
	"testing"
	"time"
)

type fakeStore struct {
	decision DecisionRequest
	queued   QueueDiscoveryRequest
}

func (store *fakeStore) Snapshot(context.Context, string, time.Time) (Snapshot, error) {
	return Snapshot{}, nil
}

func (store *fakeStore) QueueDiscovery(_ context.Context, request QueueDiscoveryRequest, _ time.Time) (DiscoveryRun, error) {
	store.queued = request
	return DiscoveryRun{ID: "run", State: "queued"}, nil
}

func (store *fakeStore) Decide(_ context.Context, request DecisionRequest, _ time.Time) (Candidate, error) {
	store.decision = request
	return Candidate{ID: request.CandidateID, CurrentState: request.State}, nil
}

func TestServiceRequiresOwnerDecisionContext(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	request := DecisionRequest{
		UserID: "owner", CandidateID: "candidate", ExpectedVersion: 1, State: StateTrial,
		Rationale: "Bounded trial with a measured rollback.", Evidence: map[string]any{"url": "https://example.com/evidence"},
		ReviewAt: now.Add(30 * 24 * time.Hour), ApplicableProjectTypes: []string{"web"},
		CompatibilityRequirements: []string{"Node 26"}, ExitConditions: []string{"Rollback on regression"},
	}
	if _, err := service.Decide(context.Background(), request, now); err != nil {
		t.Fatal(err)
	}
	if store.decision.State != StateTrial {
		t.Fatalf("decision state = %s", store.decision.State)
	}
	request.ExitConditions = nil
	if _, err := service.Decide(context.Background(), request, now); err != ErrInvalid {
		t.Fatalf("missing exit conditions error = %v", err)
	}
}
