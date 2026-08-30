package release

import (
	"strings"
	"testing"
	"time"
)

func TestNewProvenanceBindsReleaseArchiveAndSBOM(t *testing.T) {
	t.Parallel()
	manifest := validManifest(testReleaseSHA, testTreeSHA)
	statement, err := NewProvenance(
		manifest,
		"traweezy/relantern",
		"https://github.com/traweezy/relantern/.github/workflows/release.yml@refs/heads/master",
		"1234-1",
		"relantern-v1.0.0-source.tar.gz",
		strings.Repeat("a", 64),
		"relantern-v1.0.0.spdx.json",
		strings.Repeat("b", 64),
		time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewProvenance() error = %v", err)
	}
	if len(statement.Subject) != 2 ||
		statement.Predicate.BuildDefinition.ExternalParameters.ApprovedReleaseSHA != testReleaseSHA ||
		statement.Predicate.BuildDefinition.ResolvedDependencies[0].Digest["gitTree"] != testTreeSHA {
		t.Fatalf("NewProvenance() = %+v", statement)
	}
}

func TestNewProvenanceRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()
	manifest := validManifest(testReleaseSHA, testTreeSHA)
	_, err := NewProvenance(
		manifest,
		"not a repository",
		"builder\nspoofed",
		"",
		"archive",
		"bad",
		"sbom",
		"bad",
		time.Time{},
	)
	if err == nil {
		t.Fatal("NewProvenance() accepted unsafe inputs")
	}
}
