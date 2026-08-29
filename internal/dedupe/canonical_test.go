package dedupe

import "testing"

func TestCanonicalizeURLRemovesTrackingAndNormalizesIdentity(t *testing.T) {
	t.Parallel()

	canonical, err := CanonicalizeURL("HTTPS://Go.Dev:443/blog/?utm_source=news&b=2&a=3&a=1#install")
	if err != nil {
		t.Fatalf("CanonicalizeURL() error = %v", err)
	}
	if canonical != "https://go.dev/blog/?a=1&a=3&b=2" {
		t.Fatalf("CanonicalizeURL() = %q", canonical)
	}
}

func TestCanonicalizeURLRejectsUnsafeIdentities(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"ftp://go.dev/release",
		"https://user:secret@go.dev/release",
		"https://./release",
	} {
		if _, err := CanonicalizeURL(value); err == nil {
			t.Errorf("CanonicalizeURL(%q) succeeded", value)
		}
	}
}

func TestCanonicalizeURLPreservesIPv6Authority(t *testing.T) {
	t.Parallel()

	canonical, err := CanonicalizeURL("https://[2001:0db8::1]:443/release")
	if err != nil {
		t.Fatalf("CanonicalizeURL() error = %v", err)
	}
	if canonical != "https://[2001:db8::1]/release" {
		t.Fatalf("CanonicalizeURL() = %q", canonical)
	}
}

func TestResolveDeclaredCanonicalHonorsOnlyReviewedBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fetched  string
		declared string
		aliases  []HostAlias
		want     string
		accepted bool
	}{
		{
			name:     "same registrable domain",
			fetched:  "https://blog.go.dev/release?utm_source=feed",
			declared: "https://go.dev/doc/release",
			want:     "https://go.dev/doc/release",
			accepted: true,
		},
		{
			name:     "foreign domain rejected",
			fetched:  "https://go.dev/release",
			declared: "https://attacker.test/copied",
			want:     "https://go.dev/release",
		},
		{
			name:     "explicit alias",
			fetched:  "https://react.dev/blog/release",
			declared: "https://reactjs.org/blog/release",
			aliases:  []HostAlias{{From: "react.dev", To: "reactjs.org"}},
			want:     "https://reactjs.org/blog/release",
			accepted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, accepted, err := ResolveDeclaredCanonical(test.fetched, test.declared, test.aliases)
			if err != nil {
				t.Fatalf("ResolveDeclaredCanonical() error = %v", err)
			}
			if got != test.want || accepted != test.accepted {
				t.Fatalf("ResolveDeclaredCanonical() = %q, %v; want %q, %v", got, accepted, test.want, test.accepted)
			}
		})
	}
}
