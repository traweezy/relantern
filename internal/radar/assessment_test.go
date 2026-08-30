package radar

import (
	"fmt"
	"testing"
	"time"
)

func TestAssessmentFixturesSeparateEvidenceFromPopularity(t *testing.T) {
	t.Parallel()
	for index := range 10 {
		evidence := knownGoodEvidence(fmt.Sprintf("good-%d", index))
		assessment := Assess(evidence)
		if assessment.SuggestedState != StateAssess || assessment.Misleading {
			t.Fatalf("known-good fixture %d = (%s, misleading=%t)", index, assessment.SuggestedState, assessment.Misleading)
		}
		if len(assessment.Dimensions) != 7 {
			t.Fatalf("known-good fixture %d has %d dimensions", index, len(assessment.Dimensions))
		}
	}
	for index := range 10 {
		evidence := knownGoodEvidence(fmt.Sprintf("misleading-%d", index))
		evidence.License = "UNKNOWN"
		evidence.CriticalAdvisoryCount = 1
		evidence.SecurityAdvisoryCount = 1
		evidence.PopularitySignals = map[string]any{"weeklyDownloads": float64(10_000_000 + index)}
		assessment := Assess(evidence)
		if assessment.SuggestedState != StateReject || !assessment.Misleading {
			t.Fatalf("misleading fixture %d = (%s, misleading=%t)", index, assessment.SuggestedState, assessment.Misleading)
		}
	}
}

func TestAssessmentNeverSuggestsAdoptOrTrial(t *testing.T) {
	t.Parallel()
	assessment := Assess(knownGoodEvidence("safe"))
	if assessment.SuggestedState == StateAdopt || assessment.SuggestedState == StateTrial {
		t.Fatalf("Assess() suggested owner-only state %s", assessment.SuggestedState)
	}
}

func knownGoodEvidence(name string) CandidateEvidence {
	cadence := 30
	response := 4
	securityResponse := 2
	scorecard := 8.4
	bundle := int64(18_000)
	return CandidateEvidence{
		Ecosystem: "npm", PackageName: name, RepositoryURL: "https://example.com/" + name,
		DiscoverySource: "fixture", IncumbentPackage: "incumbent", StableRelease: "1.2.3", License: "MIT",
		ContributorCount: 8, ReleaseCadenceDays: &cadence, IssueResponseDays: &response,
		SecurityResponseDays: &securityResponse, ScorecardScore: &scorecard,
		SignedReleases: true, ProvenanceVerified: true, TypesSupported: true, BundleSizeBytes: &bundle,
		RuntimeCompatibility: []string{"Node 26"}, ProjectTypes: []string{"web"},
		CompatibilityRequirements: []string{"ESM"}, ExitConditions: []string{"Keep the incumbent adapter"},
		MaintenanceSignals: map[string]any{}, SecuritySignals: map[string]any{},
		PopularitySignals: map[string]any{"weeklyDownloads": float64(50_000)},
		Links: []EvidenceLink{
			{Label: "release", URL: "https://example.com/release", SourceTier: "T0"},
			{Label: "security", URL: "https://example.com/security", SourceTier: "T0"},
			{Label: "docs", URL: "https://example.com/docs", SourceTier: "T1"},
		},
		ObservedAt: time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC),
	}
}
