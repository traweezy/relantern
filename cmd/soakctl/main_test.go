package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunReportsIncompleteTemplateAndGatesPass(t *testing.T) {
	t.Parallel()
	template := `{
  "version": 1,
  "environment": "staging",
  "projectId": "",
  "releaseSha": "",
  "startedAt": "",
  "profile": {},
  "observations": [],
  "exercises": [],
  "ownerReview": {},
  "cost": {"actualCents": 0, "budgetCents": 1500, "hardCapCents": 1500},
  "nonCriticalSloMisses": [],
  "fatalFindings": []
}`
	ledgerPath := filepath.Join(t.TempDir(), "soak.json")
	if err := os.WriteFile(ledgerPath, []byte(template), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	now := func() time.Time { return time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC) }

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"--file", ledgerPath}, &stdout, &stderr, now); exitCode != 0 {
		t.Fatalf("run() exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"outcome": "in_progress"`) {
		t.Fatalf("run() output = %q", stdout.String())
	}
	if exitCode := run([]string{"--file", ledgerPath, "--require-pass"}, &stdout, &stderr, now); exitCode != 1 {
		t.Fatalf("run(require-pass) exit = %d, want 1", exitCode)
	}
}

func TestRunRejectsMissingFileFlag(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(nil, &stdout, &stderr, time.Now); exitCode != 2 {
		t.Fatalf("run() exit = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("run() stderr = %q", stderr.String())
	}
}

func TestRunRejectsInvalidInputsAndOutputFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args func(*testing.T) []string
	}{
		{
			name: "missing file",
			args: func(t *testing.T) []string {
				return []string{"--file", filepath.Join(t.TempDir(), "missing.json")}
			},
		},
		{
			name: "invalid JSON",
			args: func(t *testing.T) []string {
				return []string{"--file", writeFixture(t, `{`)}
			},
		},
		{
			name: "invalid ledger",
			args: func(t *testing.T) []string {
				return []string{"--file", writeFixture(t, `{"version":2,"environment":"staging"}`)}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exitCode := run(test.args(t), &stdout, &stderr, time.Now); exitCode != 1 {
				t.Fatalf("run() exit = %d, want 1", exitCode)
			}
		})
	}

	valid := `{
  "version": 1,
  "environment": "staging",
  "profile": {},
  "observations": [],
  "exercises": [],
  "ownerReview": {},
  "cost": {"budgetCents": 1, "hardCapCents": 1},
  "nonCriticalSloMisses": [],
  "fatalFindings": []
}`
	var stderr bytes.Buffer
	if exitCode := run(
		[]string{"--file", writeFixture(t, valid)},
		failingWriter{},
		&stderr,
		time.Now,
	); exitCode != 1 || !strings.Contains(stderr.String(), "write soak report") {
		t.Fatalf("run() exit = %d, stderr = %q", exitCode, stderr.String())
	}
}

func writeFixture(t *testing.T, value string) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "soak.json")
	if err := os.WriteFile(fixture, []byte(value), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return fixture
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, os.ErrClosed
}
