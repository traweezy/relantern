package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/release"
)

const commandTestSHA = "0123456789abcdef0123456789abcdef01234567"

func TestRunSourceAndDescribe(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC) }
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"source",
		"--event-name", "pull_request",
		"--base-ref", "master",
		"--head-ref", "staging",
		"--base-repository", "traweezy/relantern",
		"--head-repository", "traweezy/relantern",
	}, &stdout, &stderr, now)
	if code != 0 || !strings.Contains(stdout.String(), `"source": "staging"`) {
		t.Fatalf("run(source) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifest := release.Manifest{
		Version: 1, Tag: "v1.2.3", ApprovedReleaseSHA: commandTestSHA,
		ApprovedTreeSHA: strings.Repeat("a", 40), SourceBranch: "staging",
		Decision: "approved", DecidedAt: "2026-08-30T00:00:00Z",
	}
	writeCommandJSON(t, manifestPath, manifest)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"describe", "--manifest", manifestPath, "--field", "approved-release-sha",
	}, &stdout, &stderr, now)
	if code != 0 || strings.TrimSpace(stdout.String()) != commandTestSHA {
		t.Fatalf("run(describe) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTagAndIntegrity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	referencePath := filepath.Join(root, "reference.json")
	tagPath := filepath.Join(root, "tag.json")
	writeCommandJSON(t, referencePath, map[string]any{
		"ref":    "refs/tags/v1.0.0",
		"object": map[string]string{"sha": strings.Repeat("a", 40), "type": "tag"},
	})
	writeCommandJSON(t, tagPath, map[string]any{
		"tag":          "v1.0.0",
		"object":       map[string]string{"sha": commandTestSHA, "type": "commit"},
		"verification": map[string]any{"verified": true, "reason": "valid"},
	})
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"tag", "--reference", referencePath, "--tag-object", tagPath,
		"--tag", "v1.0.0", "--commit", commandTestSHA,
	}, &stdout, &stderr, time.Now)
	if code != 0 || !strings.Contains(stdout.String(), `"verified": true`) {
		t.Fatalf("run(tag) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	pullsPath := filepath.Join(root, "pulls.json")
	writeCommandJSON(t, pullsPath, []map[string]any{{
		"number": 20, "state": "closed", "merged_at": "2026-08-30T00:00:00Z",
		"merge_commit_sha": commandTestSHA,
		"base":             map[string]any{"ref": "master", "repo": map[string]string{"full_name": "traweezy/relantern"}},
		"head":             map[string]any{"ref": "staging", "repo": map[string]string{"full_name": "traweezy/relantern"}},
		"labels":           []any{},
	}})
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"integrity", "--pulls", pullsPath, "--repository", "traweezy/relantern",
		"--commit", commandTestSHA, "--repository-root", root,
	}, &stdout, &stderr, time.Now)
	if code != 0 || !strings.Contains(stdout.String(), `"path": "release"`) {
		t.Fatalf("run(integrity) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidCommandsAndEvidence(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC) }
	tests := [][]string{
		nil,
		{"unknown"},
		{"source", "--event-name", "push"},
		{"tree"},
		{"evidence", "--manifest", "missing.json"},
		{"integrity"},
		{"tag"},
		{"provenance"},
		{"describe"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), args, &stdout, &stderr, now); code == 0 {
			t.Fatalf("run(%v) unexpectedly succeeded", args)
		}
	}
}

func writeCommandJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
