package fetcher

import (
	"io"
	"strings"
	"testing"
)

func TestReleaseReadCloserReleasesExactlyOnce(t *testing.T) {
	releases := 0
	body := newReleaseReadCloser(io.NopCloser(strings.NewReader("fixture")), func() { releases++ })
	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if releases != 1 {
		t.Fatalf("release count = %d, want 1", releases)
	}
}
