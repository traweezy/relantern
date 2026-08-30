package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateIntegrityAcceptsMergedStagingRelease(t *testing.T) {
	t.Parallel()
	response := associatedPullRequest(t, "staging", nil)
	report, err := ValidateIntegrity(bytes.NewReader(response), t.TempDir(), "traweezy/relantern", testReleaseSHA, time.Now())
	if err != nil {
		t.Fatalf("ValidateIntegrity() error = %v", err)
	}
	if report.Path != "release" || report.PullRequest != 20 {
		t.Fatalf("ValidateIntegrity() report = %+v", report)
	}
}

func TestValidateIntegrityAcceptsDocumentedHotfix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	artifact := writeEvidence(t, root, "docs/evidence/hotfixes/incident.log", []byte("redacted incident evidence\n"))
	evidence := HotfixEvidence{
		Version: 1, PullRequest: 20,
		Reason:             "Emergency correction for a confirmed production availability incident.",
		IncidentReference:  "https://github.com/traweezy/relantern/issues/100",
		BackmergeReference: "https://github.com/traweezy/relantern/pull/101",
		ApprovedAt:         "2026-08-30T00:00:00Z", Evidence: []EvidenceReference{artifact},
	}
	writeJSONEvidence(t, root, "docs/evidence/hotfixes/pr-20.json", evidence)
	response := associatedPullRequest(t, "hotfix/availability", []string{"emergency-hotfix"})
	report, err := ValidateIntegrity(
		bytes.NewReader(response), root, "traweezy/relantern", testReleaseSHA,
		time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC),
	)
	if err != nil || report.Path != "hotfix" {
		t.Fatalf("ValidateIntegrity() = %+v, %v", report, err)
	}
}

func TestValidateIntegrityRejectsBypassAndFork(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response []byte
	}{
		{name: "direct push", response: []byte(`[]`)},
		{name: "wrong branch", response: associatedPullRequest(t, "feature/release", nil)},
		{name: "unlabeled hotfix", response: associatedPullRequest(t, "hotfix/availability", nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateIntegrity(bytes.NewReader(test.response), t.TempDir(), "traweezy/relantern", testReleaseSHA, time.Now())
			if err == nil || !strings.Contains(err.Error(), "not associated") {
				t.Fatalf("ValidateIntegrity() error = %v", err)
			}
		})
	}
}

func TestValidateIntegrityRejectsMalformedAndIncompleteHotfixEvidence(t *testing.T) {
	t.Parallel()
	if _, err := ValidateIntegrity(strings.NewReader(`not-json`), t.TempDir(), "traweezy/relantern", testReleaseSHA, time.Now()); err == nil {
		t.Fatal("ValidateIntegrity() accepted malformed GitHub data")
	}
	response := associatedPullRequest(t, "hotfix/availability", []string{"emergency-hotfix"})
	if _, err := ValidateIntegrity(bytes.NewReader(response), t.TempDir(), "traweezy/relantern", testReleaseSHA, time.Now()); err == nil ||
		!strings.Contains(err.Error(), "decode hotfix evidence") {
		t.Fatalf("ValidateIntegrity() error = %v", err)
	}
	if _, err := ValidateIntegrity(bytes.NewReader(response), t.TempDir(), "", "short", time.Now()); err == nil {
		t.Fatal("ValidateIntegrity() accepted invalid identity")
	}
}

func TestOpenPullRequests(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pulls.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenPullRequests(path)
	if err != nil {
		t.Fatalf("OpenPullRequests() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func associatedPullRequest(t *testing.T, head string, labels []string) []byte {
	t.Helper()
	labelValues := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		labelValues = append(labelValues, map[string]string{"name": label})
	}
	value := []map[string]any{{
		"number": 20, "state": "closed", "merged_at": "2026-08-30T00:00:00Z",
		"merge_commit_sha": testReleaseSHA,
		"base":             map[string]any{"ref": "master", "repo": map[string]string{"full_name": "traweezy/relantern"}},
		"head":             map[string]any{"ref": head, "repo": map[string]string{"full_name": "traweezy/relantern"}},
		"labels":           labelValues,
	}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(fmt.Errorf("encode pull request fixture: %w", err))
	}
	return raw
}
