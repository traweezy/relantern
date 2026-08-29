package dedupe

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"
	"time"
)

type evaluationFixture struct {
	SchemaVersion     int              `json:"schemaVersion"`
	RequiredPrecision float64          `json:"requiredPrecision"`
	RequiredRecall    float64          `json:"requiredRecall"`
	Cases             []evaluationCase `json:"cases"`
}

type evaluationCase struct {
	ID               string `json:"id"`
	Duplicate        bool   `json:"duplicate"`
	Package          string `json:"package"`
	Version          string `json:"version"`
	CandidateVersion string `json:"candidateVersion"`
	Left             string `json:"left"`
	Right            string `json:"right"`
}

type evaluationMetrics struct {
	TruePositive  int
	FalsePositive int
	TrueNegative  int
	FalseNegative int
	Precision     float64
	Recall        float64
}

func TestEvaluatedSimHashDistanceMeetsLabeledPrecision(t *testing.T) {
	t.Parallel()

	fixture := loadEvaluationFixture(t)
	selectedDistance, selectedMetrics := selectSimHashDistance(fixture)
	if selectedDistance != EvaluatedSimHashDistance {
		t.Fatalf("evaluated SimHash distance = %d, update the reviewed constant from %d; metrics = %+v", selectedDistance, EvaluatedSimHashDistance, selectedMetrics)
	}
	if selectedMetrics.Precision < fixture.RequiredPrecision || selectedMetrics.Recall < fixture.RequiredRecall {
		t.Fatalf("evaluated metrics = %+v, required precision %.2f and recall %.2f", selectedMetrics, fixture.RequiredPrecision, fixture.RequiredRecall)
	}
}

func loadEvaluationFixture(t *testing.T) evaluationFixture {
	t.Helper()
	payload, err := os.ReadFile("../../evals/fixtures/dedupe.json")
	if err != nil {
		t.Fatalf("read dedupe evaluation fixture: %v", err)
	}
	var fixture evaluationFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("decode dedupe evaluation fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 || fixture.RequiredPrecision < 0.98 || fixture.RequiredRecall < 0.90 || len(fixture.Cases) < 20 {
		t.Fatalf("invalid dedupe evaluation fixture metadata: %+v", fixture)
	}
	return fixture
}

func selectSimHashDistance(fixture evaluationFixture) (int, evaluationMetrics) {
	selectedDistance := 0
	selectedMetrics := evaluateCases(fixture.Cases, 0)
	for distance := 1; distance <= 64; distance++ {
		metrics := evaluateCases(fixture.Cases, distance)
		if metrics.Precision < fixture.RequiredPrecision {
			continue
		}
		if metrics.Recall > selectedMetrics.Recall {
			selectedDistance = distance
			selectedMetrics = metrics
		}
	}
	return selectedDistance, selectedMetrics
}

func evaluateCases(cases []evaluationCase, distance int) evaluationMetrics {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	metrics := evaluationMetrics{}
	for _, fixtureCase := range cases {
		candidateVersion := fixtureCase.CandidateVersion
		if candidateVersion == "" {
			candidateVersion = fixtureCase.Version
		}
		document := Document{
			SourceID:         "fixture-left",
			SourceTier:       "T1",
			CanonicalURL:     "https://left.example.test/" + fixtureCase.ID,
			Title:            "Left report " + fixtureCase.ID,
			Author:           "Left publisher",
			PackageName:      fixtureCase.Package,
			Version:          fixtureCase.Version,
			RawSHA256:        sha256.Sum256([]byte("left raw " + fixtureCase.ID)),
			NormalizedSHA256: sha256.Sum256([]byte(fixtureCase.Left)),
			SimHash:          SimHash(fixtureCase.Left),
			PublishedAt:      now,
			FirstSeenAt:      now,
		}
		candidate := Candidate{
			ItemID:           fixtureCase.ID,
			ClusterID:        "cluster-" + fixtureCase.ID,
			SourceID:         "fixture-right",
			SourceTier:       "T1",
			CanonicalURL:     "https://right.example.test/" + fixtureCase.ID,
			NormalizedTitle:  "right report " + fixtureCase.ID,
			NormalizedAuthor: "right publisher",
			PackageName:      NormalizeMetadata(fixtureCase.Package),
			Version:          NormalizeMetadata(candidateVersion),
			RawSHA256:        sha256.Sum256([]byte("right raw " + fixtureCase.ID)),
			NormalizedSHA256: sha256.Sum256([]byte(fixtureCase.Right)),
			SimHash:          SimHash(fixtureCase.Right),
			PublishedAt:      now,
			FirstSeenAt:      now,
		}
		decision := Decide(document, []Candidate{candidate}, Config{
			SimHashDistance:     distance,
			EmbeddingSimilarity: EvaluatedEmbeddingSimilarity,
			ClusterMaxAge:       DefaultClusterMaxAge,
		})
		predictedDuplicate := decision.Outcome == OutcomeDuplicate
		switch {
		case fixtureCase.Duplicate && predictedDuplicate:
			metrics.TruePositive++
		case fixtureCase.Duplicate:
			metrics.FalseNegative++
		case predictedDuplicate:
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
	if positive := metrics.TruePositive + metrics.FalseNegative; positive > 0 {
		metrics.Recall = float64(metrics.TruePositive) / float64(positive)
	}
	return metrics
}
