package jobqueue

import (
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/traweezy/relantern/internal/clock"
)

type ExponentialRetryPolicy struct {
	clock        clock.Clock
	initialDelay time.Duration
	maximumDelay time.Duration
}

func NewExponentialRetryPolicy(
	configuredClock clock.Clock,
	initialDelay time.Duration,
	maximumDelay time.Duration,
) ExponentialRetryPolicy {
	return ExponentialRetryPolicy{
		clock:        configuredClock,
		initialDelay: initialDelay,
		maximumDelay: maximumDelay,
	}
}

func (policy ExponentialRetryPolicy) NextRetry(job *rivertype.JobRow) time.Time {
	delay := policy.initialDelay
	for attempt := 1; attempt < job.Attempt && delay < policy.maximumDelay; attempt++ {
		if delay > policy.maximumDelay/2 {
			delay = policy.maximumDelay
			break
		}
		delay *= 2
	}
	if delay > policy.maximumDelay {
		delay = policy.maximumDelay
	}
	return policy.clock.Now().UTC().Add(delay)
}
