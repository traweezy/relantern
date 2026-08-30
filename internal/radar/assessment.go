package radar

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

var compatibleLicenses = []string{
	"0BSD", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "MIT", "MPL-2.0",
}

func Assess(evidence CandidateEvidence) Assessment {
	reasons := make([]string, 0, 8)
	hardFailure := false
	hold := false
	licenseCompatible := slices.Contains(compatibleLicenses, evidence.License)
	if !licenseCompatible {
		hardFailure = true
		reasons = append(reasons, "License compatibility is not established for the approved policy.")
	}
	if evidence.CriticalAdvisoryCount > 0 {
		hardFailure = true
		reasons = append(reasons, "Unresolved critical security advisories block progression.")
	}
	if unstableRelease(evidence.StableRelease) {
		hold = true
		reasons = append(reasons, "The observed release is prerelease or does not establish stability.")
	}
	if evidence.ContributorCount < 2 {
		hold = true
		reasons = append(reasons, "The maintainer bus factor is below the review threshold.")
	}
	if evidence.ReleaseCadenceDays == nil || evidence.IssueResponseDays == nil || evidence.SecurityResponseDays == nil {
		hold = true
		reasons = append(reasons, "Maintenance or response-time evidence is incomplete.")
	}
	if len(evidence.RuntimeCompatibility) == 0 || len(evidence.ProjectTypes) == 0 ||
		len(evidence.CompatibilityRequirements) == 0 || len(evidence.ExitConditions) == 0 {
		hold = true
		reasons = append(reasons, "Compatibility, applicability, or exit evidence is incomplete.")
	}
	if len(evidence.Links) < 3 {
		hold = true
		reasons = append(reasons, "Fewer than three attributable evidence links were supplied.")
	}
	if evidence.ScorecardScore == nil || *evidence.ScorecardScore < 5 {
		hold = true
		reasons = append(reasons, "OpenSSF Scorecard evidence is missing or below the review threshold.")
	}
	if !evidence.SignedReleases || !evidence.ProvenanceVerified {
		hold = true
		reasons = append(reasons, "Signed release and provenance evidence is incomplete.")
	}

	state := StateAssess
	switch {
	case hardFailure:
		state = StateReject
	case hold:
		state = StateHold
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "The candidate has sufficient evidence for owner assessment.")
	}
	popularity := popularityMagnitude(evidence.PopularitySignals)
	return Assessment{
		SuggestedState: state,
		Confidence:     assessmentConfidence(evidence),
		Misleading:     popularity >= 100_000 && state != StateAssess,
		Dimensions:     comparisonDimensions(evidence, licenseCompatible),
		Reasons:        reasons,
	}
}

func unstableRelease(version string) bool {
	normalized := strings.ToLower(strings.TrimSpace(version))
	return normalized == "" || strings.Contains(normalized, "alpha") ||
		strings.Contains(normalized, "beta") || strings.Contains(normalized, "-rc") ||
		strings.Contains(normalized, "snapshot")
}

func popularityMagnitude(signals map[string]any) float64 {
	maximum := 0.0
	for _, value := range signals {
		if number, ok := value.(float64); ok && number > maximum {
			maximum = number
		}
	}
	return maximum
}

func assessmentConfidence(evidence CandidateEvidence) float64 {
	checks := []bool{
		evidence.ScorecardScore != nil,
		evidence.ReleaseCadenceDays != nil,
		evidence.IssueResponseDays != nil,
		evidence.SecurityResponseDays != nil,
		len(evidence.RuntimeCompatibility) > 0,
		len(evidence.Links) >= 3,
		evidence.SignedReleases,
		evidence.ProvenanceVerified,
	}
	complete := 0
	for _, value := range checks {
		if value {
			complete++
		}
	}
	return math.Round((0.5+float64(complete)/16)*10_000) / 10_000
}

func comparisonDimensions(evidence CandidateEvidence, licenseCompatible bool) []ComparisonDimension {
	links := evidence.Links
	return []ComparisonDimension{
		dimension("Capability", fmt.Sprintf("Supports %s with types=%t", strings.Join(evidence.RuntimeCompatibility, ", "), evidence.TypesSupported), evidence.IncumbentPackage, evidence.TypesSupported && len(evidence.RuntimeCompatibility) > 0, links),
		dimension("Stability", "Stable release "+evidence.StableRelease, "Current approved dependency", !unstableRelease(evidence.StableRelease), links),
		dimension("Maintenance", fmt.Sprintf("%d contributors", evidence.ContributorCount), "Current maintainer baseline", evidence.ContributorCount >= 2 && evidence.ReleaseCadenceDays != nil, links),
		dimension("Security", fmt.Sprintf("%d advisories; %d critical", evidence.SecurityAdvisoryCount, evidence.CriticalAdvisoryCount), "Current security baseline", licenseCompatible && evidence.CriticalAdvisoryCount == 0, links),
		dimension("Performance", bundleDescription(evidence.BundleSizeBytes), "Measure against the incumbent", evidence.BundleSizeBytes != nil, links),
		dimension("Migration", strings.Join(evidence.CompatibilityRequirements, "; "), "Existing integration", len(evidence.CompatibilityRequirements) > 0, links),
		dimension("Reversibility", strings.Join(evidence.ExitConditions, "; "), "Retain incumbent rollback path", len(evidence.ExitConditions) > 0, links),
	}
}

func dimension(name string, candidate string, current string, positive bool, links []EvidenceLink) ComparisonDimension {
	verdict := "needs-evidence"
	if positive {
		verdict = "supported"
	}
	return ComparisonDimension{Name: name, Candidate: candidate, Current: current, Verdict: verdict, Evidence: links}
}

func bundleDescription(size *int64) string {
	if size == nil {
		return "No credible bundle or binary-size evidence"
	}
	return fmt.Sprintf("Measured artifact size: %d bytes", *size)
}
