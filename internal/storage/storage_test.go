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
