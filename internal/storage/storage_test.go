package storage_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/storage"
)

func TestRawObjectKeyIsContentAddressedAndUTC(t *testing.T) {
	digest := sha256.Sum256([]byte("fixture"))
	observedAt := time.Date(2026, time.August, 29, 23, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

	key, err := storage.RawObjectKey("go-blog", observedAt, digest, "application/atom+xml; charset=utf-8")
	if err != nil {
		t.Fatalf("RawObjectKey() error = %v", err)
	}
	want := "raw/go-blog/2026/08/30/f16d05ec6b29248d2c61adb1e9263f78e4f7bace1b955014a2d17872cfe4064d.xml"
	if key != want {
		t.Fatalf("RawObjectKey() = %q, want %q", key, want)
	}
}

func TestRawObjectKeyRejectsUnsafeSourceID(t *testing.T) {
	if _, err := storage.RawObjectKey("../owner", time.Now(), sha256.Sum256(nil), "text/plain"); err == nil {
		t.Fatal("RawObjectKey() accepted a path-traversal source id")
	}
}

func TestRawObjectKeySelectsSafeExtension(t *testing.T) {
	tests := []struct {
		contentType string
		extension   string
	}{
		{contentType: "application/json", extension: ".json"},
		{contentType: "text/html; charset=utf-8", extension: ".html"},
		{contentType: "text/plain", extension: ".txt"},
		{contentType: "application/octet-stream", extension: ".bin"},
		{contentType: "not a media type", extension: ".bin"},
	}
	digest := sha256.Sum256([]byte("fixture"))
	for _, test := range tests {
		key, err := storage.RawObjectKey("fixture", time.Date(2026, time.August, 29, 0, 0, 0, 0, time.UTC), digest, test.contentType)
		if err != nil {
			t.Fatalf("RawObjectKey(%q) error = %v", test.contentType, err)
		}
		if len(key) < len(test.extension) || key[len(key)-len(test.extension):] != test.extension {
			t.Fatalf("RawObjectKey(%q) = %q, want suffix %q", test.contentType, key, test.extension)
		}
	}
}

func TestRawObjectKeyRequiresObservationTime(t *testing.T) {
	if _, err := storage.RawObjectKey("fixture", time.Time{}, sha256.Sum256(nil), "text/plain"); err == nil {
		t.Fatal("RawObjectKey() accepted a zero observation time")
	}
}

func TestRawFetchObjectKeySeparatesEndpointAndURL(t *testing.T) {
	digest := sha256.Sum256([]byte("same body"))
	day := time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)
	first, err := storage.RawFetchObjectKey("github", "github-releases", "https://api.github.com/repos/a/releases", day, digest, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range [][2]string{
		{"github-advisories", "https://api.github.com/repos/a/releases"},
		{"github-releases", "https://api.github.com/repos/b/releases"},
	} {
		other, err := storage.RawFetchObjectKey("github", identity[0], identity[1], day, digest, "application/json")
		if err != nil {
			t.Fatal(err)
		}
		if first == other {
			t.Fatalf("separate fetch identities shared object key %q", first)
		}
		if err := storage.ValidateObjectKey(other); err != nil {
			t.Fatal(err)
		}
	}
	repeated, err := storage.RawFetchObjectKey("github", "github-releases", "https://api.github.com/repos/a/releases", day, digest, "application/json")
	if err != nil || repeated != first {
		t.Fatalf("repeat key = %q, %v, want %q", repeated, err, first)
	}
	if _, err := storage.RawFetchObjectKey("github", "", "https://api.github.com", day, digest, "application/json"); err == nil {
		t.Fatal("missing endpoint registry ID was accepted")
	}
}

func TestNormalizedObjectKeyIsContentAddressed(t *testing.T) {
	digest := sha256.Sum256([]byte("normalized fixture"))
	key, err := storage.NormalizedObjectKey("go-blog", digest)
	if err != nil {
		t.Fatalf("NormalizedObjectKey() error = %v", err)
	}
	want := "normalized/go-blog/c3004bb0ca2ddef6411f72e21505dc931b49cdbc3b0ab30ab018b9c8ef0d4656.txt"
	if key != want {
		t.Fatalf("NormalizedObjectKey() = %q, want %q", key, want)
	}
	if err := storage.ValidateObjectKey(key); err != nil {
		t.Fatalf("ValidateObjectKey() error = %v", err)
	}
}

func TestObjectKeysRejectUnapprovedNamespacesAndShapes(t *testing.T) {
	digest := sha256.Sum256([]byte("fixture"))
	if _, err := storage.NormalizedObjectKey("../owner", digest); err == nil {
		t.Fatal("NormalizedObjectKey() accepted a path-traversal source id")
	}
	for _, key := range []string{
		"raw/../owner/2026/08/29/" + fmt.Sprintf("%x", digest) + ".xml",
		"raw/source/not-a-date/" + fmt.Sprintf("%x", digest) + ".xml",
		"raw/source/2026/99/99/" + fmt.Sprintf("%x", digest) + ".xml",
		"normalized/source/../" + fmt.Sprintf("%x", digest) + ".txt",
		"normalized/source/not-a-digest.txt",
		"other/source/" + fmt.Sprintf("%x", digest) + ".txt",
	} {
		if err := storage.ValidateObjectKey(key); err == nil {
			t.Fatalf("ValidateObjectKey(%q) succeeded", key)
		}
	}
}
