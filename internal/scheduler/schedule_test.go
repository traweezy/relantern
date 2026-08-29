package scheduler_test

import (
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/scheduler"
)

func TestNextOccurrenceUsesFirstFallBackInstant(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, scheduler.LocalTime{Hour: 1, Minute: 30})
	after := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC)

	got, err := scheduler.NextOccurrence(after, definition)
	if err != nil {
		t.Fatalf("NextOccurrence() error = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("NextOccurrence() = %s, want first fall-back instant %s", got, want)
	}
}

func TestNextOccurrenceMovesSpringGapToFirstValidMinute(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, scheduler.LocalTime{Hour: 2, Minute: 30})
	after := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC)

	got, err := scheduler.NextOccurrence(after, definition)
	if err != nil {
		t.Fatalf("NextOccurrence() error = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("NextOccurrence() = %s, want first post-gap minute %s", got, want)
	}
}

func TestDailyEightAMProducesOneLocalDateOccurrenceAcrossTenYears(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, scheduler.LocalTime{Hour: 8})
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	for year := 2026; year < 2036; year++ {
		for _, date := range []time.Time{secondSunday(year, time.March), firstSunday(year, time.November)} {
			after := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location).Add(-time.Second)
			got, nextErr := scheduler.NextOccurrence(after, definition)
			if nextErr != nil {
				t.Fatalf("year %d: NextOccurrence() error = %v", year, nextErr)
			}
			local := got.In(location)
			if local.Year() != date.Year() || local.YearDay() != date.YearDay() || local.Hour() != 8 {
				t.Fatalf("year %d: got local occurrence %s for transition date %s", year, local, date)
			}
		}
	}
}

func TestDSTGapAndFoldRulesHoldAcrossTenYears(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	fallBackDefinition := mustDefinition(t, scheduler.LocalTime{Hour: 1, Minute: 30})
	springForwardDefinition := mustDefinition(t, scheduler.LocalTime{Hour: 2, Minute: 30})

	for year := 2026; year < 2036; year++ {
		fallBackDate := firstSunday(year, time.November)
		fallBackAfter := time.Date(
			fallBackDate.Year(),
			fallBackDate.Month(),
			fallBackDate.Day(),
			0,
			0,
			0,
			0,
			location,
		).Add(-time.Second)
		fallBack, nextErr := scheduler.NextOccurrence(fallBackAfter, fallBackDefinition)
		if nextErr != nil {
			t.Fatalf("year %d fall back: NextOccurrence() error = %v", year, nextErr)
		}
		fallBackLocal := fallBack.In(location)
		_, fallBackOffset := fallBackLocal.Zone()
		if fallBackLocal.Hour() != 1 || fallBackLocal.Minute() != 30 || fallBackOffset != -4*60*60 {
			t.Fatalf("year %d: fall-back occurrence = %s offset %d", year, fallBackLocal, fallBackOffset)
		}

		springForwardDate := secondSunday(year, time.March)
		springForwardAfter := time.Date(
			springForwardDate.Year(),
			springForwardDate.Month(),
			springForwardDate.Day(),
			0,
			0,
			0,
			0,
			location,
		).Add(-time.Second)
		springForward, nextErr := scheduler.NextOccurrence(springForwardAfter, springForwardDefinition)
		if nextErr != nil {
			t.Fatalf("year %d spring forward: NextOccurrence() error = %v", year, nextErr)
		}
		springForwardLocal := springForward.In(location)
		if springForwardLocal.Hour() != 3 || springForwardLocal.Minute() != 0 {
			t.Fatalf("year %d: spring-forward occurrence = %s", year, springForwardLocal)
		}
	}
}

func mustDefinition(t *testing.T, localTime scheduler.LocalTime) scheduler.Definition {
	t.Helper()
	definition, err := scheduler.NewDefinition(
		"America/New_York",
		localTime,
		[]int16{1, 2, 3, 4, 5, 6, 7},
	)
	if err != nil {
		t.Fatalf("NewDefinition() error = %v", err)
	}
	return definition
}

func firstSunday(year int, month time.Month) time.Time {
	return nthSunday(year, month, 1)
}

func secondSunday(year int, month time.Month) time.Time {
	return nthSunday(year, month, 2)
}

func nthSunday(year int, month time.Month, occurrence int) time.Time {
	date := time.Date(year, month, 1, 12, 0, 0, 0, time.UTC)
	daysUntilSunday := (int(time.Sunday) - int(date.Weekday()) + 7) % 7
	return date.AddDate(0, 0, daysUntilSunday+(occurrence-1)*7)
}
