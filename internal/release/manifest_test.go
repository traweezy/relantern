package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/soak"
)

const (
	testReleaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testTreeSHA    = "89abcdef0123456789abcdef0123456789abcdef"
	testProjectID  = "3bff9840-26d3-4f71-bca4-30335e154751"
)

func TestValidateEvidenceAcceptsCompleteReleaseDecision(t *testing.T) {
	t.Parallel()
	root, manifest, now := completeReleaseEvidence(t)

	report, err := ValidateEvidence(manifest, root, now)
	if err != nil {
		t.Fatalf("ValidateEvidence() error = %v", err)
	}
	if report.Tag != "v1.0.0" || report.SoakOutcome != "pass" || report.RestoreResult != "pass" ||
		report.SecurityResult != "pass" || report.PrepushResult != "pass" {
		t.Fatalf("ValidateEvidence() report = %+v", report)
	}
	if err := ValidateManifestPath(manifest, "docs/evidence/releases/v1.0.0.json"); err != nil {
		t.Fatalf("ValidateManifestPath() error = %v", err)
	}
}

func TestValidateEvidenceRejectsUntrustedClaims(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{name: "unstable tag", mutate: func(manifest *Manifest) { manifest.Tag = "v1.0.0-rc.1" }, want: "stable"},
		{name: "wrong source", mutate: func(manifest *Manifest) { manifest.SourceBranch = "feature" }, want: "sourceBranch"},
		{name: "future decision", mutate: func(manifest *Manifest) { manifest.DecidedAt = "2099-01-01T00:00:00Z" }, want: "future"},
		{name: "bad digest", mutate: func(manifest *Manifest) { manifest.SecurityGate.SHA256 = strings.Repeat("f", 64) }, want: "mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifest, now := completeReleaseEvidence(t)
			test.mutate(&manifest)
			_, err := ValidateEvidence(manifest, root, now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateEvidence() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestDecodeManifestIsStrictAndBounded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "unknown", raw: []byte(`{"version":1,"unknown":true}`), want: "unknown field"},
		{name: "trailing", raw: []byte(`{"version":1} {}`), want: "trailing"},
		{name: "oversized", raw: bytes.Repeat([]byte(" "), MaximumManifestBytes+1), want: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeManifest(bytes.NewReader(test.raw)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeManifest() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateManifestPathRejectsUnexpectedLocation(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Tag: "v1.0.0"}
	if err := ValidateManifestPath(manifest, "docs/evidence/releases/current.json"); err == nil {
		t.Fatal("ValidateManifestPath() accepted an unexpected location")
	}
}

func TestLoadAndValidateManifest(t *testing.T) {
	t.Parallel()
	root, manifest, now := completeReleaseEvidence(t)
	path := filepath.Join(root, "manifest.json")
	writeJSONEvidence(t, root, "manifest.json", manifest)
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if err := ValidateManifest(loaded, now); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestLoadManifestRejectsSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "manifest.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(link); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("LoadManifest() error = %v", err)
	}
}

func TestValidateEvidenceRejectsInvalidGateRestoreAndPath(t *testing.T) {
	t.Parallel()
	t.Run("missing gate command", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		var gate GateReport
		if err := decodeStrictFile(filepath.Join(root, filepath.FromSlash(manifest.SecurityGate.Path)), &gate); err != nil {
			t.Fatal(err)
		}
		gate.Commands = gate.Commands[:1]
		manifest.SecurityGate = writeJSONEvidence(t, root, "docs/evidence/releases/security-invalid.json", gate)
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "missing command") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
	t.Run("missed restore objective", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		var restore RestoreReport
		if err := decodeStrictFile(filepath.Join(root, filepath.FromSlash(manifest.RestoreDrill.Path)), &restore); err != nil {
			t.Fatal(err)
		}
		restore.RTOSeconds = restore.RTOTargetSeconds + 1
		manifest.RestoreDrill = writeJSONEvidence(t, root, "docs/evidence/releases/restore-invalid.json", restore)
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "RPO/RTO") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
	t.Run("unclean evidence path", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		manifest.PrepushGate.Path = "docs/evidence/releases/../releases/prepush.json"
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "clean") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
}

func TestValidateEvidenceRequiresDecisionAfterEveryGate(t *testing.T) {
	t.Parallel()
	t.Run("soak", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		manifest.DecidedAt = now.Add(-2 * time.Hour).Format(time.RFC3339)
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "must follow") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
	t.Run("restore", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		var restore RestoreReport
		if err := decodeStrictFile(filepath.Join(root, filepath.FromSlash(manifest.RestoreDrill.Path)), &restore); err != nil {
			t.Fatal(err)
		}
		restore.VerifiedAt = manifest.DecidedAt
		manifest.RestoreDrill = writeJSONEvidence(t, root, "docs/evidence/releases/restore-at-decision.json", restore)
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "before") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
	t.Run("security", func(t *testing.T) {
		root, manifest, now := completeReleaseEvidence(t)
		var gate GateReport
		if err := decodeStrictFile(filepath.Join(root, filepath.FromSlash(manifest.SecurityGate.Path)), &gate); err != nil {
			t.Fatal(err)
		}
		gate.CompletedAt = manifest.DecidedAt
		manifest.SecurityGate = writeJSONEvidence(t, root, "docs/evidence/releases/security-at-decision.json", gate)
		if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "before") {
			t.Fatalf("ValidateEvidence() error = %v", err)
		}
	})
}

func TestValidateEvidenceRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
	root, manifest, now := completeReleaseEvidence(t)
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "docs", "evidence", "releases", "escape.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("outside\n"))
	manifest.PrepushGate = EvidenceReference{
		Path: "docs/evidence/releases/escape.json", SHA256: fmt.Sprintf("%x", digest),
	}
	if _, err := ValidateEvidence(manifest, root, now); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("ValidateEvidence() error = %v", err)
	}
}

