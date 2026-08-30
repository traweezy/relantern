package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/soak"
)

const (
	MaximumManifestBytes = 1 << 20
	MaximumEvidenceBytes = 64 << 20
)

var (
	fullGitSHA    = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	stableTag     = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)
)

type EvidenceReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version            int               `json:"version"`
	Tag                string            `json:"tag"`
	ApprovedReleaseSHA string            `json:"approvedReleaseSha"`
	ApprovedTreeSHA    string            `json:"approvedTreeSha"`
	SourceBranch       string            `json:"sourceBranch"`
	Decision           string            `json:"decision"`
	DecidedAt          string            `json:"decidedAt"`
	SoakLedger         EvidenceReference `json:"soakLedger"`
	RestoreDrill       EvidenceReference `json:"restoreDrill"`
	SecurityGate       EvidenceReference `json:"securityGate"`
	PrepushGate        EvidenceReference `json:"prepushGate"`
}

type GateReport struct {
	Version     int                 `json:"version"`
	Kind        string              `json:"kind"`
	ReleaseSHA  string              `json:"releaseSha"`
	Result      string              `json:"result"`
	CompletedAt string              `json:"completedAt"`
	Commands    []string            `json:"commands"`
	Evidence    []EvidenceReference `json:"evidence"`
}

type RestoreReport struct {
	AuditCount       int    `json:"auditCount"`
	BackupSHA256     string `json:"backupSha256"`
	DigestCount      int    `json:"digestCount"`
	GitSHA           string `json:"gitSha"`
	MigrationVersion int64  `json:"migrationVersion"`
	Result           string `json:"result"`
	RPOSeconds       int64  `json:"rpoSeconds"`
	RPOTargetSeconds int64  `json:"rpoTargetSeconds"`
	RTOSeconds       int64  `json:"rtoSeconds"`
	RTOTargetSeconds int64  `json:"rtoTargetSeconds"`
	ScheduleCount    int    `json:"scheduleCount"`
	SourceCount      int    `json:"sourceCount"`
	UserCount        int    `json:"userCount"`
	VerifiedAt       string `json:"verifiedAt"`
}

type EvidenceReport struct {
	Tag                string `json:"tag"`
	ApprovedReleaseSHA string `json:"approvedReleaseSha"`
	ApprovedTreeSHA    string `json:"approvedTreeSha"`
	SoakOutcome        string `json:"soakOutcome"`
	RestoreResult      string `json:"restoreResult"`
	SecurityResult     string `json:"securityResult"`
	PrepushResult      string `json:"prepushResult"`
}

