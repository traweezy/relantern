package fetcher

import (
	"context"
	"io"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func FuzzPolicyURL(f *testing.F) {
	for _, seed := range []string{
		"https://source.example/feed",
		"http://127.0.0.1:8090/feed",
		"https://owner@source.example/feed",
		"file:///etc/passwd",
	} {
		f.Add(seed)
	}
	policy, err := NewPolicy(
		staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}},
		[]string{"source.example", "127.0.0.1"},
		[]FixtureTarget{{Host: "127.0.0.1", Port: 8090}},
	)
	if err != nil {
		f.Fatalf("NewPolicy() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, rawURL string) {
		target, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			return
		}
		resolved, validationErr := policy.ValidateURL(context.Background(), target)
		if validationErr == nil && resolved.Host != "source.example" && resolved.Host != "127.0.0.1" {
			t.Fatalf("ValidateURL(%q) allowed unexpected host %q", rawURL, resolved.Host)
		}
	})
}

func FuzzBoundedBodyDecoding(f *testing.F) {
	f.Add("identity", []byte("fixture"))
	f.Add("gzip", []byte{0x1f, 0x8b})
	f.Add("br", []byte("fixture"))
	f.Fuzz(func(t *testing.T, encoding string, payload []byte) {
		if len(encoding) > 32 || len(payload) > 4096 {
			return
		}
		body, err := decodeBody(strings.NewReader(string(payload)), encoding, 1024, 1024)
		if err != nil {
			return
		}
		defer body.close()
		decoded, readErr := io.ReadAll(body.reader)
		if readErr == nil && len(decoded) > 1025 {
			t.Fatalf("decoded body length = %d", len(decoded))
		}
	})
}
