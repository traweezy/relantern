package manualcapture

import (
	"testing"

	"github.com/traweezy/relantern/internal/embedding"
)

func TestRobotsDisallowsOnlyActiveWildcardRules(t *testing.T) {
	t.Parallel()
	document := "User-agent: other\nDisallow: /private\nUser-agent: *\nDisallow: /blocked\n"
	if !robotsDisallows(document, "/blocked/story") {
		t.Fatal("wildcard robots rule did not block matching path")
	}
	if robotsDisallows(document, "/private/story") {
		t.Fatal("unrelated user-agent rule blocked path")
	}
}

func TestBoundedEmbeddingInputPreservesUTF8Boundary(t *testing.T) {
	t.Parallel()
	input := string(make([]byte, embedding.MaximumInputBytes-1)) + "é"
	bounded, err := boundedEmbeddingInput(input)
	if err != nil {
		t.Fatalf("boundedEmbeddingInput() error = %v", err)
	}
	if len([]byte(bounded)) > embedding.MaximumInputBytes {
		t.Fatalf("bounded input bytes = %d", len([]byte(bounded)))
	}
}
