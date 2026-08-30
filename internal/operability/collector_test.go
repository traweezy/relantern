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
		LastRetentionState:    "failed",
		LastRestoreState:      "passed",
		LastRestoreCompleted:  now.Add(-36 * 24 * time.Hour),
		LastRestoreRPOSeconds: 90000,
		LastRestoreRTOSeconds: 100,
		LastRestoreRPOTarget:  86400,
		LastRestoreRTOTarget:  14400,
	}, now)
	if len(alerts) != 6 {
		t.Fatalf("Evaluate() returned %d alerts: %+v", len(alerts), alerts)
	}
}

func TestWritePrometheusHasStableLowCardinalityNames(t *testing.T) {
	var output bytes.Buffer
	if err := operability.WritePrometheus(&output, operability.Snapshot{QueueDepth: 3}); err != nil {
		t.Fatal(err)
	}
	encoded := output.String()
	for _, name := range []string{"source_poll_due_total", "river_queue_depth", "restore_rto_seconds"} {
		if !strings.Contains(encoded, name) {
			t.Fatalf("metrics output lacks %s", name)
		}
	}
	if strings.Contains(encoded, "url=") || strings.Contains(encoded, "provider_id") {
		t.Fatalf("metrics output contains a high-cardinality label: %s", encoded)
	}
}
