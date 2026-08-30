package digest

import (
	"testing"
	"time"
)

func TestScoreStoryKeepsSecurityAbovePopularityFreeGeneralSignals(t *testing.T) {
	start := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	security, category, _ := ScoreStory(StorySignals{
		Signal: "security", SourceTier: "T0", Confidence: "high",
		ObservedAt: end, WindowStart: start, WindowEnd: end,
	})
	general, _, _ := ScoreStory(StorySignals{
		Signal: "general", SourceTier: "T0", Confidence: "high",
		ObservedAt: end, WindowStart: start, WindowEnd: end,
	})
	if category != "security" || security <= general || security != 1 {
		t.Fatalf("security score = %.4f category %q, general = %.4f", security, category, general)
	}
}

func TestSelectAppliesAllocationWithoutDuplicatesAndFillsUnusedCapacity(t *testing.T) {
	candidates := []DigestCandidate{
		{Type: "story", ID: "security-1", Category: "security", Score: 1},
		{Type: "story", ID: "security-2", Category: "security", Score: 0.99},
		{Type: "story", ID: "security-3", Category: "security", Score: 0.98},
		{Type: "story", ID: "release-1", Category: "release", Score: 0.90},
		{Type: "story", ID: "general-1", Category: "general", Score: 0.80},
		{Type: "radar", ID: "radar-1", Category: "radar", Score: 0.70},
		{Type: "story", ID: "below", Category: "general", Score: 0.49},
	}
	selected := Select(candidates, 5, 0.5)
	if len(selected) != 5 {
		t.Fatalf("selected %d candidates, want 5: %+v", len(selected), selected)
	}
	seen := make(map[string]struct{}, len(selected))
	for _, candidate := range selected {
		key := candidate.Type + ":" + candidate.ID
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate candidate %q", key)
		}
		seen[key] = struct{}{}
		if candidate.ID == "below" {
			t.Fatal("candidate below minimum score was selected")
		}
	}
}

func TestRadarRejectIsNeverDigestEligible(t *testing.T) {
	score, _ := ScoreRadar(RadarSignals{CurrentState: "reject"})
	if score != 0 || len(Select([]DigestCandidate{{Type: "radar", ID: "reject", Category: "radar", Score: score}}, 10, 0.5)) != 0 {
		t.Fatalf("reject score = %.4f", score)
	}
}