func completeReleaseEvidence(t *testing.T) (string, Manifest, time.Time) {
	t.Helper()
	root := t.TempDir()
	startedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(soak.MinimumDuration)
	now := endedAt.Add(2 * time.Hour)

	shared := writeEvidence(t, root, "docs/evidence/staging/shared.log", []byte("redacted immutable evidence\n"))
	ledger := completeSoakLedger(startedAt, shared)
	soakRaw, err := json.Marshal(ledger)
	if err != nil {
		t.Fatalf("encode soak fixture: %v", err)
	}
	soakReference := writeEvidence(t, root, "docs/evidence/staging/soak.json", soakRaw)

	restore := RestoreReport{
		AuditCount: 4, BackupSHA256: strings.Repeat("a", 64), DigestCount: 1,
		GitSHA: testReleaseSHA, MigrationVersion: 20, Result: "pass",
		RPOSeconds: 10, RPOTargetSeconds: 86400, RTOSeconds: 20, RTOTargetSeconds: 14400,
		ScheduleCount: 3, SourceCount: 91, UserCount: 1,
		VerifiedAt: endedAt.Add(15 * time.Minute).Format(time.RFC3339),
	}
	restoreReference := writeJSONEvidence(t, root, "docs/evidence/releases/restore.json", restore)
	security := GateReport{
		Version: 1, Kind: "security", ReleaseSHA: testReleaseSHA, Result: "pass",
		CompletedAt: endedAt.Add(30 * time.Minute).Format(time.RFC3339),
		Commands: []string{
			"make security-scan",
			"bash scripts/go-tool.sh run github.com/zricethezav/gitleaks/v8@v8.30.1 git --redact --no-banner",
			"pnpm audit --audit-level high",
			"pnpm audit signatures",
		},
		Evidence: []EvidenceReference{shared},
	}
	securityReference := writeJSONEvidence(t, root, "docs/evidence/releases/security.json", security)
	prepush := GateReport{
		Version: 1, Kind: "prepush", ReleaseSHA: testReleaseSHA, Result: "pass",
		CompletedAt: endedAt.Add(45 * time.Minute).Format(time.RFC3339),
		Commands:    []string{"make prepush", "make prodlike-smoke"}, Evidence: []EvidenceReference{shared},
	}
	prepushReference := writeJSONEvidence(t, root, "docs/evidence/releases/prepush.json", prepush)

	manifest := Manifest{
		Version: 1, Tag: "v1.0.0", ApprovedReleaseSHA: testReleaseSHA, ApprovedTreeSHA: testTreeSHA,
		SourceBranch: "staging", Decision: "approved", DecidedAt: endedAt.Add(time.Hour).Format(time.RFC3339),
		SoakLedger: soakReference, RestoreDrill: restoreReference,
		SecurityGate: securityReference, PrepushGate: prepushReference,
	}
	return root, manifest, now
}

