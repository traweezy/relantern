package clock_test

import (
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/clock"
)

func TestFixedReturnsUTC(t *testing.T) {
	t.Parallel()

	input := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	want := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)

	if got := clock.NewFixed(input).Now(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("Fixed.Now() = %s (%s), want %s (UTC)", got, got.Location(), want)
	}
}
