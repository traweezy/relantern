package pgstore

import (
	"errors"
	"testing"

	"github.com/traweezy/relantern/internal/digest"
)

func TestRankIncompleteResearchBackfillsAfterCompletedTopStory(t *testing.T) {
	t.Parallel()
	candidates := []digest.DigestCandidate{
		{Type: "story", ID: "security", Category: "security", Score: 1},
		{Type: "story", ID: "release-1", Category: "release", Score: 0.95},
		{Type: "story", ID: "release-2", Category: "release", Score: 0.94},
		{Type: "story", ID: "release-3", Category: "release", Score: 0.93},
		{Type: "story", ID: "release-4", Category: "release", Score: 0.92},
		{Type: "story", ID: "release-5", Category: "release", Score: 0.91},
	}
	checks := make(map[string]int)
	selected, err := rankIncompleteResearch(candidates, 5, 0.5, func(clusterID string) (bool, error) {
		checks[clusterID]++
		return clusterID == "security", nil
	})
	if err != nil || len(selected) != 5 {
		t.Fatalf("rank incomplete = %+v, %v", selected, err)
	}
	for _, candidate := range selected {
		if candidate.ID == "security" {
			t.Fatal("completed story occupied an admission slot")
		}
		if checks[candidate.ID] != 1 {
			t.Fatalf("candidate %q checked %d times", candidate.ID, checks[candidate.ID])
		}
	}
	if checks["security"] != 1 {
		t.Fatalf("completed story checked %d times", checks["security"])
	}
}

func TestRankIncompleteResearchPropagatesCurrentBriefFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("database unavailable")
	_, err := rankIncompleteResearch([]digest.DigestCandidate{{
		Type: "story", ID: "release", Category: "release", Score: 0.9,
	}}, 5, 0.5, func(string) (bool, error) { return false, want })
	if !errors.Is(err, want) {
		t.Fatalf("rank incomplete error = %v, want %v", err, want)
	}
}
