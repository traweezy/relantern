package alert

import (
	"strconv"
	"strings"
	"time"
)

// Advisory is the bounded metadata retained from an official advisory entry.
// A source's identity and trust tier must be verified separately before Match.
type Advisory struct {
	ID              string
	Kind            string
	State           string
	Severity        string
	PublishedAt     string
	ReviewedAt      string
	WithdrawnAt     string
	Vulnerabilities []Vulnerability
}

type Vulnerability struct {
	Ecosystem      string
	PackageName    string
	VersionRange   string
	PatchedVersion string
}

type Watch struct {
	ID             string
	Ecosystem      string
	PackageName    string
	CurrentVersion string
	Status         string
}

type MatchResult struct {
	Watch         Watch
	Vulnerability Vulnerability
}

// Match only confirms advisories with a published, unwithdrawn official
// record and a proven affected installed version. Unsupported range syntax
// remains review-only rather than becoming an urgent alert.
func Match(advisory Advisory, watches []Watch) []MatchResult {
	if !confirmed(advisory) {
		return nil
	}
	results := make([]MatchResult, 0)
	seen := make(map[string]bool)
	for _, vulnerability := range advisory.Vulnerabilities {
		if !supportedEcosystem(vulnerability.Ecosystem) ||
			vulnerability.PackageName == "" || len(vulnerability.PackageName) > 255 ||
			len(vulnerability.PatchedVersion) > 100 {
			continue
		}
		for _, watch := range watches {
			if watch.ID == "" || watch.Status != "active" ||
				watch.Ecosystem != vulnerability.Ecosystem ||
				watch.PackageName != vulnerability.PackageName || seen[watch.ID] {
				continue
			}
			if affected, supported := versionInRange(watch.CurrentVersion, vulnerability.VersionRange); supported && affected {
				results = append(results, MatchResult{Watch: watch, Vulnerability: vulnerability})
				seen[watch.ID] = true
			}
		}
	}
	return results
}

func confirmed(advisory Advisory) bool {
	if !validGHSAID(advisory.ID) || advisory.Severity != "critical" ||
		advisory.WithdrawnAt != "" || len(advisory.Vulnerabilities) == 0 ||
		len(advisory.Vulnerabilities) > 100 || !validTimestamp(advisory.PublishedAt) ||
		(advisory.State != "" && advisory.State != "published") {
		return false
	}
	return (advisory.Kind == "reviewed" && validTimestamp(advisory.ReviewedAt)) ||
		(advisory.Kind == "" && advisory.State == "published")
}

func validGHSAID(value string) bool {
	if len(value) != len("GHSA-xxxx-yyyy-zzzz") || !strings.HasPrefix(value, "GHSA-") {
		return false
	}
	for index, character := range value[5:] {
		if index == 4 || index == 9 {
			if character != '-' {
				return false
			}
			continue
		}
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && !parsed.IsZero()
}

func supportedEcosystem(value string) bool {
	switch value {
	case "go", "npm", "rust", "pub":
		return true
	default:
		return false
	}
}

type numericVersion [3]uint32

func parseNumericVersion(value string) (numericVersion, bool) {
	var parsed numericVersion
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" || len(value) > 32 {
		return parsed, false
	}
	parts := strings.Split(value, ".")
	if len(parts) > len(parsed) {
		return parsed, false
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return parsed, false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return parsed, false
			}
		}
		component, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return parsed, false
		}
		parsed[index] = uint32(component)
	}
	return parsed, true
}

func compareVersion(left, right numericVersion) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func versionInRange(version string, rawRange string) (bool, bool) {
	installed, ok := parseNumericVersion(version)
	if !ok || rawRange == "" || len(rawRange) > 200 {
		return false, false
	}
	clauses := strings.Split(rawRange, ",")
	if len(clauses) == 0 || len(clauses) > 8 {
		return false, false
	}
	affected := true
	for _, rawClause := range clauses {
		clause := strings.TrimSpace(rawClause)
		operator := ""
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(clause, candidate) {
				operator = candidate
				clause = strings.TrimSpace(strings.TrimPrefix(clause, candidate))
				break
			}
		}
		bound, valid := parseNumericVersion(clause)
		if !valid || operator == "" {
			return false, false
		}
		comparison := compareVersion(installed, bound)
		switch operator {
		case ">=":
			affected = affected && comparison >= 0
		case "<=":
			affected = affected && comparison <= 0
		case ">":
			affected = affected && comparison > 0
		case "<":
			affected = affected && comparison < 0
		case "=":
			affected = affected && comparison == 0
		}
	}
	return affected, true
}
