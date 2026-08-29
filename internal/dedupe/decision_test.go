package dedupe

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestDecideUsesDeterministicPrecedence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	document := decisionDocument(now)
	candidates := []Candidate{
		decisionCandidate("simhash", "cluster-simhash", "T0", now, document),
		decisionCandidate("canonical", "cluster-canonical", "T3", now, document),
	}
	candidates[0].CanonicalURL = "https://other.example.test/story"
	candidates[0].NormalizedSHA256 = sha256.Sum256([]byte("other normalized body"))
	candidates[0].RawSHA256 = sha256.Sum256([]byte("other raw body"))
	candidates[0].NormalizedTitle = "different title"
	candidates[0].NormalizedAuthor = "different author"
	candidates[1].SourceID = "other-source"
	candidates[1].SimHash = SimHash("completely unrelated body")

	decision := Decide(document, candidates, DefaultConfig())
	if decision.Method != MethodCanonicalURL || decision.CandidateItemID != "canonical" {
		t.Fatalf("Decide() = %+v", decision)
	}
}

func TestDecideClassifiesRevisionDuplicateClusterAndNew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	document := decisionDocument(now)
	tests := []struct {
		name      string
		candidate Candidate
		want      Outcome
		method    Method
	}{
		{
			name:      "revision",
			candidate: decisionCandidate("item", "cluster", "T1", now, document),
			want:      OutcomeRevision,
			method:    MethodRevision,
		},
		{
			name: "normalized hash",
			candidate: func() Candidate {
				candidate := decisionCandidate("item", "cluster", "T1", now, document)
				candidate.SourceID = "other-source"
				candidate.CanonicalURL = "https://other.example.test/story"
				candidate.RawSHA256 = sha256.Sum256([]byte("different raw"))
				return candidate
			}(),
			want:   OutcomeDuplicate,
			method: MethodNormalizedSHA256,
		},
		{
			name: "metadata",
			candidate: func() Candidate {
				candidate := unrelatedCandidate(now)
				candidate.NormalizedTitle = NormalizeMetadata(document.Title)
				candidate.NormalizedAuthor = NormalizeMetadata(document.Author)
				return candidate
			}(),
			want:   OutcomeDuplicate,
			method: MethodMetadata,
		},
		{
			name: "embedding cluster",
			candidate: func() Candidate {
				candidate := unrelatedCandidate(now)
				candidate.PackageName = ""
				candidate.Version = ""
				candidate.EmbeddingSimilarity = 0.91
				return candidate
			}(),
			want:   OutcomeCluster,
			method: MethodEmbedding,
		},
		{
			name: "package version cluster",
			candidate: func() Candidate {
				candidate := unrelatedCandidate(now)
				candidate.PackageName = "go"
				candidate.Version = "1 27"
				return candidate
			}(),
			want:   OutcomeCluster,
			method: MethodPackageVersion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := Decide(document, []Candidate{test.candidate}, DefaultConfig())
			if decision.Outcome != test.want || decision.Method != test.method {
				t.Fatalf("Decide() = %+v", decision)
			}
		})
	}

	if decision := Decide(document, []Candidate{unrelatedCandidate(now.Add(-31 * 24 * time.Hour))}, DefaultConfig()); decision.Outcome != OutcomeCreate || decision.Method != MethodNew {
		t.Fatalf("Decide() unrelated = %+v", decision)
	}
}

func TestDecideBreaksCandidateTiesByTrustThenAgeThenID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	document := decisionDocument(now)
	lowTrust := decisionCandidate("a-low-trust", "cluster-low", "T3", now.Add(-time.Hour), document)
	highTrust := decisionCandidate("z-high-trust", "cluster-high", "T0", now, document)
	decision := Decide(document, []Candidate{lowTrust, highTrust}, DefaultConfig())
	if decision.CandidateItemID != highTrust.ItemID {
		t.Fatalf("Decide() chose %q", decision.CandidateItemID)
	}
}

func decisionDocument(now time.Time) Document {
	return Document{
		RevisionID:       "revision-current",
		SourceID:         "go-blog",
		SourceTier:       "T0",
		CanonicalURL:     "https://go.dev/blog/go1.27",
		Title:            "Go 1.27 is released",
		Author:           "Go team",
		PackageName:      "go",
		Version:          "1.27",
		RawSHA256:        sha256.Sum256([]byte("raw body")),
		NormalizedSHA256: sha256.Sum256([]byte("normalized body")),
		SimHash:          SimHash("Go 1.27 release details for production users"),
		PublishedAt:      now,
		FirstSeenAt:      now,
	}
}

func decisionCandidate(itemID string, clusterID string, tier string, now time.Time, document Document) Candidate {
	return Candidate{
		ItemID:           itemID,
		ClusterID:        clusterID,
		RevisionID:       "candidate-revision",
		SourceID:         document.SourceID,
		SourceTier:       tier,
		CanonicalURL:     document.CanonicalURL,
		NormalizedTitle:  NormalizeMetadata(document.Title),
		NormalizedAuthor: NormalizeMetadata(document.Author),
		PackageName:      NormalizeMetadata(document.PackageName),
		Version:          NormalizeMetadata(document.Version),
		RawSHA256:        document.RawSHA256,
		NormalizedSHA256: document.NormalizedSHA256,
		SimHash:          document.SimHash,
		PublishedAt:      now,
		FirstSeenAt:      now,
	}
}

func unrelatedCandidate(now time.Time) Candidate {
	return Candidate{
		ItemID:           "unrelated",
		ClusterID:        "unrelated-cluster",
		RevisionID:       "unrelated-revision",
		SourceID:         "react-blog",
		SourceTier:       "T1",
		CanonicalURL:     "https://react.dev/blog/compiler",
		NormalizedTitle:  "react compiler rollout",
		NormalizedAuthor: "react team",
		PackageName:      "react",
		Version:          "19 2",
		RawSHA256:        sha256.Sum256([]byte("react raw")),
		NormalizedSHA256: sha256.Sum256([]byte("react normalized")),
		SimHash:          SimHash("React compiler rollout and browser rendering details"),
		PublishedAt:      now,
		FirstSeenAt:      now,
	}
}
