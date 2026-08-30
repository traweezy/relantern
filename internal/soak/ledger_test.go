package soak

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testProjectID  = "3bff9840-26d3-4f71-bca4-30335e154751"
	testReleaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testDigest     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestEvaluatePassesCompleteLedger(t *testing.T) {
	t.Parallel()
	ledger, now := completeLedger()

	report, err := Evaluate(ledger, now)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if report.Outcome != OutcomePass || report.DurationHours != 336 ||
		report.ObservationCount != 15 || report.UsefulnessRate != 0.8 || len(report.Reasons) != 0 {
		t.Fatalf("Evaluate() report = %+v", report)
	}
}

func TestEvaluateDistinguishesIncompleteExtendAndFail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Ledger)
		want   Outcome
		reason string
	}{
		{
			name: "in progress",
			mutate: func(ledger *Ledger) {
				ledger.EndedAt = ""
			},
			want:   OutcomeInProgress,
			reason: "soak has not been concluded",
		},
		{
			name: "short duration",
			mutate: func(ledger *Ledger) {
				ledger.EndedAt = mustTime(ledger.StartedAt).Add(13 * 24 * time.Hour).Format(time.RFC3339)
				ledger.Observations = ledger.Observations[:14]
			},
			want:   OutcomeExtend,
			reason: "minimum 336-hour soak duration has not elapsed",
		},
		{
			name: "observation gap",
			mutate: func(ledger *Ledger) {
				ledger.Observations = append(ledger.Observations[:7], ledger.Observations[8:]...)
			},
			want:   OutcomeExtend,
			reason: "observation continuity has a gap greater than 26 hours",
		},
		{
			name: "unsupported material claim",
			mutate: func(ledger *Ledger) {
				ledger.Observations[4].UnsupportedMaterialClaims = 1
			},
			want:   OutcomeExtend,
			reason: "observation 5 recorded unsupported material claims",
		},
		{
			name: "low usefulness",
			mutate: func(ledger *Ledger) {
				ledger.OwnerReview.UsefulOrAlreadyKnown = 7
			},
			want:   OutcomeExtend,
			reason: "owner usefulness rate is below 80 percent",
		},
		{
			name: "missing provider outage",
			mutate: func(ledger *Ledger) {
				ledger.Exercises = ledger.Exercises[1:]
			},
			want:   OutcomeExtend,
			reason: "at least two provider outage exercises are required",
		},
		{
			name: "fatal finding",
			mutate: func(ledger *Ledger) {
				ledger.FatalFindings = []FatalFinding{{
					Kind:       FatalLostEvidence,
					OccurredAt: mustTime(ledger.StartedAt).Add(48 * time.Hour).Format(time.RFC3339),
					Evidence:   evidence("docs/evidence/staging/lost-evidence.json"),
				}}
			},
			want:   OutcomeFail,
			reason: "fatal staging finding: lost_evidence",
		},
		{
			name: "hard cap exceeded",
			mutate: func(ledger *Ledger) {
				ledger.Cost.ActualCents = 1501
			},
			want:   OutcomeFail,
			reason: "provider cost exceeded the hard cap",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ledger, now := completeLedger()
			test.mutate(&ledger)
			report, err := Evaluate(ledger, now)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if report.Outcome != test.want || !slicesContain(report.Reasons, test.reason) {
				t.Fatalf("Evaluate() report = %+v, want %s containing %q", report, test.want, test.reason)
			}
		})
	}
}

