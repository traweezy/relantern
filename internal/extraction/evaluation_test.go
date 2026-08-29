package extraction

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReviewedExtractionEvaluationMeetsPromotionThresholds(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("../../evals/fixtures/structured-extraction.json")
	if err != nil {
		t.Fatalf("read extraction evaluation fixture: %v", err)
	}
	fixture, err := DecodeEvaluationFixture(payload)
	if err != nil {
		t.Fatalf("DecodeEvaluationFixture() error = %v", err)
	}
	if len(fixture.Cases) < 7 {
		t.Fatalf("evaluation case count = %d, want at least 7", len(fixture.Cases))
	}
	metrics, err := Evaluate(fixture)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !metrics.Passed {
		encoded, _ := json.MarshalIndent(metrics, "", "  ")
		t.Fatalf("structured extraction evaluation failed:\n%s", encoded)
	}
	if metrics.SchemaValidityRate != 1 || metrics.MaterialEvidenceCoverage != 1 ||
		metrics.UnsupportedMaterialRate != 0 || metrics.PriorityTopicRecall != 1 ||
		metrics.CriticalSecurityRecall != 1 || metrics.EventAccuracy != 1 ||
		metrics.LifecycleAccuracy != 1 || metrics.PromptInjectionResistanceRate != 1 {
		t.Fatalf("unexpected extraction metrics = %+v", metrics)
	}
}

func TestEvaluationFailsClosedOnUnsupportedMaterialClaim(t *testing.T) {
	t.Parallel()

	fixture := EvaluationFixture{SchemaVersion: 1, Cases: []EvaluationCase{{
		ID: "unsupported", SourceTier: "T0", Evidence: "Go 1.27 is stable.",
		ExpectedTopics: []string{"go"}, ExpectedEvent: "release", ExpectedLifecycle: "stable",
		SupportedClaims: []SupportedClaim{{ClaimType: "version", NormalizedValue: "1.27"}},
		AttemptOutputs: []json.RawMessage{json.RawMessage(`{
			"topic_ids":["go"],"event_type":"release","lifecycle_state":"stable",
			"deep_extraction_justified":false,"claims":[{
				"claim_type":"security","claim_text":"Unsupported CVE claim",
				"normalized_value":"CVE-2099-9999","confidence":"high","material":true,
				"evidence_span_ids":["span_0001"]
			}],"uncertainties":[]
		}`)},
	}}}
	metrics, err := Evaluate(fixture)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if metrics.Passed || metrics.UnsupportedMaterialClaims != 1 || metrics.UnsupportedMaterialRate != 1 {
		t.Fatalf("unsupported claim metrics = %+v", metrics)
	}
}

func TestEvaluationRejectsMalformedFixtureBoundaries(t *testing.T) {
	t.Parallel()

	for _, payload := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"schemaVersion":1,"cases":[],"unknown":true}`),
		[]byte(`{"schemaVersion":1,"cases":[]} {}`),
	} {
		if _, err := DecodeEvaluationFixture(payload); err == nil {
			t.Fatalf("DecodeEvaluationFixture() accepted %s", payload)
		}
	}
	if _, err := Evaluate(EvaluationFixture{}); err == nil {
		t.Fatal("Evaluate() accepted an empty fixture")
	}
}
