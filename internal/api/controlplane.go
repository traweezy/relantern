package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/controlplane"
)

type SourcesOutput struct {
	Body controlplane.SourcesSnapshot
}

type SourceOutput struct {
	Body controlplane.ManagedSource
}

type SourcePreferenceBody struct {
	Muted               bool    `json:"muted"`
	ExcludeFromDigest   bool    `json:"excludeFromDigest"`
	RelevanceAdjustment float64 `json:"relevanceAdjustment" minimum:"-1" maximum:"1"`
	ExpectedVersion     int64   `json:"expectedVersion" minimum:"0"`
}

type SourcePreferenceInput struct {
	Authorization string               `header:"Authorization"`
	UserID        string               `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	SourceID      string               `path:"sourceId" minLength:"1" maxLength:"255"`
	Body          SourcePreferenceBody `json:"body"`
}

type SourcePreferenceOutput struct {
	Body controlplane.SourcePreference
}

type SourceActionBody struct {
	Action string `json:"action" enum:"approve,pause,reject,resume,test"`
	Reason string `json:"reason" minLength:"3" maxLength:"1000"`
}

type SourceActionInput struct {
	Authorization string           `header:"Authorization"`
	UserID        string           `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	SourceID      string           `path:"sourceId" minLength:"1" maxLength:"255"`
	Body          SourceActionBody `json:"body"`
}

type SettingsOutput struct {
	Body controlplane.SettingsSnapshot
}

type SettingsBody struct {
	ExpectedVersion      int64                            `json:"expectedVersion" minimum:"1"`
	ProfileName          string                           `json:"profileName" minLength:"1" maxLength:"120"`
	ProfileSummary       string                           `json:"profileSummary" maxLength:"2000"`
	Topics               []controlplane.InterestTopic     `json:"topics" minItems:"1" maxItems:"50"`
	Technologies         []controlplane.WatchedTechnology `json:"technologies" maxItems:"100"`
	Timezone             string                           `json:"timezone" minLength:"1" maxLength:"255"`
	QuietHoursStart      string                           `json:"quietHoursStart" minLength:"5" maxLength:"5"`
	QuietHoursEnd        string                           `json:"quietHoursEnd" minLength:"5" maxLength:"5"`
	CriticalAlertsBypass bool                             `json:"criticalAlertsBypass"`
	MonthlySoftBudgetUSD string                           `json:"monthlySoftBudgetUsd" minLength:"1" maxLength:"20"`
	MonthlyHardBudgetUSD string                           `json:"monthlyHardBudgetUsd" minLength:"1" maxLength:"20"`
	RawRetentionDays     int                              `json:"rawRetentionDays" minimum:"7" maximum:"3650"`
	AuditRetentionDays   int                              `json:"auditRetentionDays" minimum:"30" maximum:"3650"`
}

type SettingsInput struct {
	Authorization string       `header:"Authorization"`
	UserID        string       `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          SettingsBody `json:"body"`
}

type ScheduleBody struct {
	ExpectedVersion        int64    `json:"expectedVersion" minimum:"1"`
	Timezone               string   `json:"timezone" minLength:"1" maxLength:"255"`
	LocalTime              string   `json:"localTime" minLength:"5" maxLength:"5"`
	DaysOfWeek             []int16  `json:"daysOfWeek" minItems:"1" maxItems:"7"`
	Enabled                bool     `json:"enabled"`
	CatchupPolicy          string   `json:"catchupPolicy" enum:"catch_up,skip"`
	CatchupGraceMinutes    int      `json:"catchupGraceMinutes" minimum:"0" maximum:"10080"`
	WeekendMode            string   `json:"weekendMode" enum:"normal,weekly_only,off"`
	MaximumItems           int      `json:"maximumItems" enum:"5,10,15,20"`
	MinimumScore           float64  `json:"minimumScore" minimum:"0" maximum:"1"`
	IncludeComingSoon      bool     `json:"includeComingSoon"`
	IncludeRadarCandidates bool     `json:"includeRadarCandidates"`
	IncludeLaterReminders  bool     `json:"includeLaterReminders"`
	EmptyBehavior          string   `json:"emptyBehavior" enum:"send_nothing,all_clear,dashboard_only"`
	Channels               []string `json:"channels" minItems:"1" maxItems:"3"`
}

type ScheduleInput struct {
	Authorization string       `header:"Authorization"`
	UserID        string       `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	ScheduleID    string       `path:"scheduleId" minLength:"36" maxLength:"36"`
	Body          ScheduleBody `json:"body"`
}

