package reembedding

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/traweezy/relantern/internal/embedding"
)

func TestBoundedEmbeddingInputIsDeterministicAndUTF8Safe(t *testing.T) {
	t.Parallel()
	input := "  stable normalized content  "
	bounded, err := boundedEmbeddingInput(input)
	if err != nil || bounded != "stable normalized content" {
		t.Fatalf("boundedEmbeddingInput() = %q, %v", bounded, err)
	}

	long := strings.Repeat("x", embedding.MaximumInputBytes-1) + "é" + "tail"
	bounded, err = boundedEmbeddingInput(long)
	if err != nil {
		t.Fatalf("boundedEmbeddingInput(long) error = %v", err)
	}
	if len(bounded) > embedding.MaximumInputBytes || !utf8.ValidString(bounded) || strings.HasSuffix(bounded, "tail") {
		t.Fatalf("bounded long input bytes = %d, valid = %t", len(bounded), utf8.ValidString(bounded))
	}
	if _, err := boundedEmbeddingInput("   "); err == nil {
		t.Fatal("boundedEmbeddingInput() accepted blank input")
	}
}
