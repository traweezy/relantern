package dedupe

import (
	"net/url"
	"testing"
)

func FuzzCanonicalizeURLIsIdempotent(fuzzer *testing.F) {
	for _, seed := range []string{
		"https://go.dev/blog/release?utm_source=feed&a=2&a=1#section",
		"http://127.0.0.1:80/status",
		"https://[2001:db8::1]:443/release",
		"not a URL",
	} {
		fuzzer.Add(seed)
	}
	fuzzer.Fuzz(func(t *testing.T, value string) {
		canonical, err := CanonicalizeURL(value)
		if err != nil {
			return
		}
		repeated, err := CanonicalizeURL(canonical)
		if err != nil {
			t.Fatalf("canonical output failed to canonicalize: %v", err)
		}
		if repeated != canonical {
			t.Fatalf("canonicalization is not idempotent: %q then %q", canonical, repeated)
		}
		parsed, err := url.Parse(canonical)
		if err != nil || parsed.Fragment != "" || parsed.User != nil {
			t.Fatalf("canonical URL retained forbidden components: %q", canonical)
		}
	})
}

func FuzzSimHashAndMetadataAreDeterministic(fuzzer *testing.F) {
	for _, seed := range []string{"Go 1.27 release", "React Compiler", "版本发布", ""} {
		fuzzer.Add(seed)
	}
	fuzzer.Fuzz(func(t *testing.T, value string) {
		fingerprint := SimHash(value)
		if SimHash(value) != fingerprint || SimHashDistance(fingerprint, fingerprint) != 0 {
			t.Fatal("SimHash is not deterministic")
		}
		normalized := NormalizeMetadata(value)
		if NormalizeMetadata(normalized) != normalized {
			t.Fatalf("metadata normalization is not idempotent: %q", normalized)
		}
	})
}
