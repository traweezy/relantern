package operability_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/operability"
)

func TestEvaluateUsesDocumentedBoundedThresholds(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	alerts := operability.Evaluate(operability.Snapshot{
		PriorityFreshness:     901,
		OldestJobAge:          901,
		DelayedDigests:        1,
		UndeliveredCritical:   1,
		LastRetentionState:    "failed",
		LastRestoreState:      "passed",
		LastRestoreCompleted:  now.Add(-36 * 24 * time.Hour),
		LastRestoreRPOSeconds: 90000,
		LastRestoreRTOSeconds: 100,
		LastRestoreRPOTarget:  86400,
		LastRestoreRTOTarget:  14400,
	}, now)
	if len(alerts) != 7 {
		t.Fatalf("Evaluate() returned %d alerts: %+v", len(alerts), alerts)
	}
}

func TestWritePrometheusHasStableLowCardinalityNames(t *testing.T) {
	var output bytes.Buffer
	if err := operability.WritePrometheus(&output, operability.Snapshot{QueueDepth: 3}); err != nil {
		t.Fatal(err)
	}
	encoded := output.String()
	for _, name := range []string{"source_poll_due_total", "river_queue_depth", "restore_rto_seconds", "critical_alert_undelivered_count", "critical_alert_corrected_count", "critical_alert_delivery_suppressed_count", "reviewed_advisory_scan_pending", "reviewed_advisory_scan_age_seconds", "reviewed_advisory_scan_issues", "advisory_observation_pending", "advisory_observation_oldest_age_seconds"} {
		if !strings.Contains(encoded, name) {
			t.Fatalf("metrics output lacks %s", name)
		}
	}
	if strings.Contains(encoded, "url=") || strings.Contains(encoded, "provider_id") {
		t.Fatalf("metrics output contains a high-cardinality label: %s", encoded)
	}
}

func TestEvaluateAlertsWhenAdvisoryObservationExceedsTenMinutes(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		pending int64
		age     float64
		want    bool
	}{
		{name: "below threshold", pending: 1, age: 600},
		{name: "stale pending observation", pending: 1, age: 601, want: true},
		{name: "no pending observation", age: 601},
	} {
		t.Run(test.name, func(t *testing.T) {
			alerts := operability.Evaluate(operability.Snapshot{
				AdvisoryObservationsPending: test.pending,
				AdvisoryObservationAge:      test.age,
			}, now)
			if (len(alerts) == 1) != test.want {
				t.Fatalf("stalled advisory observation signal = %+v, want %t", alerts, test.want)
			}
			if test.want && alerts[0].Name != "advisory_observation_stalled" {
				t.Fatalf("stalled advisory observation signal = %+v", alerts)
			}
		})
	}
}

func TestEvaluateAlertsWhenReviewedAdvisoryScanExceedsDay(t *testing.T) {
	alerts := operability.Evaluate(operability.Snapshot{
		AdvisoryScanPending: 1,
		AdvisoryScanAge:     24*3600 + 1,
	}, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	if len(alerts) != 1 || alerts[0].Name != "reviewed_advisory_scan_stalled" {
		t.Fatalf("expected advisory coverage warning, got %+v", alerts)
	}
}

func TestEvaluateAlertsWhenReviewedAdvisoryScanHasInvalidCursor(t *testing.T) {
	alerts := operability.Evaluate(operability.Snapshot{
		AdvisoryScanPending: 1,
		AdvisoryScanIssues:  1,
	}, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	if len(alerts) != 1 || alerts[0].Name != "reviewed_advisory_scan_invalid" {
		t.Fatalf("expected invalid advisory cursor warning, got %+v", alerts)
	}
}
