package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTreeAllowsOnlyAddedEvidence(t *testing.T) {
	repository := newGitRepository(t)
	writeTestFile(t, repository, "app.txt", "immutable application\n")
	approved := commitAll(t, repository, "approved")
	tree := gitTestOutput(t, repository, "rev-parse", approved+"^{tree}")

	writeTestFile(t, repository, "docs/evidence/releases/v1.0.0.json", "{}\n")
	staging := commitAll(t, repository, "release evidence")
	candidate := emptyCommit(t, repository, "promotion candidate")
	manifest := validManifest(approved, tree)

	report, err := ValidateTree(context.Background(), repository, manifest, staging, candidate)
	if err != nil {
		t.Fatalf("ValidateTree() error = %v", err)
	}
	if len(report.MetadataAdded) != 1 || report.MetadataAdded[0] != "docs/evidence/releases/v1.0.0.json" {
		t.Fatalf("ValidateTree() report = %+v", report)
	}
}

func TestValidateTreeRejectsCodeAndEvidenceRewrites(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		path    string
		update  string
	}{
		{name: "code change", path: "app.txt", initial: "approved\n", update: "changed\n"},
		{name: "evidence rewrite", path: "docs/evidence/existing.json", initial: "approved\n", update: "changed\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newGitRepository(t)
			writeTestFile(t, repository, test.path, test.initial)
			approved := commitAll(t, repository, "approved")
			tree := gitTestOutput(t, repository, "rev-parse", approved+"^{tree}")
			writeTestFile(t, repository, test.path, test.update)
			staging := commitAll(t, repository, "invalid rewrite")
			_, err := ValidateTree(context.Background(), repository, validManifest(approved, tree), staging, "")
			if err == nil || !strings.Contains(err.Error(), "non-permitted path") {
				t.Fatalf("ValidateTree() error = %v", err)
			}
		})
	}
}

func TestValidateTreeRejectsNonRegularEvidence(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*testing.T, string)
	}{
		{
			name: "symlink",
			write: func(t *testing.T, repository string) {
				path := filepath.Join(repository, "docs", "evidence", "release.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../../app.txt", path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "executable",
			write: func(t *testing.T, repository string) {
				writeTestFile(t, repository, "docs/evidence/release.json", "{}\n")
				if err := os.Chmod(filepath.Join(repository, "docs", "evidence", "release.json"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newGitRepository(t)
			writeTestFile(t, repository, "app.txt", "approved\n")
			approved := commitAll(t, repository, "approved")
			tree := gitTestOutput(t, repository, "rev-parse", approved+"^{tree}")
			test.write(t, repository)
			staging := commitAll(t, repository, "invalid evidence")
			if _, err := ValidateTree(context.Background(), repository, validManifest(approved, tree), staging, ""); err == nil ||
				!strings.Contains(err.Error(), "regular non-executable") {
				t.Fatalf("ValidateTree() error = %v", err)
			}
		})
	}
}

func TestValidateTreeRejectsMismatchedCandidateAndTreeIdentity(t *testing.T) {
	repository := newGitRepository(t)
	writeTestFile(t, repository, "app.txt", "approved\n")
	approved := commitAll(t, repository, "approved")
	tree := gitTestOutput(t, repository, "rev-parse", approved+"^{tree}")
	writeTestFile(t, repository, "docs/evidence/releases/v1.0.0.json", "{}\n")
	staging := commitAll(t, repository, "evidence")
	writeTestFile(t, repository, "candidate-only.txt", "unexpected\n")
	candidate := commitAll(t, repository, "candidate")

	manifest := validManifest(approved, tree)
	if _, err := ValidateTree(context.Background(), repository, manifest, staging, candidate); err == nil ||
		!strings.Contains(err.Error(), "differs") {
		t.Fatalf("ValidateTree() error = %v, want candidate mismatch", err)
	}
	manifest.ApprovedTreeSHA = strings.Repeat("f", 40)
	if _, err := ValidateTree(context.Background(), repository, manifest, staging, ""); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("ValidateTree() error = %v, want approved tree mismatch", err)
	}
}

func TestValidateTreeRejectsInvalidAndUnrelatedCommits(t *testing.T) {
	repository := newGitRepository(t)
	writeTestFile(t, repository, "app.txt", "approved\n")
	approved := commitAll(t, repository, "approved")
	tree := gitTestOutput(t, repository, "rev-parse", approved+"^{tree}")
	if _, err := ValidateTree(context.Background(), repository, validManifest(approved, tree), "short", ""); err == nil {
		t.Fatal("ValidateTree() accepted a short SHA")
	}

	gitTestRun(t, repository, "checkout", "--quiet", "--orphan", "unrelated")
	gitTestRun(t, repository, "rm", "--quiet", "-rf", ".")
	writeTestFile(t, repository, "unrelated.txt", "unrelated\n")
	unrelated := commitAll(t, repository, "unrelated")
	if _, err := ValidateTree(context.Background(), repository, validManifest(approved, tree), unrelated, ""); err == nil ||
		!strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("ValidateTree() error = %v", err)
	}
}

func validManifest(commit string, tree string) Manifest {
	return Manifest{
		Version: 1, Tag: "v1.0.0", ApprovedReleaseSHA: commit, ApprovedTreeSHA: tree,
		SourceBranch: "staging", Decision: "approved", DecidedAt: "2026-08-30T00:00:00Z",
	}
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	gitTestRun(t, repository, "init", "--quiet")
	gitTestRun(t, repository, "config", "user.name", "Release Test")
	gitTestRun(t, repository, "config", "user.email", "release@example.test")
	return repository
}

func writeTestFile(t *testing.T, root string, relative string, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func commitAll(t *testing.T, repository string, message string) string {
	t.Helper()
	gitTestRun(t, repository, "add", "-A")
	gitTestRun(t, repository, "commit", "--quiet", "-m", message)
	return gitTestOutput(t, repository, "rev-parse", "HEAD")
}

func emptyCommit(t *testing.T, repository string, message string) string {
	t.Helper()
	gitTestRun(t, repository, "commit", "--quiet", "--allow-empty", "-m", message)
	return gitTestOutput(t, repository, "rev-parse", "HEAD")
}

func gitTestRun(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", arguments, output, err)
	}
}

func gitTestOutput(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", arguments, output, err)
	}
	return strings.TrimSpace(string(output))
}
