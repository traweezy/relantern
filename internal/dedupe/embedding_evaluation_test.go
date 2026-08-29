package dedupe

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

type embeddingEvaluationFixture struct {
	SchemaVersion     int                       `json:"schemaVersion"`
	RequiredPrecision float64                   `json:"requiredPrecision"`
	RequiredRecall    float64                   `json:"requiredRecall"`
	Cases             []embeddingEvaluationCase `json:"cases"`
}

type embeddingEvaluationCase struct {
	ID         string  `json:"id"`
	Cluster    bool    `json:"cluster"`
	Similarity float64 `json:"similarity"`
}

func TestEvaluatedEmbeddingSimilarityMeetsLabeledPrecision(t *testing.T) {
	t.Parallel()

	fixture := loadEmbeddingEvaluationFixture(t)
	selectedThreshold, selectedMetrics := selectEmbeddingThreshold(fixture)
	if selectedThreshold != EvaluatedEmbeddingSimilarity {
		t.Fatalf(
			"evaluated embedding threshold = %g, update the reviewed constant from %g; metrics = %+v",
			selectedThreshold,
			EvaluatedEmbeddingSimilarity,
			selectedMetrics,
		)
	}
	if selectedMetrics.Precision < fixture.RequiredPrecision || selectedMetrics.Recall < fixture.RequiredRecall {
		t.Fatalf(
			"evaluated metrics = %+v, required precision %.2f and recall %.2f",
			selectedMetrics,
			fixture.RequiredPrecision,
			fixture.RequiredRecall,
		)
	}
}

func loadEmbeddingEvaluationFixture(t *testing.T) embeddingEvaluationFixture {
	t.Helper()
	payload, err := os.ReadFile("../../evals/fixtures/dedupe-embedding.json")
	if err != nil {
		t.Fatalf("read embedding dedupe evaluation fixture: %v", err)
	}
	var fixture embeddingEvaluationFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("decode embedding dedupe evaluation fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 || fixture.RequiredPrecision < 0.98 ||
		fixture.RequiredRecall < 0.9 || len(fixture.Cases) < 20 {
		t.Fatalf("invalid embedding dedupe evaluation fixture metadata: %+v", fixture)
	}
	for _, fixtureCase := range fixture.Cases {
		if fixtureCase.ID == "" || fixtureCase.Similarity < 0 || fixtureCase.Similarity > 1 {
			t.Fatalf("invalid embedding dedupe evaluation case: %+v", fixtureCase)
		}
	}
	return fixture
}

func selectEmbeddingThreshold(fixture embeddingEvaluationFixture) (float64, evaluationMetrics) {
	thresholds := make([]float64, 0, len(fixture.Cases))
	seen := make(map[float64]struct{}, len(fixture.Cases))
	for _, fixtureCase := range fixture.Cases {
		if _, found := seen[fixtureCase.Similarity]; found {
			continue
		}
		seen[fixtureCase.Similarity] = struct{}{}
		thresholds = append(thresholds, fixtureCase.Similarity)
	}
	sort.Float64s(thresholds)
	selectedThreshold := 1.0
	selectedMetrics := evaluateEmbeddingCases(fixture.Cases, selectedThreshold)
	for _, threshold := range thresholds {
		metrics := evaluateEmbeddingCases(fixture.Cases, threshold)
		if metrics.Precision < fixture.RequiredPrecision {
			continue
		}
		if metrics.Recall > selectedMetrics.Recall ||
			(metrics.Recall == selectedMetrics.Recall && threshold < selectedThreshold) {
			selectedThreshold = threshold
			selectedMetrics = metrics
		}
	}
	return selectedThreshold, selectedMetrics
}

func evaluateEmbeddingCases(cases []embeddingEvaluationCase, threshold float64) evaluationMetrics {
	metrics := evaluationMetrics{}
	for _, fixtureCase := range cases {
		predictedCluster := fixtureCase.Similarity >= threshold
		switch {
		case fixtureCase.Cluster && predictedCluster:
			metrics.TruePositive++
		case fixtureCase.Cluster:
			metrics.FalseNegative++
		case predictedCluster:
			metrics.FalsePositive++
		default:
			metrics.TrueNegative++
		}
	}
	if predictedPositive := metrics.TruePositive + metrics.FalsePositive; predictedPositive > 0 {
		metrics.Precision = float64(metrics.TruePositive) / float64(predictedPositive)
	} else {
		metrics.Precision = 1
	}
	if positives := metrics.TruePositive + metrics.FalseNegative; positives > 0 {
		metrics.Recall = float64(metrics.TruePositive) / float64(positives)
	}
	return metrics
}
