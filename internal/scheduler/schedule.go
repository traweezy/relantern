package scheduler

import (
	"errors"
	"fmt"
	"time"
)

const maxScheduleSearchDays = 370

type LocalTime struct {
	Hour   int
	Minute int
}

type Definition struct {
	Timezone    string
	LocalTime   LocalTime
	ISOWeekdays map[int]struct{}
}

func NewDefinition(timezone string, localTime LocalTime, weekdays []int16) (Definition, error) {
	if timezone == "" {
		return Definition{}, errors.New("timezone is required")
	}
	if localTime.Hour < 0 || localTime.Hour > 23 || localTime.Minute < 0 || localTime.Minute > 59 {
		return Definition{}, errors.New("local time is outside the valid clock range")
	}
	if len(weekdays) == 0 {
		return Definition{}, errors.New("at least one ISO weekday is required")
	}

	weekdaySet := make(map[int]struct{}, len(weekdays))
	for _, weekday := range weekdays {
		if weekday < 1 || weekday > 7 {
			return Definition{}, fmt.Errorf("ISO weekday must be from 1 through 7: %d", weekday)
		}
		weekdaySet[int(weekday)] = struct{}{}
	}

	if _, err := time.LoadLocation(timezone); err != nil {
		return Definition{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	return Definition{Timezone: timezone, LocalTime: localTime, ISOWeekdays: weekdaySet}, nil
}

func NextOccurrence(after time.Time, definition Definition) (time.Time, error) {
	location, err := time.LoadLocation(definition.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %q: %w", definition.Timezone, err)
	}

	localAfter := after.In(location)
	for dayOffset := range maxScheduleSearchDays {
		localDate := time.Date(
			localAfter.Year(),
			localAfter.Month(),
			localAfter.Day()+dayOffset,
			12,
			0,
			0,
			0,
			location,
		)
		if _, enabled := definition.ISOWeekdays[isoWeekday(localDate.Weekday())]; !enabled {
			continue
		}

		candidate, err := resolveLocalMinute(localDate, definition.LocalTime, location)
		if err != nil {
			return time.Time{}, err
		}
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
	}

	return time.Time{}, errors.New("no schedule occurrence found within one year")
}

func resolveLocalMinute(localDate time.Time, requested LocalTime, location *time.Location) (time.Time, error) {
	year, month, day := localDate.Date()
	utcStart := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Add(-14 * time.Hour)
	utcEnd := utcStart.Add(52 * time.Hour)
	requestedMinute := requested.Hour*60 + requested.Minute

	var firstAfterGap time.Time
	firstAfterGapMinute := 24 * 60
	for candidate := utcStart; !candidate.After(utcEnd); candidate = candidate.Add(time.Minute) {
		localized := candidate.In(location)
		candidateYear, candidateMonth, candidateDay := localized.Date()
		if candidateYear != year || candidateMonth != month || candidateDay != day {
			continue
		}

		candidateMinute := localized.Hour()*60 + localized.Minute()
		if candidateMinute == requestedMinute {
			return candidate, nil
		}
		if candidateMinute > requestedMinute && candidateMinute < firstAfterGapMinute {
			firstAfterGap = candidate
			firstAfterGapMinute = candidateMinute
		}
	}

	if !firstAfterGap.IsZero() {
		return firstAfterGap, nil
	}
	return time.Time{}, fmt.Errorf(
		"cannot resolve %04d-%02d-%02d %02d:%02d in %s",
		year,
		month,
		day,
		requested.Hour,
		requested.Minute,
		location,
	)
}

func isoWeekday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}
