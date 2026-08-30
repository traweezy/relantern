package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

const fixtureUserID = "01991234-5678-7abc-8def-0123456789ab"
const fixtureScheduleID = "01991234-5678-7abc-8def-0123456789ac"

type fixtureStore struct {
	schedule      ScheduleDefinition
	settings      UpdateSettingsRequest
	scheduleWrite UpdateScheduleRequest
	action        ScheduleActionRequest
}

func (store *fixtureStore) Sources(context.Context, string, time.Time) (SourcesSnapshot, error) {
	return SourcesSnapshot{Sources: []ManagedSource{}}, nil
}

func (store *fixtureStore) UpdateSourcePreference(
	context.Context,
	UpdateSourcePreferenceRequest,
	time.Time,
) (SourcePreference, error) {
	return SourcePreference{}, nil
}

func (store *fixtureStore) ActOnSource(
	context.Context,
	SourceActionRequest,
	time.Time,
) (ManagedSource, error) {
	return ManagedSource{}, nil
}

func (store *fixtureStore) Settings(context.Context, string, time.Time) (SettingsSnapshot, error) {
	return SettingsSnapshot{}, nil
}

func (store *fixtureStore) UpdateSettings(
	_ context.Context,
	request UpdateSettingsRequest,
	_ time.Time,
) (SettingsSnapshot, error) {
	store.settings = request
	return SettingsSnapshot{}, nil
}

func (store *fixtureStore) Schedule(context.Context, string, string) (ScheduleDefinition, error) {
	if store.schedule.ID == "" {
		return ScheduleDefinition{}, ErrNotFound
	}
	return store.schedule, nil
}

func (store *fixtureStore) UpdateSchedule(
	_ context.Context,
	request UpdateScheduleRequest,
	_ time.Time,
) (ScheduleDefinition, error) {
	store.scheduleWrite = request
	return store.schedule, nil
}

func (store *fixtureStore) ActOnSchedule(
	_ context.Context,
	request ScheduleActionRequest,
	_ time.Time,
) (ScheduleActionResult, error) {
	store.action = request
	return ScheduleActionResult{Schedule: store.schedule}, nil
}

func (store *fixtureStore) PreviewSchedule(
	context.Context,
	string,
	string,
	time.Time,
) (SchedulePreview, error) {
	return SchedulePreview{}, nil
}

func (store *fixtureStore) Operations(
	context.Context,
	string,
	time.Time,
	DeploymentMetadata,
) (OperationsSnapshot, error) {
	return OperationsSnapshot{}, nil
}

func TestUpdateSettingsRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()
	service, err := NewService(&fixtureStore{}, DeploymentMetadata{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	base := validSettingsRequest()
	tests := []struct {
		name   string
		mutate func(*UpdateSettingsRequest)
	}{
		{name: "duplicate topics", mutate: func(request *UpdateSettingsRequest) {
			request.Topics = append(request.Topics, request.Topics[0])
		}},
		{name: "hard budget below soft", mutate: func(request *UpdateSettingsRequest) {
			request.MonthlySoftBudgetUSD = "50.00"
			request.MonthlyHardBudgetUSD = "25.00"
		}},
		{name: "equal quiet hours", mutate: func(request *UpdateSettingsRequest) {
			request.QuietHoursEnd = request.QuietHoursStart
		}},
		{name: "invalid timezone", mutate: func(request *UpdateSettingsRequest) {
			request.Timezone = "Mars/Olympus"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.Topics = append([]InterestTopic(nil), base.Topics...)
			test.mutate(&request)
			if _, updateErr := service.UpdateSettings(context.Background(), request, time.Now()); !errors.Is(updateErr, ErrInvalid) {
				t.Fatalf("UpdateSettings() error = %v, want ErrInvalid", updateErr)
			}
		})
	}
}

func TestUpdateScheduleCalculatesDSTSafeFutureOccurrence(t *testing.T) {
	t.Parallel()
	store := &fixtureStore{schedule: validSchedule()}
	service, err := NewService(store, DeploymentMetadata{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Date(2026, time.March, 8, 5, 0, 0, 0, time.UTC)
	request := validScheduleRequest()
	request.LocalTime = "02:30"
	if _, err := service.UpdateSchedule(context.Background(), request, now); err != nil {
		t.Fatalf("UpdateSchedule() error = %v", err)
	}
	want := time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC)
	if !store.scheduleWrite.NextDueAt.Equal(want) {
		t.Fatalf("NextDueAt = %s, want %s", store.scheduleWrite.NextDueAt, want)
	}
}

func TestPauseSkipsToFirstOccurrenceAfterPause(t *testing.T) {
	t.Parallel()
	store := &fixtureStore{schedule: validSchedule()}
	service, err := NewService(store, DeploymentMetadata{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	pausedUntil := time.Date(2026, time.August, 31, 16, 0, 0, 0, time.UTC)
	if _, err := service.ActOnSchedule(context.Background(), ScheduleActionRequest{
		UserID: fixtureUserID, ScheduleID: fixtureScheduleID,
		Action: "pause", Reason: "Owner travel", PausedUntil: &pausedUntil,
	}, now); err != nil {
		t.Fatalf("ActOnSchedule() error = %v", err)
	}
	want := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	if store.action.NextDueAt == nil || !store.action.NextDueAt.Equal(want) {
		t.Fatalf("pause NextDueAt = %v, want %s", store.action.NextDueAt, want)
	}
}

func TestRunNowRequiresStableIdempotencyKey(t *testing.T) {
	t.Parallel()
	store := &fixtureStore{schedule: validSchedule()}
	service, err := NewService(store, DeploymentMetadata{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.ActOnSchedule(context.Background(), ScheduleActionRequest{
		UserID: fixtureUserID, ScheduleID: fixtureScheduleID,
		Action: "run_now", Reason: "Preview current briefing", IdempotencyKey: "short",
	}, time.Now())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ActOnSchedule() error = %v, want ErrInvalid", err)
	}
}

func validSettingsRequest() UpdateSettingsRequest {
	return UpdateSettingsRequest{
		UserID: fixtureUserID, ExpectedVersion: 1,
		ProfileName: "Owner", ProfileSummary: "Backend and platform intelligence.",
		Topics: []InterestTopic{{TopicID: "go", Priority: 1, Weight: 1, Keywords: []string{}, Exclusions: []string{}}},
		Technologies: []WatchedTechnology{{
			Technology: "Go", PackageName: "go", CurrentVersion: "1.27.0",
			Status: "active", Source: "version-manifest",
		}},
		Timezone: "America/New_York", QuietHoursStart: "22:00", QuietHoursEnd: "07:00",
		CriticalAlertsBypass: true, MonthlySoftBudgetUSD: "25.00", MonthlyHardBudgetUSD: "50.00",
		RawRetentionDays: 90, AuditRetentionDays: 365,
	}
}

func validSchedule() ScheduleDefinition {
	return ScheduleDefinition{
		ID: fixtureScheduleID, ScheduleType: "daily_digest", Timezone: "America/New_York",
		LocalTime: "08:00", DaysOfWeek: []int16{1, 2, 3, 4, 5, 6, 7}, Enabled: true,
		CatchupPolicy: "catch_up", CatchupGraceMinutes: 360, NextDueAt: time.Now(),
		WeekendMode: "normal", MaximumItems: 10, MinimumScore: 0.5,
		IncludeComingSoon: true, IncludeRadarCandidates: true,
		EmptyBehavior: "dashboard_only", Channels: []string{"dashboard"}, Version: 1,
	}
}

func validScheduleRequest() UpdateScheduleRequest {
	schedule := validSchedule()
	return UpdateScheduleRequest{
		UserID: fixtureUserID, ScheduleID: schedule.ID, ExpectedVersion: schedule.Version,
		Timezone: schedule.Timezone, LocalTime: schedule.LocalTime, DaysOfWeek: schedule.DaysOfWeek,
		Enabled: schedule.Enabled, CatchupPolicy: schedule.CatchupPolicy,
		CatchupGraceMinutes: schedule.CatchupGraceMinutes, WeekendMode: schedule.WeekendMode,
		MaximumItems: schedule.MaximumItems, MinimumScore: schedule.MinimumScore,
		IncludeComingSoon:      schedule.IncludeComingSoon,
		IncludeRadarCandidates: schedule.IncludeRadarCandidates,
		IncludeLaterReminders:  schedule.IncludeLaterReminders,
		EmptyBehavior:          schedule.EmptyBehavior, Channels: schedule.Channels,
	}
}
