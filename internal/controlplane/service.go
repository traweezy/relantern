package controlplane

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/extraction"
	"github.com/traweezy/relantern/internal/scheduler"
)

var (
	ErrInvalid  = errors.New("invalid control-plane request")
	ErrNotFound = errors.New("control-plane resource not found")
	ErrConflict = errors.New("control-plane resource changed")
)

var topicIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

type Store interface {
	Sources(context.Context, string, time.Time) (SourcesSnapshot, error)
	UpdateSourcePreference(context.Context, UpdateSourcePreferenceRequest, time.Time) (SourcePreference, error)
	ActOnSource(context.Context, SourceActionRequest, time.Time) (ManagedSource, error)
	Settings(context.Context, string, time.Time) (SettingsSnapshot, error)
	UpdateSettings(context.Context, UpdateSettingsRequest, time.Time) (SettingsSnapshot, error)
	Schedule(context.Context, string, string) (ScheduleDefinition, error)
	UpdateSchedule(context.Context, UpdateScheduleRequest, time.Time) (ScheduleDefinition, error)
	ActOnSchedule(context.Context, ScheduleActionRequest, time.Time) (ScheduleActionResult, error)
	PreviewSchedule(context.Context, string, string, time.Time) (SchedulePreview, error)
	Operations(context.Context, string, time.Time, DeploymentMetadata) (OperationsSnapshot, error)
}

type Service struct {
	store      Store
	deployment DeploymentMetadata
}

func NewService(store Store, deployment DeploymentMetadata) (*Service, error) {
	if store == nil {
		return nil, errors.New("control-plane store is required")
	}
	return &Service{store: store, deployment: deployment}, nil
}

func (service *Service) Sources(ctx context.Context, userID string, now time.Time) (SourcesSnapshot, error) {
	if strings.TrimSpace(userID) == "" {
		return SourcesSnapshot{}, ErrInvalid
	}
	return service.store.Sources(ctx, userID, now.UTC())
}

func (service *Service) UpdateSourcePreference(
	ctx context.Context,
	request UpdateSourcePreferenceRequest,
	now time.Time,
) (SourcePreference, error) {
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.SourceID) == "" ||
		request.ExpectedVersion < 0 || math.IsNaN(request.RelevanceAdjustment) ||
		request.RelevanceAdjustment < -1 || request.RelevanceAdjustment > 1 {
		return SourcePreference{}, ErrInvalid
	}
	return service.store.UpdateSourcePreference(ctx, request, now.UTC())
}

func (service *Service) ActOnSource(
	ctx context.Context,
	request SourceActionRequest,
	now time.Time,
) (ManagedSource, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.SourceID) == "" ||
		!slices.Contains([]string{"approve", "pause", "reject", "resume", "test"}, request.Action) ||
		len(request.Reason) < 3 || len(request.Reason) > 1000 {
		return ManagedSource{}, ErrInvalid
	}
	return service.store.ActOnSource(ctx, request, now.UTC())
}

func (service *Service) Settings(ctx context.Context, userID string, now time.Time) (SettingsSnapshot, error) {
	if strings.TrimSpace(userID) == "" {
		return SettingsSnapshot{}, ErrInvalid
	}
	return service.store.Settings(ctx, userID, now.UTC())
}

func (service *Service) UpdateSettings(
	ctx context.Context,
	request UpdateSettingsRequest,
	now time.Time,
) (SettingsSnapshot, error) {
	if err := validateSettings(request); err != nil {
		return SettingsSnapshot{}, err
	}
	return service.store.UpdateSettings(ctx, request, now.UTC())
}

func (service *Service) UpdateSchedule(
	ctx context.Context,
	request UpdateScheduleRequest,
	now time.Time,
) (ScheduleDefinition, error) {
	definition, err := validateSchedule(request)
	if err != nil {
		return ScheduleDefinition{}, err
	}
	current, err := service.store.Schedule(ctx, request.UserID, request.ScheduleID)
	if err != nil {
		return ScheduleDefinition{}, err
	}
	nextSearchAt := now.UTC()
	if current.PausedUntil != nil && current.PausedUntil.After(nextSearchAt) {
		nextSearchAt = current.PausedUntil.UTC()
	}
	nextDueAt, err := scheduler.NextOccurrence(nextSearchAt, definition)
	if err != nil {
		return ScheduleDefinition{}, fmt.Errorf("%w: calculate next occurrence: %v", ErrInvalid, err)
	}
	request.NextDueAt = nextDueAt
	return service.store.UpdateSchedule(ctx, request, now.UTC())
}

