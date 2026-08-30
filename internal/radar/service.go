package radar

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid radar request")
	ErrNotFound = errors.New("radar resource not found")
	ErrConflict = errors.New("radar resource changed")
)

type Store interface {
	Snapshot(context.Context, string, time.Time) (Snapshot, error)
	QueueDiscovery(context.Context, QueueDiscoveryRequest, time.Time) (DiscoveryRun, error)
	Decide(context.Context, DecisionRequest, time.Time) (Candidate, error)
}

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("radar store is required")
	}
	return &Service{store: store}, nil
}

func (service *Service) Snapshot(ctx context.Context, userID string, now time.Time) (Snapshot, error) {
	if strings.TrimSpace(userID) == "" || now.IsZero() {
		return Snapshot{}, ErrInvalid
	}
	return service.store.Snapshot(ctx, userID, now.UTC())
}

func (service *Service) QueueDiscovery(
	ctx context.Context,
	request QueueDiscoveryRequest,
	now time.Time,
) (DiscoveryRun, error) {
	if strings.TrimSpace(request.UserID) == "" || request.TriggerType != "owner" ||
		len(request.IdempotencyKey) < 16 || len(request.IdempotencyKey) > 200 || now.IsZero() {
		return DiscoveryRun{}, ErrInvalid
	}
	return service.store.QueueDiscovery(ctx, request, now.UTC())
}

func (service *Service) Decide(
	ctx context.Context,
	request DecisionRequest,
	now time.Time,
) (Candidate, error) {
	request.Rationale = strings.TrimSpace(request.Rationale)
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.CandidateID) == "" ||
		request.ExpectedVersion < 1 || !validState(request.State) ||
		len(request.Rationale) < 3 || len(request.Rationale) > 4000 ||
		!request.ReviewAt.After(now.Add(time.Minute)) || request.ReviewAt.After(now.Add(5*365*24*time.Hour)) ||
		!validStringSet(request.ApplicableProjectTypes, 20, 120) ||
		!validStringSet(request.CompatibilityRequirements, 50, 500) ||
		!validStringSet(request.ExitConditions, 50, 500) || !validEvidence(request.Evidence) {
		return Candidate{}, ErrInvalid
	}
	return service.store.Decide(ctx, request, now.UTC())
}

func validState(state State) bool {
	return slices.Contains([]State{StateAdopt, StateTrial, StateAssess, StateHold, StateReject}, state)
}

func validStringSet(values []string, maximumItems int, maximumLength int) bool {
	if len(values) < 1 || len(values) > maximumItems {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || len(trimmed) > maximumLength {
			return false
		}
		if _, exists := seen[trimmed]; exists {
			return false
		}
		seen[trimmed] = struct{}{}
	}
	return true
}

func validEvidence(evidence map[string]any) bool {
	if len(evidence) < 1 || len(evidence) > 50 {
		return false
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil || len(encodedEvidence) > 50_000 {
		return false
	}
	for key, value := range evidence {
		if len(strings.TrimSpace(key)) < 1 || len(key) > 120 {
			return false
		}
		if encoded, ok := value.(string); ok {
			if len(encoded) > 2000 {
				return false
			}
			if strings.HasSuffix(strings.ToLower(key), "url") {
				parsed, err := url.Parse(encoded)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
					return false
				}
			}
		}
	}
	return true
}