func TestEvaluateRejectsInvalidEvidenceIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Ledger)
		want   string
	}{
		{
			name: "mismatched release",
			mutate: func(ledger *Ledger) {
				ledger.Observations[0].ReleaseSHA = "1123456789abcdef0123456789abcdef01234567"
			},
			want: "does not match the frozen release SHA",
		},
		{
			name: "path traversal",
			mutate: func(ledger *Ledger) {
				ledger.Observations[0].Evidence[0].Reference = "../private.json"
			},
			want: "safe repository-relative path or HTTPS URL",
		},
		{
			name: "invalid digest",
			mutate: func(ledger *Ledger) {
				ledger.Observations[0].Evidence[0].SHA256 = "not-a-digest"
			},
			want: "lowercase SHA-256 digest",
		},
		{
			name: "duplicate singleton exercise",
			mutate: func(ledger *Ledger) {
				duplicate := ledger.Exercises[2]
				duplicate.OccurredAt = mustTime(ledger.Exercises[len(ledger.Exercises)-1].OccurredAt).
					Add(time.Hour).Format(time.RFC3339)
				ledger.Exercises = append(ledger.Exercises, duplicate)
			},
			want: ".kind is duplicated",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ledger, now := completeLedger()
			test.mutate(&ledger)
			_, err := Evaluate(ledger, now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Evaluate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestEvaluateReportsCombinedNonFatalFailures(t *testing.T) {
	t.Parallel()
	ledger, now := completeLedger()
	ledger.Profile = Profile{}
	ledger.Observations[0].FreshnessSLOMet = false
	ledger.Observations[0].DuplicateAlerts = 1
	ledger.Observations[0].QueueHealthy = false
	ledger.Observations[0].Demo = DemoChecks{}
	ledger.Observations[0].Evidence = nil
	ledger.Exercises[0].Passed = false
	ledger.Exercises[0].Evidence = nil
	ledger.OwnerReview.ReviewedTopItems = 9
	ledger.OwnerReview.UsefulOrAlreadyKnown = 9
	ledger.Cost = Cost{}
	ledger.NonCriticalSLOMisses = []string{"one transient freshness miss"}

	report, err := Evaluate(ledger, now)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	for _, wanted := range []string{
		"Priority 0 sources are not confirmed active",
		"real read-only APIs are not confirmed active",
		"low-cap staging OpenAI project is not confirmed",
		"staging-only delivery isolation is not confirmed",
		"observation 1 missed the freshness SLO",
		"observation 1 recorded duplicate alerts",
		"observation 1 did not have a healthy recovered queue",
		"observation 1 did not pass all anonymous demo checks",
		"observation 1 has no durable evidence",
		"exercise 1 did not pass",
		"exercise 1 has no durable evidence",
		"owner review has fewer than 10 top items",
		"cost budget is not configured",
		"provider hard cap is not configured",
		"non-critical SLO miss: one transient freshness miss",
	} {
		if !slicesContain(report.Reasons, wanted) {
			t.Errorf("Evaluate() reasons = %v, want %q", report.Reasons, wanted)
		}
	}
	if report.Outcome != OutcomeExtend {
		t.Fatalf("Evaluate() outcome = %s, want extend", report.Outcome)
	}
}

func TestEvaluateRejectsMalformedLedgerFields(t *testing.T) {
	t.Parallel()
	ledger, now := completeLedger()
	ledger.Version = 2
	ledger.Environment = "production"
	ledger.ProjectID = "not-a-project"
	ledger.ReleaseSHA = "short"
	ledger.StartedAt = "invalid"
	ledger.EndedAt = now.Format(time.RFC3339)
	ledger.Observations[0].ObservedAt = "invalid"
	ledger.Observations[0].ReleaseSHA = "short"
	ledger.Observations[0].DuplicateAlerts = -1
	ledger.Exercises[0].Kind = "unknown"
	ledger.Exercises[0].Provider = ""
	ledger.Exercises[1].Provider = "openai"
	ledger.FatalFindings = []FatalFinding{{Kind: "unknown", OccurredAt: "invalid"}}
	ledger.OwnerReview = OwnerReview{ReviewedTopItems: 1, UsefulOrAlreadyKnown: 2}
	ledger.Cost = Cost{ActualCents: -1, BudgetCents: 20, HardCapCents: 10}
	ledger.NonCriticalSLOMisses = []string{" "}

	_, err := Evaluate(ledger, now)
	if err == nil {
		t.Fatal("Evaluate() accepted a malformed ledger")
	}
	for _, wanted := range []string{
		"version must be 1",
		"environment must be staging",
		"projectId must be a lowercase Railway UUID",
		"releaseSha must be a full lowercase Git SHA",
		"startedAt must be RFC3339",
		"counts must be non-negative",
		"kind is unsupported",
		"ownerReview useful count cannot exceed reviewed count",
		"cost values must be non-negative integer cents",
		"cost budget cannot exceed the hard cap",
		"must not be empty",
	} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("Evaluate() error = %v, want containing %q", err, wanted)
		}
	}
}

func TestEvaluateRequiresChronologyAndCurrentEvidenceWindow(t *testing.T) {
	t.Parallel()
	t.Run("chronology", func(t *testing.T) {
		ledger, now := completeLedger()
		ledger.Observations[1].ObservedAt = ledger.Observations[0].ObservedAt
		if _, err := Evaluate(ledger, now); err == nil || !strings.Contains(err.Error(), "strictly chronological") {
			t.Fatalf("Evaluate() error = %v, want chronology error", err)
		}
	})
	t.Run("future conclusion", func(t *testing.T) {
		ledger, now := completeLedger()
		ledger.EndedAt = now.Add(time.Hour).Format(time.RFC3339)
		report, err := Evaluate(ledger, now)
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		if report.Outcome != OutcomeExtend || !slicesContain(report.Reasons, "soak end time is in the future") {
			t.Fatalf("Evaluate() report = %+v", report)
		}
	})
	t.Run("evidence outside window", func(t *testing.T) {
		ledger, now := completeLedger()
		ledger.Exercises[0].OccurredAt = mustTime(ledger.StartedAt).Add(-time.Hour).Format(time.RFC3339)
		report, err := Evaluate(ledger, now)
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		if report.Outcome != OutcomeExtend || !slicesContain(report.Reasons, "exercise falls outside the soak window") {
			t.Fatalf("Evaluate() report = %+v", report)
		}
	})
}

func TestEvaluateAcceptsHTTPSArtifactReference(t *testing.T) {
	t.Parallel()
	ledger, now := completeLedger()
	ledger.Observations[0].Evidence[0].Reference = "https://example.test/immutable/evidence.json"
	report, err := Evaluate(ledger, now)
	if err != nil || report.Outcome != OutcomePass {
		t.Fatalf("Evaluate() = %+v, %v", report, err)
	}
}

func TestVerifyLocalArtifactsChecksDigestAndRepositoryBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	payload := []byte("redacted staging evidence\n")
	digest := sha256.Sum256(payload)
	reference := "docs/evidence/staging/day-01.json"
	artifactPath := filepath.Join(root, filepath.FromSlash(reference))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}
	if err := os.WriteFile(artifactPath, payload, 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	ledger := Ledger{Observations: []Observation{{Evidence: []Artifact{{
		Reference: reference,
		SHA256:    fmt.Sprintf("%x", digest),
	}}}}}
	if err := VerifyLocalArtifacts(ledger, root); err != nil {
		t.Fatalf("VerifyLocalArtifacts() error = %v", err)
	}

	ledger.Observations[0].Evidence[0].SHA256 = testDigest
	if err := VerifyLocalArtifacts(ledger, root); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("VerifyLocalArtifacts() error = %v, want digest mismatch", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, payload, 0o600); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}
	link := filepath.Join(root, "escape.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create evidence symlink: %v", err)
	}
	ledger.Observations[0].Evidence[0] = Artifact{Reference: "escape.json", SHA256: fmt.Sprintf("%x", digest)}
	if err := VerifyLocalArtifacts(ledger, root); err == nil || !strings.Contains(err.Error(), "outside the evidence root") {
		t.Fatalf("VerifyLocalArtifacts() error = %v, want boundary error", err)
	}
}

