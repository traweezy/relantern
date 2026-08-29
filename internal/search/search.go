package search

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

const (
	DefaultRRFK        = 60
	DefaultResultLimit = 20
	MaximumResultLimit = 100
)

type RankedID struct {
	ID   string
	Rank int
}

type FusedRank struct {
	ID           string
	Score        float64
	KeywordRank  *int
	SemanticRank *int
}

func Fuse(keyword []RankedID, semantic []RankedID, k int) ([]FusedRank, error) {
	if k < 1 || k > 1000 {
		return nil, errors.New("RRF k must be between 1 and 1000")
	}
	results := make(map[string]FusedRank, len(keyword)+len(semantic))
	add := func(ranked []RankedID, keywordList bool) error {
		seen := make(map[string]struct{}, len(ranked))
		for _, candidate := range ranked {
			if strings.TrimSpace(candidate.ID) == "" || candidate.Rank < 1 {
				return errors.New("ranked candidates require a non-empty ID and positive rank")
			}
			if _, duplicate := seen[candidate.ID]; duplicate {
				return errors.New("a ranking list may contain each candidate only once")
			}
			seen[candidate.ID] = struct{}{}
			result := results[candidate.ID]
			result.ID = candidate.ID
			rank := candidate.Rank
			result.Score += 1 / float64(k+rank)
			if keywordList {
				result.KeywordRank = &rank
			} else {
				result.SemanticRank = &rank
			}
			results[candidate.ID] = result
		}
		return nil
	}
	if err := add(keyword, true); err != nil {
		return nil, err
	}
	if err := add(semantic, false); err != nil {
		return nil, err
	}
	fused := make([]FusedRank, 0, len(results))
	for _, result := range results {
		fused = append(fused, result)
	}
	sort.Slice(fused, func(left int, right int) bool {
		if fused[left].Score != fused[right].Score {
			return fused[left].Score > fused[right].Score
		}
		leftBest := bestRank(fused[left])
		rightBest := bestRank(fused[right])
		if leftBest != rightBest {
			return leftBest < rightBest
		}
		return fused[left].ID < fused[right].ID
	})
	return fused, nil
}

func ValidateQuery(query string) error {
	trimmed := strings.TrimSpace(query)
	if len([]byte(trimmed)) < 2 || len([]byte(trimmed)) > 500 {
		return errors.New("search query must contain between 2 and 500 bytes")
	}
	if !strings.ContainsFunc(trimmed, func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsNumber(character)
	}) {
		return errors.New("search query must contain a letter or number")
	}
	return nil
}

func bestRank(result FusedRank) int {
	best := int(^uint(0) >> 1)
	if result.KeywordRank != nil && *result.KeywordRank < best {
		best = *result.KeywordRank
	}
	if result.SemanticRank != nil && *result.SemanticRank < best {
		best = *result.SemanticRank
	}
	return best
}