func DecodeManifest(reader io.Reader) (Manifest, error) {
	var manifest Manifest
	if err := decodeStrict(reader, MaximumManifestBytes, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func LoadManifest(path string) (Manifest, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("stat release manifest: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, errors.New("release manifest must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open release manifest: %w", err)
	}
	defer file.Close()
	return DecodeManifest(file)
}

func ValidateManifestPath(manifest Manifest, manifestPath string) error {
	expected := filepath.ToSlash(filepath.Join("docs", "evidence", "releases", manifest.Tag+".json"))
	if filepath.ToSlash(filepath.Clean(manifestPath)) != expected {
		return fmt.Errorf("release manifest path must be %s", expected)
	}
	return nil
}

func ValidateManifest(manifest Manifest, now time.Time) error {
	_, err := validateManifest(manifest, now)
	return err
}

func ValidateEvidence(manifest Manifest, repositoryRoot string, now time.Time) (EvidenceReport, error) {
	decidedAt, err := validateManifest(manifest, now)
	if err != nil {
		return EvidenceReport{}, err
	}

	references := []struct {
		name      string
		reference EvidenceReference
	}{
		{name: "soakLedger", reference: manifest.SoakLedger},
		{name: "restoreDrill", reference: manifest.RestoreDrill},
		{name: "securityGate", reference: manifest.SecurityGate},
		{name: "prepushGate", reference: manifest.PrepushGate},
	}
	paths := make([]string, 0, len(references))
	for _, current := range references {
		if err := validateReference(current.name, current.reference); err != nil {
			return EvidenceReport{}, err
		}
		if slices.Contains(paths, current.reference.Path) {
			return EvidenceReport{}, fmt.Errorf("release evidence path %q is duplicated", current.reference.Path)
		}
		paths = append(paths, current.reference.Path)
	}

	soakPath, err := verifyEvidenceFile(repositoryRoot, "soakLedger", manifest.SoakLedger)
	if err != nil {
		return EvidenceReport{}, err
	}
	soakFile, err := os.Open(soakPath)
	if err != nil {
		return EvidenceReport{}, fmt.Errorf("open soak ledger: %w", err)
	}
	ledger, decodeErr := soak.Decode(soakFile)
	closeErr := soakFile.Close()
	if decodeErr != nil {
		return EvidenceReport{}, decodeErr
	}
	if closeErr != nil {
		return EvidenceReport{}, fmt.Errorf("close soak ledger: %w", closeErr)
	}
	soakReport, err := soak.Evaluate(ledger, now.UTC())
	if err != nil {
		return EvidenceReport{}, fmt.Errorf("validate soak ledger: %w", err)
	}
	if err := soak.VerifyLocalArtifacts(ledger, repositoryRoot); err != nil {
		return EvidenceReport{}, fmt.Errorf("verify soak evidence: %w", err)
	}
	if soakReport.Outcome != soak.OutcomePass {
		return EvidenceReport{}, fmt.Errorf("staging soak outcome is %s, want pass", soakReport.Outcome)
	}
	if ledger.ReleaseSHA != manifest.ApprovedReleaseSHA {
		return EvidenceReport{}, errors.New("staging soak release SHA does not match the approved release")
	}
	endedAt, err := parseTime("soak endedAt", ledger.EndedAt)
	if err != nil || !decidedAt.After(endedAt) {
		return EvidenceReport{}, errors.New("release decision must follow the completed staging soak")
	}

	restorePath, err := verifyEvidenceFile(repositoryRoot, "restoreDrill", manifest.RestoreDrill)
	if err != nil {
		return EvidenceReport{}, err
	}
	var restore RestoreReport
	if err := decodeStrictFile(restorePath, &restore); err != nil {
		return EvidenceReport{}, fmt.Errorf("decode restore drill: %w", err)
	}
	if err := validateRestoreReport(restore, manifest.ApprovedReleaseSHA, decidedAt); err != nil {
		return EvidenceReport{}, err
	}

	security, err := loadGateReport(repositoryRoot, "security", manifest.SecurityGate, manifest.ApprovedReleaseSHA, decidedAt)
	if err != nil {
		return EvidenceReport{}, err
	}
	prepush, err := loadGateReport(repositoryRoot, "prepush", manifest.PrepushGate, manifest.ApprovedReleaseSHA, decidedAt)
	if err != nil {
		return EvidenceReport{}, err
	}

	return EvidenceReport{
		Tag: manifest.Tag, ApprovedReleaseSHA: manifest.ApprovedReleaseSHA,
		ApprovedTreeSHA: manifest.ApprovedTreeSHA, SoakOutcome: string(soakReport.Outcome),
		RestoreResult: restore.Result, SecurityResult: security.Result, PrepushResult: prepush.Result,
	}, nil
}

func validateManifest(manifest Manifest, now time.Time) (time.Time, error) {
	var problems []string
	if manifest.Version != 1 {
		problems = append(problems, "version must be 1")
	}
	if !stableTag.MatchString(manifest.Tag) {
		problems = append(problems, "tag must be a stable vMAJOR.MINOR.PATCH release")
	}
	if !fullGitSHA.MatchString(manifest.ApprovedReleaseSHA) {
		problems = append(problems, "approvedReleaseSha must be a full lowercase Git SHA")
	}
	if !fullGitSHA.MatchString(manifest.ApprovedTreeSHA) {
		problems = append(problems, "approvedTreeSha must be a full lowercase Git tree SHA")
	}
	if manifest.SourceBranch != "staging" {
		problems = append(problems, "sourceBranch must be staging")
	}
	if manifest.Decision != "approved" {
		problems = append(problems, "decision must be approved")
	}
	decidedAt, err := parseTime("decidedAt", manifest.DecidedAt)
	if err != nil {
		problems = append(problems, err.Error())
	} else if decidedAt.After(now.UTC()) {
		problems = append(problems, "decidedAt must not be in the future")
	}
	if len(problems) > 0 {
		return time.Time{}, fmt.Errorf("invalid release manifest: %s", strings.Join(problems, "; "))
	}
	return decidedAt, nil
}

func validateRestoreReport(report RestoreReport, releaseSHA string, decidedAt time.Time) error {
	verifiedAt, err := parseTime("restore verifiedAt", report.VerifiedAt)
	if err != nil {
		return err
	}
	if report.Result != "pass" || report.GitSHA != releaseSHA || !sha256Pattern.MatchString(report.BackupSHA256) {
		return errors.New("restore drill must pass for the approved release with a valid backup hash")
	}
	if report.MigrationVersion < 1 || report.RPOTargetSeconds < 1 || report.RTOTargetSeconds < 1 ||
		report.RPOSeconds < 0 || report.RTOSeconds < 0 || report.RPOSeconds > report.RPOTargetSeconds ||
		report.RTOSeconds > report.RTOTargetSeconds {
		return errors.New("restore drill has invalid migration or RPO/RTO results")
	}
	if report.AuditCount < 0 || report.DigestCount < 0 || report.ScheduleCount < 0 ||
		report.SourceCount < 0 || report.UserCount < 0 {
		return errors.New("restore drill verification counts must be non-negative")
	}
	if !decidedAt.After(verifiedAt) {
		return errors.New("restore drill must complete before the release decision")
	}
	return nil
}

func loadGateReport(
	repositoryRoot string,
	kind string,
	reference EvidenceReference,
	releaseSHA string,
	decidedAt time.Time,
) (GateReport, error) {
	path, err := verifyEvidenceFile(repositoryRoot, kind+"Gate", reference)
	if err != nil {
		return GateReport{}, err
	}
	var report GateReport
	if err := decodeStrictFile(path, &report); err != nil {
		return GateReport{}, fmt.Errorf("decode %s gate: %w", kind, err)
	}
	completedAt, err := parseTime(kind+" completedAt", report.CompletedAt)
	if err != nil {
		return GateReport{}, err
	}
	if report.Version != 1 || report.Kind != kind || report.ReleaseSHA != releaseSHA || report.Result != "pass" {
		return GateReport{}, fmt.Errorf("%s gate must be a version 1 pass for the approved release", kind)
	}
	if !decidedAt.After(completedAt) {
		return GateReport{}, fmt.Errorf("%s gate must complete before the release decision", kind)
	}
	requiredCommands := map[string][]string{
		"security": {"make security-scan", "gitleaks git --redact --no-banner", "pnpm audit --audit-level high", "pnpm audit signatures"},
		"prepush":  {"make prepush", "make prodlike-smoke"},
	}[kind]
	for _, command := range requiredCommands {
		if !slices.Contains(report.Commands, command) {
			return GateReport{}, fmt.Errorf("%s gate is missing command %q", kind, command)
		}
	}
	if len(report.Evidence) == 0 {
		return GateReport{}, fmt.Errorf("%s gate requires hashed evidence", kind)
	}
	for index, evidence := range report.Evidence {
		name := fmt.Sprintf("%sGate.evidence[%d]", kind, index)
		if err := validateReference(name, evidence); err != nil {
			return GateReport{}, err
		}
		if _, err := verifyEvidenceFile(repositoryRoot, name, evidence); err != nil {
			return GateReport{}, err
		}
	}
	return report, nil
}

func validateReference(name string, reference EvidenceReference) error {
	clean := filepath.ToSlash(filepath.Clean(reference.Path))
	if clean != reference.Path || !strings.HasPrefix(clean, "docs/evidence/") ||
		strings.ContainsAny(reference.Path, "\x00\r\n") || !sha256Pattern.MatchString(reference.SHA256) {
		return fmt.Errorf("%s must use a clean repository evidence path and lowercase SHA-256", name)
	}
	return nil
}

func verifyEvidenceFile(repositoryRoot string, name string, reference EvidenceReference) (string, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	candidate := filepath.Join(root, filepath.FromSlash(reference.Path))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s resolves outside the repository", name)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaximumEvidenceBytes {
		return "", fmt.Errorf("%s must be a regular file no larger than %d bytes", name, MaximumEvidenceBytes)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", name, err)
	}
	hasher := sha256.New()
	copied, copyErr := io.Copy(hasher, io.LimitReader(file, MaximumEvidenceBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("hash %s: %w", name, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close %s: %w", name, closeErr)
	}
	if copied > MaximumEvidenceBytes || fmt.Sprintf("%x", hasher.Sum(nil)) != reference.SHA256 {
		return "", fmt.Errorf("%s SHA-256 mismatch", name)
	}
	return resolved, nil
}

func decodeStrictFile(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return decodeStrict(file, MaximumEvidenceBytes, destination)
}

func decodeStrict(reader io.Reader, maximum int64, destination any) error {
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > maximum {
		return fmt.Errorf("JSON exceeds %d bytes", maximum)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func parseTime(name string, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Format(time.RFC3339) != value {
		return time.Time{}, fmt.Errorf("%s must be canonical RFC3339", name)
	}
	return parsed.UTC(), nil
}

func validHTTPSReference(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}
