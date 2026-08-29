package scheduler

import (
	"testing"
	"time"
)

func TestClassifyOccurrenceHonorsCatchupAndSkipPolicies(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		schedule    dueSchedule
		now         time.Time
		wantState   string
		wantTrigger string
		wantEnqueue bool
		wantSkipped bool
	}{
		{
			name:        "exact due time",
			schedule:    dueSchedule{catchupPolicy: "skip"},
			now:         scheduledFor,
			wantState:   "due",
			wantTrigger: "scheduled",
			wantEnqueue: true,
		},
		{
			name:        "owner skips next occurrence",
			schedule:    dueSchedule{skipNextAt: &scheduledFor},
			now:         scheduledFor,
			wantState:   "skipped",
			wantTrigger: "scheduled",
			wantSkipped: true,
		},
		{
			name:        "catch up inside grace",
			schedule:    dueSchedule{catchupPolicy: "catch_up", catchupGrace: 30 * time.Minute},
			now:         scheduledFor.Add(20 * time.Minute),
			wantState:   "due",
			wantTrigger: "catch_up",
			wantEnqueue: true,
		},
		{
			name:        "miss outside grace",
			schedule:    dueSchedule{catchupPolicy: "catch_up", catchupGrace: 30 * time.Minute},
			now:         scheduledFor.Add(31 * time.Minute),
			wantState:   "missed",
			wantTrigger: "scheduled",
		},
		{
			name:        "skip overdue occurrence",
			schedule:    dueSchedule{catchupPolicy: "skip", catchupGrace: 24 * time.Hour},
			now:         scheduledFor.Add(time.Second),
			wantState:   "missed",
			wantTrigger: "scheduled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, trigger, enqueue, skipped := classifyOccurrence(test.schedule, scheduledFor, test.now)
			if state != test.wantState || trigger != test.wantTrigger ||
				enqueue != test.wantEnqueue || skipped != test.wantSkipped {
				t.Fatalf(
					"classifyOccurrence() = (%q, %q, %t, %t)",
					state,
					trigger,
					enqueue,
					skipped,
				)
			}
		})
	}
}