func completeSoakLedger(start time.Time, artifact EvidenceReference) soak.Ledger {
	toSoakArtifact := func() []soak.Artifact {
		return []soak.Artifact{{Reference: artifact.Path, SHA256: artifact.SHA256}}
	}
	observations := make([]soak.Observation, 0, 15)
	for day := 0; day <= 14; day++ {
		observations = append(observations, soak.Observation{
			ObservedAt: start.Add(time.Duration(day) * 24 * time.Hour).Format(time.RFC3339),
			ReleaseSHA: testReleaseSHA, FreshnessSLOMet: true, QueueHealthy: true,
			Demo:     soak.DemoChecks{Uptime: true, Performance: true, Accessibility: true, Isolation: true},
			Evidence: toSoakArtifact(),
		})
	}
	exercise := func(kind soak.ExerciseKind, day int, provider string) soak.Exercise {
		return soak.Exercise{
			Kind: kind, OccurredAt: start.Add(time.Duration(day)*24*time.Hour + time.Hour).Format(time.RFC3339),
			ReleaseSHA: testReleaseSHA, Provider: provider, Passed: true, Evidence: toSoakArtifact(),
		}
	}
	return soak.Ledger{
		Version: 1, Environment: "staging", ProjectID: testProjectID, ReleaseSHA: testReleaseSHA,
		StartedAt: start.Format(time.RFC3339), EndedAt: start.Add(soak.MinimumDuration).Format(time.RFC3339),
		Profile: soak.Profile{
			Priority0SourcesActive: true, RealReadOnlyAPIsActive: true,
			LowCapOpenAIProject: true, StagingOnlyDelivery: true,
		},
		Observations: observations,
		Exercises: []soak.Exercise{
			exercise(soak.ExerciseProviderOutage, 1, "github"),
			exercise(soak.ExerciseProviderOutage, 3, "openai"),
			exercise(soak.ExerciseRedeployDuringBacklog, 5, ""),
			exercise(soak.ExerciseRedeployDigestWindow, 7, ""),
			exercise(soak.ExerciseMissedMorningCatchup, 9, ""),
			exercise(soak.ExerciseDatabaseRestore, 11, ""),
		},
		OwnerReview: soak.OwnerReview{ReviewedTopItems: 10, UsefulOrAlreadyKnown: 8},
		Cost:        soak.Cost{ActualCents: 500, BudgetCents: 1500, HardCapCents: 1500},
	}
}

func writeJSONEvidence(t *testing.T, root string, relative string, value any) EvidenceReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", relative, err)
	}
	return writeEvidence(t, root, relative, raw)
}

func writeEvidence(t *testing.T, root string, relative string, raw []byte) EvidenceReference {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create evidence directory: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	digest := sha256.Sum256(raw)
	return EvidenceReference{Path: relative, SHA256: fmt.Sprintf("%x", digest)}
}
