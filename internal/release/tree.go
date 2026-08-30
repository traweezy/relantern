package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TreeReport struct {
	ApprovedCommit  string   `json:"approvedCommit"`
	ApprovedTree    string   `json:"approvedTree"`
	StagingCommit   string   `json:"stagingCommit"`
	CandidateCommit string   `json:"candidateCommit,omitempty"`
	MetadataAdded   []string `json:"metadataAdded"`
}

func ValidateTree(
	ctx context.Context,
	repositoryRoot string,
	manifest Manifest,
	stagingCommit string,
	candidateCommit string,
) (TreeReport, error) {
	if _, err := validateManifest(manifest, farFuture()); err != nil {
		return TreeReport{}, err
	}
	if !fullGitSHA.MatchString(stagingCommit) || candidateCommit != "" && !fullGitSHA.MatchString(candidateCommit) {
		return TreeReport{}, errors.New("tree validation requires full lowercase Git SHAs")
	}
	approvedTree, err := gitOutput(ctx, repositoryRoot, "rev-parse", manifest.ApprovedReleaseSHA+"^{tree}")
	if err != nil {
		return TreeReport{}, fmt.Errorf("resolve approved release tree: %w", err)
	}
	if approvedTree != manifest.ApprovedTreeSHA {
		return TreeReport{}, errors.New("approved release tree does not match the manifest")
	}
	if err := gitRun(ctx, repositoryRoot, "merge-base", "--is-ancestor", manifest.ApprovedReleaseSHA, stagingCommit); err != nil {
		return TreeReport{}, errors.New("approved release must be an ancestor of the staging head")
	}

	raw, err := gitOutputBytes(
		ctx,
		repositoryRoot,
		"diff",
		"--name-status",
		"--no-renames",
		"-z",
		manifest.ApprovedReleaseSHA,
		stagingCommit,
		"--",
		".",
	)
	if err != nil {
		return TreeReport{}, fmt.Errorf("compare approved and staging trees: %w", err)
	}
	metadata, err := validateMetadataDiff(raw)
	if err != nil {
		return TreeReport{}, err
	}
	if err := validateMetadataModes(ctx, repositoryRoot, stagingCommit, metadata); err != nil {
		return TreeReport{}, err
	}

	if candidateCommit != "" {
		stagingTree, treeErr := gitOutput(ctx, repositoryRoot, "rev-parse", stagingCommit+"^{tree}")
		if treeErr != nil {
			return TreeReport{}, fmt.Errorf("resolve staging tree: %w", treeErr)
		}
		candidateTree, treeErr := gitOutput(ctx, repositoryRoot, "rev-parse", candidateCommit+"^{tree}")
		if treeErr != nil {
			return TreeReport{}, fmt.Errorf("resolve promotion candidate tree: %w", treeErr)
		}
		if stagingTree != candidateTree {
			return TreeReport{}, errors.New("promotion candidate tree differs from the staging head")
		}
	}

	return TreeReport{
		ApprovedCommit:  manifest.ApprovedReleaseSHA,
		ApprovedTree:    manifest.ApprovedTreeSHA,
		StagingCommit:   stagingCommit,
		CandidateCommit: candidateCommit,
		MetadataAdded:   metadata,
	}, nil
}

func validateMetadataModes(ctx context.Context, root string, commit string, paths []string) error {
	for _, path := range paths {
		raw, err := gitOutputBytes(ctx, root, "ls-tree", "-z", commit, "--", path)
		if err != nil {
			return fmt.Errorf("inspect release metadata %q: %w", path, err)
		}
		if bytes.Count(raw, []byte{0}) != 1 || !bytes.HasSuffix(raw, []byte{0}) ||
			!bytes.HasPrefix(raw, []byte("100644 blob ")) {
			return fmt.Errorf("release metadata %q must be a regular non-executable Git blob", path)
		}
	}
	return nil
}

func validateMetadataDiff(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	parts := bytes.Split(raw, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts)%2 != 0 {
		return nil, errors.New("unexpected Git name-status output")
	}
	paths := make([]string, 0, len(parts)/2)
	for index := 0; index < len(parts); index += 2 {
		status := string(parts[index])
		path := filepath.ToSlash(string(parts[index+1]))
		if status != "A" || !strings.HasPrefix(path, "docs/evidence/") || path == "docs/evidence/" {
			return nil, fmt.Errorf("release tree changed non-permitted path %q with status %q", path, status)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func gitOutput(ctx context.Context, root string, arguments ...string) (string, error) {
	raw, err := gitOutputBytes(ctx, root, arguments...)
	return strings.TrimSpace(string(raw)), err
}

func gitOutputBytes(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", root}, arguments...)
	command := exec.CommandContext(ctx, "git", commandArguments...)
	raw, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s: %w", strings.Join(arguments, " "), strings.TrimSpace(string(raw)), err)
	}
	return raw, nil
}

func gitRun(ctx context.Context, root string, arguments ...string) error {
	_, err := gitOutputBytes(ctx, root, arguments...)
	return err
}

func farFuture() time.Time {
	return time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
}
