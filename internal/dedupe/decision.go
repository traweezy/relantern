package dedupe

import (
	"bytes"
	"math"
	"sort"
	"strings"
	"time"
)

type rankedDecision struct {
	Decision
	Candidate Candidate
	rank      int
}

func Decide(document Document, candidates []Candidate, config Config) Decision {
	if err := config.Validate(); err != nil {
		return Decision{Outcome: OutcomeCreate, Method: MethodNew, Similarity: 1}
	}
	normalizedTitle := NormalizeMetadata(document.Title)
	normalizedAuthor := NormalizeMetadata(document.Author)
	packageName := NormalizeMetadata(document.PackageName)
	version := NormalizeMetadata(document.Version)
	decisions := make([]rankedDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decision, rank, matched := compareCandidate(
			document,
			normalizedTitle,
			normalizedAuthor,
			packageName,
			version,
			candidate,
			config,
		)
		if matched {
			decisions = append(decisions, rankedDecision{Decision: decision, Candidate: candidate, rank: rank})
		}
	}
	if len(decisions) == 0 {
		return Decision{Outcome: OutcomeCreate, Method: MethodNew, Similarity: 1}
	}
	sort.SliceStable(decisions, func(first int, second int) bool {
		if decisions[first].rank != decisions[second].rank {
			return decisions[first].rank < decisions[second].rank
		}
		if decisions[first].rank == 0 &&
			decisions[first].Candidate.ItemID == decisions[second].Candidate.ItemID &&
			!decisions[first].Candidate.ObservedAt.Equal(decisions[second].Candidate.ObservedAt) {
			return decisions[first].Candidate.ObservedAt.After(decisions[second].Candidate.ObservedAt)
		}
		firstTier := trustTierRank(decisions[first].Candidate.SourceTier)
		secondTier := trustTierRank(decisions[second].Candidate.SourceTier)
		if firstTier != secondTier {
			return firstTier < secondTier
		}
		if !decisions[first].Candidate.FirstSeenAt.Equal(decisions[second].Candidate.FirstSeenAt) {
			return decisions[first].Candidate.FirstSeenAt.Before(decisions[second].Candidate.FirstSeenAt)
		}
		return decisions[first].Candidate.ItemID < decisions[second].Candidate.ItemID
	})
	return decisions[0].Decision
}

func compareCandidate(
	document Document,
	normalizedTitle string,
	normalizedAuthor string,
	packageName string,
	version string,
	candidate Candidate,
	config Config,
) (Decision, int, bool) {
	base := Decision{
		CandidateItemID:     candidate.ItemID,
		CandidateRevisionID: candidate.RevisionID,
		ClusterID:           candidate.ClusterID,
		Similarity:          1,
	}
	if document.SourceID == candidate.SourceID && document.CanonicalURL == candidate.CanonicalURL {
		base.Outcome, base.Method = OutcomeRevision, MethodRevision
		return base, 0, true
	}
	if document.CanonicalURL == candidate.CanonicalURL {
		base.Outcome, base.Method = OutcomeDuplicate, MethodCanonicalURL
		return base, 1, true
	}
	if bytes.Equal(document.RawSHA256[:], candidate.RawSHA256[:]) {
		base.Outcome, base.Method = OutcomeDuplicate, MethodRawSHA256
		return base, 2, true
	}
	if bytes.Equal(document.NormalizedSHA256[:], candidate.NormalizedSHA256[:]) {
		base.Outcome, base.Method = OutcomeDuplicate, MethodNormalizedSHA256
		return base, 3, true
	}
	withinAge := datesWithin(document, candidate, config.ClusterMaxAge)
	metadataMatch := normalizedTitle != "" && normalizedTitle == candidate.NormalizedTitle &&
		((normalizedAuthor != "" && normalizedAuthor == candidate.NormalizedAuthor) ||
			(packageName != "" && version != "" && packageName == candidate.PackageName && version == candidate.Version))
	if withinAge && metadataMatch {
		base.Outcome, base.Method = OutcomeDuplicate, MethodMetadata
		return base, 4, true
	}
	distance := SimHashDistance(document.SimHash, candidate.SimHash)
	if withinAge && metadataCompatible(packageName, version, candidate) && document.SimHash != 0 && candidate.SimHash != 0 && distance <= config.SimHashDistance {
		base.Outcome, base.Method = OutcomeDuplicate, MethodSimHash
		base.SimHashDistance = distance
		base.Similarity = 1 - float64(distance)/64
		return base, 5, true
	}
	if withinAge &&
		metadataCompatible(packageName, version, candidate) &&
		candidate.EmbeddingSimilarity >= config.EmbeddingSimilarity &&
		candidate.EmbeddingSimilarity <= 1 {
		base.Outcome, base.Method = OutcomeCluster, MethodEmbedding
		base.Similarity = candidate.EmbeddingSimilarity
		return base, 6, true
	}
	if withinAge && packageName != "" && version != "" && packageName == candidate.PackageName && version == candidate.Version {
		base.Outcome, base.Method = OutcomeCluster, MethodPackageVersion
		base.SimHashDistance = distance
		base.Similarity = math.Max(0, 1-float64(distance)/64)
		return base, 7, true
	}
	return Decision{}, 0, false
}

func metadataCompatible(packageName string, version string, candidate Candidate) bool {
	if packageName != "" && candidate.PackageName != "" && packageName != candidate.PackageName {
		return false
	}
	if version != "" && candidate.Version != "" && version != candidate.Version {
		return false
	}
	return true
}

func datesWithin(document Document, candidate Candidate, maximumAge time.Duration) bool {
	first := document.PublishedAt
	if first.IsZero() {
		first = document.FirstSeenAt
	}
	second := candidate.PublishedAt
	if second.IsZero() {
		second = candidate.FirstSeenAt
	}
	if first.IsZero() || second.IsZero() {
		return false
	}
	difference := first.Sub(second)
	if difference < 0 {
		difference = -difference
	}
	return difference <= maximumAge
}

func trustTierRank(tier string) int {
	switch strings.ToUpper(tier) {
	case "T0":
		return 0
	case "T1":
		return 1
	case "T2":
		return 2
	case "T3":
		return 3
	default:
		return 4
	}
}
