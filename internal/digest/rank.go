package digest

import (
	"cmp"
	"math"
	"slices"
	"time"
)

func ScoreStory(signals StorySignals) (float64, string, string) {
	base, category, reason := storySignalScore(signals.Signal, signals.LifecycleState)
	sourceAdjustment := map[string]float64{"T0": 0.10, "T1": 0.05, "T2": 0, "T3": -0.10}[signals.SourceTier]
	confidenceAdjustment := map[string]float64{
		"high": 0.05, "medium": 0, "low": -0.10, "unknown": -0.15,
	}[signals.Confidence]
	statusAdjustment := 0.0
	if signals.Status == "updated" {
		statusAdjustment = 0.03
	}
	recencyAdjustment := recencyScore(signals.ObservedAt, signals.WindowStart, signals.WindowEnd)
	return roundedScore(base + sourceAdjustment + confidenceAdjustment + statusAdjustment + recencyAdjustment), category, reason
}

func storySignalScore(signal string, lifecycle string) (float64, string, string) {
	switch {
	case signal == "security":
		return 0.90, "security", "Confirmed security evidence receives the highest deterministic priority."
	case signal == "breaking-change":
		return 0.82, "release", "A breaking change can require owner action in the current stack."
	case lifecycle == "preview" || lifecycle == "release_candidate" || signal == "proposal":
		return 0.68, "coming_soon", "A source-declared preview or proposal can affect near-term planning."
	case signal == "release":
		return 0.74, "release", "A stable release is eligible for the bounded release allocation."
	case signal == "deprecation":
		return 0.78, "release", "A documented deprecation can require migration planning."
	default:
		return 0.56, "general", "The evidence-backed story is ranked within the current digest window."
	}
}

func ScoreRadar(signals RadarSignals) (float64, string) {
	base := map[string]float64{
		"adopt":  0.72,
		"trial":  0.70,
		"assess": 0.66,
		"hold":   0.54,
		"reject": 0.0,
	}[signals.CurrentState]
	if signals.Misleading {
		base -= 0.08
	}
	return roundedScore(base), "A package assessment is ready for explicit owner review."
}

func Select(candidates []DigestCandidate, maximum int, minimumScore float64) []DigestCandidate {
	if maximum <= 0 {
		return []DigestCandidate{}
	}
	eligible := make([]DigestCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Score >= minimumScore {
			eligible = append(eligible, candidate)
		}
	}
	slices.SortFunc(eligible, func(left DigestCandidate, right DigestCandidate) int {
		if scoreOrder := cmp.Compare(right.Score, left.Score); scoreOrder != 0 {
			return scoreOrder
		}
		if categoryOrder := cmp.Compare(categoryPriority(left.Category), categoryPriority(right.Category)); categoryOrder != 0 {
			return categoryOrder
		}
		if timeOrder := right.Observed.Compare(left.Observed); timeOrder != 0 {
			return timeOrder
		}
		if typeOrder := cmp.Compare(left.Type, right.Type); typeOrder != 0 {
			return typeOrder
		}
		return cmp.Compare(left.ID, right.ID)
	})

	selected := make([]DigestCandidate, 0, min(maximum, len(eligible)))
	seen := make(map[string]struct{}, len(eligible))
	appendCategory := func(category string, limit int) {
		for _, candidate := range eligible {
			key := candidate.Type + ":" + candidate.ID
			if candidate.Category != category || len(selected) >= maximum || limit <= 0 {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			selected = append(selected, candidate)
			seen[key] = struct{}{}
			limit--
		}
	}
	appendCategory("security", 2)
	appendCategory("release", 4)
	appendCategory("coming_soon", 2)
	appendCategory("radar", 2)
	for _, candidate := range eligible {
		if len(selected) >= maximum {
			break
		}
		key := candidate.Type + ":" + candidate.ID
		if _, exists := seen[key]; exists {
			continue
		}
		selected = append(selected, candidate)
		seen[key] = struct{}{}
	}
	return selected
}

func recencyScore(observedAt time.Time, windowStart time.Time, windowEnd time.Time) float64 {
	window := windowEnd.Sub(windowStart)
	if window <= 0 || observedAt.Before(windowStart) {
		return 0
	}
	position := observedAt.Sub(windowStart).Seconds() / window.Seconds()
	return math.Max(0, math.Min(0.05, position*0.05))
}

func roundedScore(value float64) float64 {
	return math.Round(math.Max(0, math.Min(1, value))*10_000) / 10_000
}

func categoryPriority(category string) int {
	return map[string]int{
		"security": 0, "release": 1, "coming_soon": 2,
		"radar": 3, "later": 4, "general": 5,
	}[category]
}
