package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/radar"
)

type RadarSnapshotOutput struct {
	Body radar.Snapshot
}

type RadarDiscoveryBody struct {
	IdempotencyKey string `json:"idempotencyKey" minLength:"16" maxLength:"200"`
}

type RadarDiscoveryInput struct {
	Authorization string             `header:"Authorization"`
	UserID        string             `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          RadarDiscoveryBody `json:"body"`
}

type RadarDiscoveryOutput struct {
	Body radar.DiscoveryRun
}

type RadarDecisionBody struct {
	ExpectedVersion           int64          `json:"expectedVersion" minimum:"1"`
	State                     radar.State    `json:"state" enum:"adopt,trial,assess,hold,reject"`
	Rationale                 string         `json:"rationale" minLength:"3" maxLength:"4000"`
	Evidence                  map[string]any `json:"evidence" minProperties:"1" maxProperties:"50"`
	ReviewAt                  time.Time      `json:"reviewAt"`
	ApplicableProjectTypes    []string       `json:"applicableProjectTypes" minItems:"1" maxItems:"20"`
	CompatibilityRequirements []string       `json:"compatibilityRequirements" minItems:"1" maxItems:"50"`
	ExitConditions            []string       `json:"exitConditions" minItems:"1" maxItems:"50"`
}

type RadarDecisionInput struct {
	Authorization string            `header:"Authorization"`
	UserID        string            `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	CandidateID   string            `path:"candidateId" minLength:"36" maxLength:"36"`
	Body          RadarDecisionBody `json:"body"`
}

type RadarCandidateOutput struct {
	Body radar.Candidate
}

func registerRadar(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "get-radar", Method: http.MethodGet, Path: "/api/v1/radar",
		Summary: "Get owner Radar candidates, comparisons, decisions, and review dates", Tags: []string{"radar"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*RadarSnapshotOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.radar != nil); err != nil {
			return nil, err
		}
		result, err := configuration.radar.Snapshot(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapRadarError(ctx, logger, "get Radar", err)
		}
		return &RadarSnapshotOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "queue-radar-discovery", Method: http.MethodPost, Path: "/api/v1/radar/discovery-runs",
		Summary: "Queue an owner-requested Radar discovery over ingested evidence", Tags: []string{"radar"},
	}, func(ctx context.Context, input *RadarDiscoveryInput) (*RadarDiscoveryOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.radar != nil); err != nil {
			return nil, err
		}
		result, err := configuration.radar.QueueDiscovery(ctx, radar.QueueDiscoveryRequest{
			UserID: input.UserID, TriggerType: "owner", IdempotencyKey: input.Body.IdempotencyKey,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapRadarError(ctx, logger, "queue Radar discovery", err)
		}
		return &RadarDiscoveryOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "record-radar-decision", Method: http.MethodPost,
		Path:    "/api/v1/radar/candidates/{candidateId}/decisions",
		Summary: "Record the owner's explicit Radar decision and review contract", Tags: []string{"radar"},
	}, func(ctx context.Context, input *RadarDecisionInput) (*RadarCandidateOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.radar != nil); err != nil {
			return nil, err
		}
		result, err := configuration.radar.Decide(ctx, radar.DecisionRequest{
			UserID: input.UserID, CandidateID: input.CandidateID,
			ExpectedVersion: input.Body.ExpectedVersion, State: input.Body.State,
			Rationale: input.Body.Rationale, Evidence: input.Body.Evidence,
			ReviewAt: input.Body.ReviewAt, ApplicableProjectTypes: input.Body.ApplicableProjectTypes,
			CompatibilityRequirements: input.Body.CompatibilityRequirements,
			ExitConditions:            input.Body.ExitConditions,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapRadarError(ctx, logger, "record Radar decision", err)
		}
		return &RadarCandidateOutput{Body: result}, nil
	})
}

func mapRadarError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	switch {
	case errors.Is(err, radar.ErrInvalid):
		return huma.Error400BadRequest("The Radar request is invalid.")
	case errors.Is(err, radar.ErrNotFound):
		return huma.Error404NotFound("The requested Radar resource was not found.")
	case errors.Is(err, radar.ErrConflict):
		return huma.Error409Conflict("The Radar candidate changed; refresh and try again.")
	default:
		logger.ErrorContext(ctx, operation+" failed", "error", err)
		return huma.Error500InternalServerError("The private Radar service is unavailable.")
	}
}
