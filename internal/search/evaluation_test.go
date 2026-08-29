package search_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/traweezy/relantern/internal/search"
)

type searchEvaluation struct {
	SchemaVersion              int                    `json:"schemaVersion"`
	RequiredMeanReciprocalRank float64                `json:"requiredMeanReciprocalRank"`
	RequiredRecallAt5          float64                `json:"requiredRecallAt5"`
	Cases                      []searchEvaluationCase `json:"cases"`
}

type searchEvaluationCase struct {
	Name        string            `json:"name"`
	RelevantIDs []string          `json:"relevantIds"`
	Keyword     []search.RankedID `json:"keyword"`
	Semantic    []search.RankedID `json:"semantic"`
}

func TestReviewedSearchEvaluationMeetsThresholds(t *testing.T) {
	contents, err := os.ReadFile("../../evals/fixtures/search.json")
	if err != nil {
		t.Fatalf("read search evaluation: %v", err)
	}
	var fixture searchEvaluation
	decoderError := json.Unmarshal(contents, &fixture)
	if decoderError != nil {
		t.Fatalf("decode search evaluation: %v", decoderError)
	}
	if fixture.SchemaVersion != 1 || len(fixture.Cases) < 5 {
		t.Fatalf("search evaluation shape = version %d, cases %d", fixture.SchemaVersion, len(fixture.Cases))
	}

	var reciprocalRankTotal float64
	var recalled int
	for _, evaluationCase := range fixture.Cases {
		results, err := search.Fuse(evaluationCase.Keyword, evaluationCase.Semantic, search.DefaultRRFK)
		if err != nil {
			t.Fatalf("%s: Fuse() error = %v", evaluationCase.Name, err)
		}
		relevant := make(map[string]struct{}, len(evaluationCase.RelevantIDs))
		for _, id := range evaluationCase.RelevantIDs {
			relevant[id] = struct{}{}
		}
		firstRelevantRank := 0
		caseRecalled := false
		for index, result := range results {
			if _, ok := relevant[result.ID]; !ok {
				continue
			}
			if firstRelevantRank == 0 {
				firstRelevantRank = index + 1
			}
			if index < 5 {
				caseRecalled = true
			}
		}
		if firstRelevantRank > 0 {
			reciprocalRankTotal += 1 / float64(firstRelevantRank)
		}
		if caseRecalled {
			recalled++
		}
	}
	meanReciprocalRank := reciprocalRankTotal / float64(len(fixture.Cases))
	recallAt5 := float64(recalled) / float64(len(fixture.Cases))
	if meanReciprocalRank < fixture.RequiredMeanReciprocalRank || recallAt5 < fixture.RequiredRecallAt5 {
		t.Fatalf(
			"search evaluation MRR %.3f and recall@5 %.3f; require %.3f and %.3f",
			meanReciprocalRank,
			recallAt5,
			fixture.RequiredMeanReciprocalRank,
			fixture.RequiredRecallAt5,
		)
	}
}
