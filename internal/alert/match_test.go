package alert

import "testing"

func TestMatchConfirmedCriticalAdvisory(t *testing.T) {
	advisory := Advisory{
		ID: "GHSA-abcd-1234-efgh", Kind: "reviewed", Severity: "critical",
		PublishedAt: "2026-09-20T01:00:00Z", ReviewedAt: "2026-09-20T01:05:00Z",
		Vulnerabilities: []Vulnerability{{
			Ecosystem: "go", PackageName: "example.com/library",
			VersionRange: ">= 1.2.0, < 1.4.3", PatchedVersion: "1.4.3",
		}},
	}
	watches := []Watch{
		{ID: "affected", Ecosystem: "go", PackageName: "example.com/library", CurrentVersion: "v1.4.2", Status: "active"},
		{ID: "patched", Ecosystem: "go", PackageName: "example.com/library", CurrentVersion: "v1.4.3", Status: "active"},
		{ID: "other-ecosystem", Ecosystem: "npm", PackageName: "example.com/library", CurrentVersion: "1.4.2", Status: "active"},
		{ID: "unknown-version", Ecosystem: "go", PackageName: "example.com/library", CurrentVersion: "latest", Status: "active"},
	}
	matches := Match(advisory, watches)
	if len(matches) != 1 || matches[0].Watch.ID != "affected" {
		t.Fatalf("Match() = %#v, want only the affected Go watch", matches)
	}
}

func TestMatchFailsClosedOnUnconfirmedOrUnsupportedEvidence(t *testing.T) {
	base := Advisory{
		ID: "GHSA-abcd-1234-efgh", Kind: "reviewed", Severity: "critical",
		PublishedAt: "2026-09-20T01:00:00Z", ReviewedAt: "2026-09-20T01:05:00Z",
		Vulnerabilities: []Vulnerability{{Ecosystem: "npm", PackageName: "@example/library", VersionRange: ">= 1.0.0, < 2.0.0"}},
	}
	watch := []Watch{{ID: "one", Ecosystem: "npm", PackageName: "@example/library", CurrentVersion: "1.5.0", Status: "active"}}
	cases := []struct {
		name   string
		change func(*Advisory, *[]Watch)
	}{
		{"community rumor", func(advisory *Advisory, _ *[]Watch) { advisory.Kind = "unreviewed" }},
		{"high severity", func(advisory *Advisory, _ *[]Watch) { advisory.Severity = "high" }},
		{"withdrawn", func(advisory *Advisory, _ *[]Watch) { advisory.WithdrawnAt = "2026-09-20T02:00:00Z" }},
		{"missing review", func(advisory *Advisory, _ *[]Watch) { advisory.ReviewedAt = "" }},
		{"unpublished", func(advisory *Advisory, _ *[]Watch) { advisory.PublishedAt = "" }},
		{"unsupported range", func(advisory *Advisory, _ *[]Watch) { advisory.Vulnerabilities[0].VersionRange = "^1.0.0" }},
		{"prerelease version", func(_ *Advisory, watches *[]Watch) { (*watches)[0].CurrentVersion = "1.5.0-rc.1" }},
		{"legacy watch", func(_ *Advisory, watches *[]Watch) { (*watches)[0].Ecosystem = "" }},
		{"nonactive watch", func(_ *Advisory, watches *[]Watch) { (*watches)[0].Status = "planned" }},
		{"unsupported ecosystem", func(advisory *Advisory, watches *[]Watch) {
			advisory.Vulnerabilities[0].Ecosystem = "maven"
			(*watches)[0].Ecosystem = "maven"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			advisory := base
			advisory.Vulnerabilities = append([]Vulnerability(nil), base.Vulnerabilities...)
			watches := append([]Watch(nil), watch...)
			test.change(&advisory, &watches)
			if matches := Match(advisory, watches); len(matches) != 0 {
				t.Fatalf("Match() = %#v, want no urgent match", matches)
			}
		})
	}
}

func TestMatchCoalescesRepeatedRangesForOneWatch(t *testing.T) {
	advisory := Advisory{
		ID: "GHSA-abcd-1234-efgh", State: "published", Severity: "critical",
		PublishedAt: "2026-09-20T01:00:00Z",
		Vulnerabilities: []Vulnerability{
			{Ecosystem: "rust", PackageName: "sample", VersionRange: ">= 1.0.0, < 2.0.0"},
			{Ecosystem: "rust", PackageName: "sample", VersionRange: ">= 1.1.0, < 1.9.0"},
		},
	}
	watches := []Watch{{ID: "one", Ecosystem: "rust", PackageName: "sample", CurrentVersion: "1.5.0", Status: "active"}}
	if matches := Match(advisory, watches); len(matches) != 1 {
		t.Fatalf("Match() returned %d matches, want one", len(matches))
	}
}
