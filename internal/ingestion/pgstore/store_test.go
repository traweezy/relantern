package pgstore

import (
	"testing"
	"time"
)

func TestNextPollAtIsStableAndBounded(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	for _, interval := range []time.Duration{5 * time.Minute, 15 * time.Minute, 7 * 24 * time.Hour} {
		first := nextPollAt(now, interval, "go-blog")
		second := nextPollAt(now, interval, "go-blog")
		if !first.Equal(second) {
			t.Fatal("poll jitter changed for the same endpoint")
		}
		if first.Before(now.Add(interval)) || first.After(now.Add(interval+30*time.Second)) {
			t.Fatalf("next poll %s is outside interval %s", first, interval)
		}
	}
}
