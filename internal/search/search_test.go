package search_test

import (
	"testing"

	"github.com/traweezy/relantern/internal/search"
)

func TestFuseUsesDeterministicReciprocalRankFusion(t *testing.T) {
	t.Parallel()
	results, err := search.Fuse(
		[]search.RankedID{{ID: "keyword-only", Rank: 1}, {ID: "both", Rank: 2}},
		[]search.RankedID{{ID: "both", Rank: 1}, {ID: "semantic-only", Rank: 2}},
		search.DefaultRRFK,
	)
	if err != nil {
		t.Fatalf("Fuse() error = %v", err)
	}
	if len(results) != 3 || results[0].ID != "both" || results[0].KeywordRank == nil || results[0].SemanticRank == nil {
		t.Fatalf("Fuse() = %+v", results)
	}
	if results[1].ID != "keyword-only" || results[2].ID != "semantic-only" {
		t.Fatalf("tie order = %+v", results)
	}
}

func TestFuseBreaksEqualScoresByRankThenID(t *testing.T) {
	t.Parallel()
	results, err := search.Fuse(
		[]search.RankedID{{ID: "z-keyword", Rank: 1}, {ID: "both", Rank: 3}},
		[]search.RankedID{{ID: "a-semantic", Rank: 1}, {ID: "both", Rank: 4}},
		search.DefaultRRFK,
	)
	if err != nil {
		t.Fatalf("Fuse() error = %v", err)
	}
	if len(results) != 3 || results[0].ID != "both" || results[1].ID != "a-semantic" || results[2].ID != "z-keyword" {
		t.Fatalf("equal-score order = %+v", results)
	}
	empty, err := search.Fuse(nil, nil, search.DefaultRRFK)
	if err != nil || len(empty) != 0 {
		t.Fatalf("Fuse(empty) = %+v, %v", empty, err)
	}
}

func TestFuseAndQueryValidationRejectInvalidBoundaries(t *testing.T) {
	t.Parallel()
	if _, err := search.Fuse(nil, nil, 0); err == nil {
		t.Fatal("Fuse() accepted zero k")
	}
	for _, ranked := range [][]search.RankedID{
		{{ID: "", Rank: 1}},
		{{ID: "item", Rank: 0}},
		{{ID: "item", Rank: 1}, {ID: "item", Rank: 2}},
	} {
		if _, err := search.Fuse(ranked, nil, search.DefaultRRFK); err == nil {
			t.Errorf("Fuse() accepted %+v", ranked)
		}
	}
	for _, query := range []string{"", "?", "x", string(make([]byte, 501)), "a" + string(make([]byte, 500))} {
		if err := search.ValidateQuery(query); err == nil {
			t.Errorf("ValidateQuery() accepted %q", query)
		}
	}
	if err := search.ValidateQuery("database failover"); err != nil {
		t.Fatalf("ValidateQuery() error = %v", err)
	}
}