func TestVerifyLocalArtifactsAllowsPinnedRemoteEvidence(t *testing.T) {
	t.Parallel()
	ledger := Ledger{Exercises: []Exercise{{Evidence: []Artifact{{
		Reference: "https://example.test/immutable/evidence.json",
		SHA256:    testDigest,
	}}}}}
	if err := VerifyLocalArtifacts(ledger, t.TempDir()); err != nil {
		t.Fatalf("VerifyLocalArtifacts() error = %v", err)
	}
}

func TestDecodeIsStrictAndBounded(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "unknown field", raw: []byte(`{"version":1,"unexpected":true}`), want: "unknown field"},
		{name: "trailing value", raw: []byte(`{"version":1} {}`), want: "trailing JSON value"},
		{name: "oversized", raw: bytes.Repeat([]byte(" "), MaximumLedgerBytes+1), want: "exceeds"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode(bytes.NewReader(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestDecodeRejectsReaderFailure(t *testing.T) {
	t.Parallel()
	_, err := Decode(failingReader{})
	if err == nil || !strings.Contains(err.Error(), "read soak ledger") {
		t.Fatalf("Decode() error = %v", err)
	}
}

func completeLedger() (Ledger, time.Time) {
	start := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	observations := make([]Observation, 0, 15)
	for day := 0; day <= 14; day++ {
		observations = append(observations, Observation{
			ObservedAt:      start.Add(time.Duration(day) * 24 * time.Hour).Format(time.RFC3339),
			ReleaseSHA:      testReleaseSHA,
			FreshnessSLOMet: true,
			QueueHealthy:    true,
			Demo: DemoChecks{
				Uptime: true, Performance: true, Accessibility: true, Isolation: true,
			},
			Evidence: evidence("docs/evidence/staging/daily.json"),
		})
	}
	exercise := func(kind ExerciseKind, day int, provider string) Exercise {
		return Exercise{
			Kind:       kind,
			OccurredAt: start.Add(time.Duration(day)*24*time.Hour + time.Hour).Format(time.RFC3339),
			ReleaseSHA: testReleaseSHA,
			Provider:   provider,
			Passed:     true,
			Evidence:   evidence("docs/evidence/staging/exercise.json"),
		}
	}
	return Ledger{
		Version:     1,
		Environment: "staging",
		ProjectID:   testProjectID,
		ReleaseSHA:  testReleaseSHA,
		StartedAt:   start.Format(time.RFC3339),
		EndedAt:     start.Add(MinimumDuration).Format(time.RFC3339),
		Profile: Profile{
			Priority0SourcesActive: true,
			RealReadOnlyAPIsActive: true,
			LowCapOpenAIProject:    true,
			StagingOnlyDelivery:    true,
		},
		Observations: observations,
		Exercises: []Exercise{
			exercise(ExerciseProviderOutage, 1, "github"),
			exercise(ExerciseProviderOutage, 3, "openai"),
			exercise(ExerciseRedeployDuringBacklog, 5, ""),
			exercise(ExerciseRedeployDigestWindow, 7, ""),
			exercise(ExerciseMissedMorningCatchup, 9, ""),
			exercise(ExerciseDatabaseRestore, 11, ""),
		},
		OwnerReview: OwnerReview{ReviewedTopItems: 10, UsefulOrAlreadyKnown: 8},
		Cost:        Cost{ActualCents: 500, BudgetCents: 1500, HardCapCents: 1500},
	}, start.Add(MinimumDuration)
}

func evidence(reference string) []Artifact {
	return []Artifact{{Reference: reference, SHA256: testDigest}}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func slicesContain(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture read failure")
}
