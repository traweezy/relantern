package alert

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/scheduler"
)

type QuietHours struct {
	Timezone string
	Start    string
	End      string
	Bypass   bool
}

// DeliveryAt keeps dashboard admission immediate while deferring external
// channels to the next owner-local quiet-hours end when bypass is disabled.
func DeliveryAt(now time.Time, quiet QuietHours) (time.Time, error) {
	if now.IsZero() || quiet.Timezone == "" {
		return time.Time{}, errors.New("delivery time and owner timezone are required")
	}
	start, ok := parseClock(quiet.Start)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid quiet-hours start %q", quiet.Start)
	}
	end, ok := parseClock(quiet.End)
	if !ok || start == end {
		return time.Time{}, fmt.Errorf("invalid quiet-hours end %q", quiet.End)
	}
	location, err := time.LoadLocation(quiet.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load owner timezone: %w", err)
	}
	if quiet.Bypass {
		return now.UTC(), nil
	}
	local := now.In(location)
	minute := local.Hour()*60 + local.Minute()
	inQuietHours := false
	if start < end {
		inQuietHours = minute >= start && minute < end
	} else {
		inQuietHours = minute >= start || minute < end
	}
	if !inQuietHours {
		return now.UTC(), nil
	}
	definition, err := scheduler.NewDefinition(quiet.Timezone,
		scheduler.LocalTime{Hour: end / 60, Minute: end % 60},
		[]int16{1, 2, 3, 4, 5, 6, 7})
	if err != nil {
		return time.Time{}, err
	}
	return scheduler.NextOccurrence(now, definition)
}

func parseClock(value string) (int, bool) {
	parts := strings.Split(value, ":")
	if len(value) != 5 || len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}