type ScheduleOutput struct {
	Body controlplane.ScheduleDefinition
}

type ScheduleActionBody struct {
	Action         string     `json:"action" enum:"pause,resume,run_now,skip_next"`
	Reason         string     `json:"reason" minLength:"3" maxLength:"1000"`
	IdempotencyKey string     `json:"idempotencyKey,omitempty" maxLength:"200"`
	PausedUntil    *time.Time `json:"pausedUntil,omitempty"`
}

type ScheduleActionInput struct {
	Authorization string             `header:"Authorization"`
	UserID        string             `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	ScheduleID    string             `path:"scheduleId" minLength:"36" maxLength:"36"`
	Body          ScheduleActionBody `json:"body"`
}

type ScheduleActionOutput struct {
	Body controlplane.ScheduleActionResult
}

type SchedulePreviewInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	ScheduleID    string `path:"scheduleId" minLength:"36" maxLength:"36"`
}

type SchedulePreviewOutput struct {
	Body controlplane.SchedulePreview
}

type OperationsOutput struct {
	Body controlplane.OperationsSnapshot
}

func registerControlPlane(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "list-sources", Method: http.MethodGet, Path: "/api/v1/sources",
		Summary: "List owner source registry health and preferences", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*SourcesOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.Sources(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "list sources", err)
		}
		return &SourcesOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-source-preference", Method: http.MethodPut,
		Path:    "/api/v1/sources/{sourceId}/preference",
		Summary: "Update an owner source ranking and delivery preference", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *SourcePreferenceInput) (*SourcePreferenceOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.UpdateSourcePreference(ctx, controlplane.UpdateSourcePreferenceRequest{
			UserID: input.UserID, SourceID: input.SourceID,
			Muted: input.Body.Muted, ExcludeFromDigest: input.Body.ExcludeFromDigest,
			RelevanceAdjustment: input.Body.RelevanceAdjustment, ExpectedVersion: input.Body.ExpectedVersion,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "update source preference", err)
		}
		return &SourcePreferenceOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "act-on-source", Method: http.MethodPost,
		Path:    "/api/v1/sources/{sourceId}/actions",
		Summary: "Test, review, pause, or resume a source", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *SourceActionInput) (*SourceOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.ActOnSource(ctx, controlplane.SourceActionRequest{
			UserID: input.UserID, SourceID: input.SourceID,
			Action: input.Body.Action, Reason: input.Body.Reason,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "act on source", err)
		}
		return &SourceOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-settings", Method: http.MethodGet, Path: "/api/v1/settings",
		Summary: "Get private owner profile and schedule settings", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*SettingsOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.Settings(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "get settings", err)
		}
		return &SettingsOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-settings", Method: http.MethodPut, Path: "/api/v1/settings",
		Summary: "Update private owner profile, stack, budget, and retention settings", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *SettingsInput) (*SettingsOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.UpdateSettings(ctx, controlplane.UpdateSettingsRequest{
			UserID: input.UserID, ExpectedVersion: input.Body.ExpectedVersion,
			ProfileName: input.Body.ProfileName, ProfileSummary: input.Body.ProfileSummary,
			Topics: input.Body.Topics, Technologies: input.Body.Technologies,
			Timezone: input.Body.Timezone, QuietHoursStart: input.Body.QuietHoursStart,
			QuietHoursEnd: input.Body.QuietHoursEnd, CriticalAlertsBypass: input.Body.CriticalAlertsBypass,
			MonthlySoftBudgetUSD: input.Body.MonthlySoftBudgetUSD,
			MonthlyHardBudgetUSD: input.Body.MonthlyHardBudgetUSD,
			RawRetentionDays:     input.Body.RawRetentionDays,
			AuditRetentionDays:   input.Body.AuditRetentionDays,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "update settings", err)
		}
		return &SettingsOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-schedule", Method: http.MethodPut,
		Path:    "/api/v1/settings/schedules/{scheduleId}",
		Summary: "Update a durable owner schedule definition", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *ScheduleInput) (*ScheduleOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.UpdateSchedule(ctx, controlplane.UpdateScheduleRequest{
			UserID: input.UserID, ScheduleID: input.ScheduleID,
			ExpectedVersion: input.Body.ExpectedVersion, Timezone: input.Body.Timezone,
			LocalTime: input.Body.LocalTime, DaysOfWeek: input.Body.DaysOfWeek,
			Enabled: input.Body.Enabled, CatchupPolicy: input.Body.CatchupPolicy,
			CatchupGraceMinutes: input.Body.CatchupGraceMinutes, WeekendMode: input.Body.WeekendMode,
			MaximumItems: input.Body.MaximumItems, MinimumScore: input.Body.MinimumScore,
			IncludeComingSoon:      input.Body.IncludeComingSoon,
			IncludeRadarCandidates: input.Body.IncludeRadarCandidates,
			IncludeLaterReminders:  input.Body.IncludeLaterReminders,
			EmptyBehavior:          input.Body.EmptyBehavior, Channels: input.Body.Channels,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "update schedule", err)
		}
		return &ScheduleOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "act-on-schedule", Method: http.MethodPost,
		Path:    "/api/v1/settings/schedules/{scheduleId}/actions",
		Summary: "Preview-run, skip, pause, or resume a durable owner schedule", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *ScheduleActionInput) (*ScheduleActionOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.ActOnSchedule(ctx, controlplane.ScheduleActionRequest{
			UserID: input.UserID, ScheduleID: input.ScheduleID,
			Action: input.Body.Action, Reason: input.Body.Reason,
			IdempotencyKey: input.Body.IdempotencyKey, PausedUntil: input.Body.PausedUntil,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "act on schedule", err)
		}
		return &ScheduleActionOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "preview-schedule", Method: http.MethodGet,
		Path:    "/api/v1/settings/schedules/{scheduleId}/preview",
		Summary: "Preview the next owner schedule without delivery", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *SchedulePreviewInput) (*SchedulePreviewOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.PreviewSchedule(ctx, input.UserID, input.ScheduleID, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "preview schedule", err)
		}
		return &SchedulePreviewOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-operations", Method: http.MethodGet, Path: "/api/v1/operations",
		Summary: "Get bounded private queue, source, deployment, and schedule operations", Tags: []string{"control-plane"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*OperationsOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.controlPlane != nil); err != nil {
			return nil, err
		}
		result, err := configuration.controlPlane.Operations(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapControlPlaneError(ctx, logger, "get operations", err)
		}
		return &OperationsOutput{Body: result}, nil
	})
}

func mapControlPlaneError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	switch {
	case errors.Is(err, controlplane.ErrInvalid):
		return huma.Error400BadRequest("The control-plane request is invalid.")
	case errors.Is(err, controlplane.ErrNotFound):
		return huma.Error404NotFound("The requested control-plane resource was not found.")
	case errors.Is(err, controlplane.ErrConflict):
		return huma.Error409Conflict("The control-plane resource changed or cannot enter that state; refresh and try again.")
	default:
		logger.ErrorContext(ctx, operation+" failed", "error", err)
		return huma.Error500InternalServerError("The private control-plane service is unavailable.")
	}
}
