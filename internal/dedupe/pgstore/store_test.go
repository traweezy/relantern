package pgstore

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/dedupe"
)

func TestStorageEncodingAndPresentationHelpers(t *testing.T) {
	t.Parallel()

	digest := sha256.Sum256([]byte("fixture"))
	var copied [sha256.Size]byte
	if err := copyDigest(&copied, digest[:]); err != nil || copied != digest {
		t.Fatalf("copyDigest() = %x, %v", copied, err)
	}
	if err := copyDigest(&copied, digest[:sha256.Size-1]); err == nil {
		t.Fatal("copyDigest() accepted a short digest")
	}

	const fingerprint uint64 = 0xfedcba9876543210
	encoded := encodeSimHash(fingerprint)
	decoded, err := decodeSimHash(encoded)
	if err != nil || decoded != fingerprint {
		t.Fatalf("SimHash round trip = %x, %v", decoded, err)
	}
	if _, err := decodeSimHash(encoded[:7]); err == nil {
		t.Fatal("decodeSimHash() accepted a short fingerprint")
	}

	document := dedupe.Document{
		RevisionID:   "01a04edd-9cc3-7110-81c4-a794321f5f87",
		CanonicalURL: "https://example.test/story",
	}
	if title := titleFor(document); title != document.CanonicalURL {
		t.Fatalf("titleFor() = %q", title)
	}
	if slug := slugFor(document); slug != "https-example-test-story-01a04edd9cc3" {
		t.Fatalf("slugFor() = %q", slug)
	}
	document.Title = strings.Repeat("Release ", 30) + "版本"
	if slug := slugFor(document); len(slug) > 93 || !strings.HasSuffix(slug, "-01a04edd9cc3") {
		t.Fatalf("long slugFor() = %q", slug)
	}
	document.Title = "版本发布"
	if slug := slugFor(document); slug != "story-01a04edd9cc3" {
		t.Fatalf("non-ASCII slugFor() = %q", slug)
	}
	document.Title = strings.Repeat("界", 1001)
	if title := titleFor(document); len([]rune(title)) != 1000 {
		t.Fatalf("bounded title has %d characters", len([]rune(title)))
	}

	for tier, want := range map[string]int{"T0": 0, "T1": 1, "T2": 2, "T3": 3, "unknown": 4} {
		if got := tierRank(tier); got != want {
			t.Errorf("tierRank(%q) = %d, want %d", tier, got, want)
		}
	}
	for tier, want := range map[string]string{"T0": "clustered", "T1": "clustered", "T2": "needs_review", "T3": "needs_review"} {
		if got := lifecycleFor(tier); got != want {
			t.Errorf("lifecycleFor(%q) = %q, want %q", tier, got, want)
		}
	}
	if nullableTime(time.Time{}) != nil {
		t.Fatal("nullableTime() returned a value for zero time")
	}
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	if value := nullableTime(now); value == nil || !value.Equal(now) {
		t.Fatalf("nullableTime() = %v", value)
	}
}