func (service *Service) ActOnSchedule(
	ctx context.Context,
	request ScheduleActionRequest,
	now time.Time,
) (ScheduleActionResult, error) {
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.ScheduleID) == "" ||
		!slices.Contains([]string{"pause", "resume", "run_now", "skip_next"}, request.Action) ||
		len(strings.TrimSpace(request.Reason)) < 3 || len(request.Reason) > 1000 {
		return ScheduleActionResult{}, ErrInvalid
	}
	current, err := service.store.Schedule(ctx, request.UserID, request.ScheduleID)
	if err != nil {
		return ScheduleActionResult{}, err
	}
	definition, err := scheduleDefinition(current.Timezone, current.LocalTime, current.DaysOfWeek)
	if err != nil {
		return ScheduleActionResult{}, err
	}
	switch request.Action {
	case "pause":
		if request.PausedUntil == nil || !request.PausedUntil.After(now.Add(time.Minute)) ||
			request.PausedUntil.After(now.Add(366*24*time.Hour)) {
			return ScheduleActionResult{}, ErrInvalid
		}
		nextDueAt, nextErr := scheduler.NextOccurrence(request.PausedUntil.UTC(), definition)
		if nextErr != nil {
			return ScheduleActionResult{}, fmt.Errorf("%w: calculate post-pause occurrence: %v", ErrInvalid, nextErr)
		}
		request.NextDueAt = &nextDueAt
	case "resume":
		nextDueAt, nextErr := scheduler.NextOccurrence(now.UTC(), definition)
		if nextErr != nil {
			return ScheduleActionResult{}, fmt.Errorf("%w: calculate resumed occurrence: %v", ErrInvalid, nextErr)
		}
		request.NextDueAt = &nextDueAt
	case "run_now":
		if len(request.IdempotencyKey) < 16 || len(request.IdempotencyKey) > 200 {
			return ScheduleActionResult{}, ErrInvalid
		}
	}
	request.Reason = strings.TrimSpace(request.Reason)
	return service.store.ActOnSchedule(ctx, request, now.UTC())
}

func (service *Service) PreviewSchedule(
	ctx context.Context,
	userID string,
	scheduleID string,
	now time.Time,
) (SchedulePreview, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(scheduleID) == "" {
		return SchedulePreview{}, ErrInvalid
	}
	return service.store.PreviewSchedule(ctx, userID, scheduleID, now.UTC())
}

func (service *Service) Operations(ctx context.Context, userID string, now time.Time) (OperationsSnapshot, error) {
	if strings.TrimSpace(userID) == "" {
		return OperationsSnapshot{}, ErrInvalid
	}
	return service.store.Operations(ctx, userID, now.UTC(), service.deployment)
}

