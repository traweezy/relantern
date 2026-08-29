package storage_test

import (
	"crypto/sha256"
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
