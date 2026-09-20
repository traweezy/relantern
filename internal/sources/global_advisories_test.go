package sources_test

import (
	"strings"
	"testing"

	"github.com/traweezy/relantern/internal/sources"
)

const nextGlobalAdvisoryPage = "https://api.github.com/advisories?after=opaque%2Bcursor&direction=desc&per_page=100&sort=updated&type=reviewed"

func TestIsGlobalAdvisoryPageURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "pinned root", url: sources.GlobalReviewedAdvisoriesURL, want: true},
		{name: "opaque after page", url: nextGlobalAdvisoryPage, want: true},
		{name: "query order is not an authority change", url: "https://api.github.com/advisories?type=reviewed&sort=updated&after=next&per_page=100&direction=desc", want: true},
		{name: "alternate root spelling", url: "https://api.github.com/advisories?type=reviewed&sort=updated&per_page=100&direction=desc"},
		{name: "missing after", url: "https://api.github.com/advisories?direction=desc&per_page=100&sort=updated&type=reviewed&after="},
		{name: "duplicate after", url: nextGlobalAdvisoryPage + "&after=second"},
		{name: "duplicate reviewed filter", url: nextGlobalAdvisoryPage + "&type=malware"},
		{name: "unknown filter", url: nextGlobalAdvisoryPage + "&ecosystem=npm"},
		{name: "encoded query key", url: "https://api.github.com/advisories?%61fter=next&direction=desc&per_page=100&sort=updated&type=reviewed"},
		{name: "unreviewed", url: strings.Replace(nextGlobalAdvisoryPage, "type=reviewed", "type=unreviewed", 1)},
		{name: "descending changed", url: strings.Replace(nextGlobalAdvisoryPage, "direction=desc", "direction=asc", 1)},
		{name: "page size changed", url: strings.Replace(nextGlobalAdvisoryPage, "per_page=100", "per_page=1000", 1)},
		{name: "off origin", url: strings.Replace(nextGlobalAdvisoryPage, "api.github.com", "api.github.com.evil.example", 1)},
		{name: "userinfo", url: strings.Replace(nextGlobalAdvisoryPage, "https://", "https://owner@", 1)},
		{name: "nondefault port", url: strings.Replace(nextGlobalAdvisoryPage, "api.github.com", "api.github.com:443", 1)},
		{name: "wrong path", url: strings.Replace(nextGlobalAdvisoryPage, "/advisories?", "/repos/owner/repo/security-advisories?", 1)},
		{name: "fragment", url: nextGlobalAdvisoryPage + "#next"},
		{name: "oversized cursor", url: strings.Replace(nextGlobalAdvisoryPage, "opaque%2Bcursor", strings.Repeat("a", 1025), 1)},
		{name: "control cursor", url: strings.Replace(nextGlobalAdvisoryPage, "opaque%2Bcursor", "one%0Atwo", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sources.IsGlobalAdvisoryPageURL(test.url); got != test.want {
				t.Fatalf("IsGlobalAdvisoryPageURL() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestGlobalAdvisoryNextPage(t *testing.T) {
	root := sources.GlobalReviewedAdvisoriesURL
	tests := []struct {
		name    string
		current string
		link    string
		want    string
		wantErr bool
	}{
		{name: "no next", current: root, link: "", want: ""},
		{name: "last only", current: root, link: "<" + nextGlobalAdvisoryPage + ">; rel=last", want: ""},
		{name: "next and last", current: root, link: "<" + nextGlobalAdvisoryPage + ">; rel=next, <" + nextGlobalAdvisoryPage + ">; rel=last", want: nextGlobalAdvisoryPage},
		{name: "quoted parameter comma", current: root, link: "<" + nextGlobalAdvisoryPage + ">; title=\"a,b\"; rel=\"next\"", want: nextGlobalAdvisoryPage},
		{name: "next after prior page", current: nextGlobalAdvisoryPage, link: "<" + strings.Replace(nextGlobalAdvisoryPage, "opaque%2Bcursor", "second", 1) + ">; rel=next", want: strings.Replace(nextGlobalAdvisoryPage, "opaque%2Bcursor", "second", 1)},
		{name: "self link", current: nextGlobalAdvisoryPage, link: "<" + nextGlobalAdvisoryPage + ">; rel=next", wantErr: true},
		{name: "same cursor with different query order", current: nextGlobalAdvisoryPage, link: "<https://api.github.com/advisories?type=reviewed&sort=updated&after=opaque%2Bcursor&per_page=100&direction=desc>; rel=next", wantErr: true},
		{name: "root loop", current: nextGlobalAdvisoryPage, link: "<" + root + ">; rel=next", wantErr: true},
		{name: "duplicate next", current: root, link: "<" + nextGlobalAdvisoryPage + ">; rel=next, <" + nextGlobalAdvisoryPage + ">; rel=next", wantErr: true},
		{name: "unknown query", current: root, link: "<" + nextGlobalAdvisoryPage + "&ecosystem=npm>; rel=next", wantErr: true},
		{name: "off origin", current: root, link: "<https://api.github.com.evil.example/advisories?after=next&direction=desc&per_page=100&sort=updated&type=reviewed>; rel=next", wantErr: true},
		{name: "relative URL", current: root, link: "</advisories?after=next>; rel=next", wantErr: true},
		{name: "malformed relation", current: root, link: "<" + nextGlobalAdvisoryPage + ">; rel=\"next", wantErr: true},
		{name: "repeated rel", current: root, link: "<" + nextGlobalAdvisoryPage + ">; rel=next; rel=last", wantErr: true},
		{name: "oversized header", current: root, link: strings.Repeat("a", 8193), wantErr: true},
		{name: "invalid current", current: "https://api.github.com/advisories", link: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sources.GlobalAdvisoryNextPage(test.current, test.link)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("GlobalAdvisoryNextPage() = %q, %v; want %q, error %t", got, err, test.want, test.wantErr)
			}
		})
	}
}
