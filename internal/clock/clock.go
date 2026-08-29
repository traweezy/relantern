package clock

import "time"

// Clock makes scheduling deterministic and prevents direct wall-clock access in
// core scheduling code.
type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time {
	return time.Now().UTC()
}

type Fixed struct {
	now time.Time
}

func NewFixed(now time.Time) Fixed {
	return Fixed{now: now.UTC()}
}

func (clock Fixed) Now() time.Time {
	return clock.now
}