func validateSettings(request UpdateSettingsRequest) error {
	request.ProfileName = strings.TrimSpace(request.ProfileName)
	request.ProfileSummary = strings.TrimSpace(request.ProfileSummary)
	if strings.TrimSpace(request.UserID) == "" || request.ExpectedVersion < 1 ||
		len(request.ProfileName) < 1 || len(request.ProfileName) > 120 ||
		len(request.ProfileSummary) > 2000 || len(request.Topics) < 1 || len(request.Topics) > 50 ||
		len(request.Technologies) > 100 || request.RawRetentionDays < 7 || request.RawRetentionDays > 3650 ||
		request.AuditRetentionDays < 30 || request.AuditRetentionDays > 3650 {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return ErrInvalid
	}
	quietStart, err := parseClock(request.QuietHoursStart)
	if err != nil {
		return ErrInvalid
	}
	quietEnd, err := parseClock(request.QuietHoursEnd)
	if err != nil || quietStart == quietEnd {
		return ErrInvalid
	}
	soft, err := extraction.ParseUSD(request.MonthlySoftBudgetUSD)
	if err != nil {
		return ErrInvalid
	}
	hard, err := extraction.ParseUSD(request.MonthlyHardBudgetUSD)
	if err != nil || extraction.ValidateBudgetRange(soft, hard) != nil {
		return ErrInvalid
	}
	seenTopics := make(map[string]struct{}, len(request.Topics))
	for _, topic := range request.Topics {
		if !topicIDPattern.MatchString(topic.TopicID) || len(topic.TopicID) > 100 ||
			topic.Priority < 1 || topic.Priority > 5 || math.IsNaN(topic.Weight) ||
			topic.Weight < 0 || topic.Weight > 1 || len(topic.Keywords) > 50 || len(topic.Exclusions) > 50 {
			return ErrInvalid
		}
		if _, duplicate := seenTopics[topic.TopicID]; duplicate {
			return ErrInvalid
		}
		seenTopics[topic.TopicID] = struct{}{}
		if !validStringSet(topic.Keywords, 100) || !validStringSet(topic.Exclusions, 100) {
			return ErrInvalid
		}
	}
	seenPackages := make(map[string]struct{}, len(request.Technologies))
	for _, technology := range request.Technologies {
		if len(strings.TrimSpace(technology.Technology)) < 1 || len(technology.Technology) > 120 ||
			len(strings.TrimSpace(technology.PackageName)) < 1 || len(technology.PackageName) > 255 ||
			len(technology.CurrentVersion) > 100 || len(technology.VersionConstraint) > 200 ||
			!slices.Contains([]string{"active", "evaluating", "legacy", "planned"}, technology.Status) ||
			len(strings.TrimSpace(technology.Source)) < 1 || len(technology.Source) > 120 {
			return ErrInvalid
		}
		if _, duplicate := seenPackages[technology.PackageName]; duplicate {
			return ErrInvalid
		}
		seenPackages[technology.PackageName] = struct{}{}
	}
	return nil
}

func validateSchedule(request UpdateScheduleRequest) (scheduler.Definition, error) {
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.ScheduleID) == "" ||
		request.ExpectedVersion < 1 || !slices.Contains([]string{"catch_up", "skip"}, request.CatchupPolicy) ||
		request.CatchupGraceMinutes < 0 || request.CatchupGraceMinutes > 7*24*60 ||
		!slices.Contains([]string{"normal", "off", "weekly_only"}, request.WeekendMode) ||
		!slices.Contains([]int{5, 10, 15, 20}, request.MaximumItems) ||
		math.IsNaN(request.MinimumScore) || request.MinimumScore < 0 || request.MinimumScore > 1 ||
		!slices.Contains([]string{"all_clear", "dashboard_only", "send_nothing"}, request.EmptyBehavior) ||
		len(request.Channels) < 1 || len(request.Channels) > 3 || !validEnumSet(request.Channels, []string{"dashboard", "discord", "email"}) {
		return scheduler.Definition{}, ErrInvalid
	}
	return scheduleDefinition(request.Timezone, request.LocalTime, request.DaysOfWeek)
}

func scheduleDefinition(timezone string, localTime string, weekdays []int16) (scheduler.Definition, error) {
	parsed, err := parseClock(localTime)
	if err != nil {
		return scheduler.Definition{}, ErrInvalid
	}
	definition, err := scheduler.NewDefinition(timezone, scheduler.LocalTime{Hour: parsed.Hour(), Minute: parsed.Minute()}, weekdays)
	if err != nil {
		return scheduler.Definition{}, ErrInvalid
	}
	return definition, nil
}

func parseClock(value string) (time.Time, error) {
	return time.Parse("15:04", value)
}

func validStringSet(values []string, maximumLength int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || len(trimmed) > maximumLength {
			return false
		}
		if _, duplicate := seen[trimmed]; duplicate {
			return false
		}
		seen[trimmed] = struct{}{}
	}
	return true
}

func validEnumSet(values []string, allowed []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
