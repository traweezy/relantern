package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pullRequest struct {
	Number         int     `json:"number"`
	State          string  `json:"state"`
	MergedAt       *string `json:"merged_at"`
	MergeCommitSHA string  `json:"merge_commit_sha"`
	Base           struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
	Head struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type HotfixEvidence struct {
	Version            int                 `json:"version"`
	PullRequest        int                 `json:"pullRequest"`
	Reason             string              `json:"reason"`
	IncidentReference  string              `json:"incidentReference"`
	BackmergeReference string              `json:"backmergeReference"`
	ApprovedAt         string              `json:"approvedAt"`
	Evidence           []EvidenceReference `json:"evidence"`
}

type IntegrityReport struct {
	CommitSHA   string `json:"commitSha"`
	PullRequest int    `json:"pullRequest"`
	Path        string `json:"path"`
}

func ValidateIntegrity(
	reader io.Reader,
	repositoryRoot string,
	repository string,
	commitSHA string,
	now time.Time,
) (IntegrityReport, error) {
	if !fullGitSHA.MatchString(commitSHA) || strings.TrimSpace(repository) == "" {
		return IntegrityReport{}, errors.New("integrity validation requires a repository and full commit SHA")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaximumManifestBytes+1))
	if err != nil {
		return IntegrityReport{}, fmt.Errorf("read associated pull requests: %w", err)
	}
	if len(raw) > MaximumManifestBytes {
		return IntegrityReport{}, errors.New("associated pull request response is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var pullRequests []pullRequest
	if err := decoder.Decode(&pullRequests); err != nil {
		return IntegrityReport{}, fmt.Errorf("decode associated pull requests: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return IntegrityReport{}, errors.New("decode associated pull requests: trailing JSON value")
	}

	for _, pullRequest := range pullRequests {
		if pullRequest.State != "closed" || pullRequest.MergedAt == nil ||
			pullRequest.MergeCommitSHA != commitSHA || pullRequest.Base.Ref != "master" ||
			pullRequest.Base.Repo.FullName != repository || pullRequest.Head.Repo.FullName != repository {
			continue
		}
		if pullRequest.Head.Ref == "staging" {
			return IntegrityReport{CommitSHA: commitSHA, PullRequest: pullRequest.Number, Path: "release"}, nil
		}
		if strings.HasPrefix(pullRequest.Head.Ref, "hotfix/") && hasLabel(pullRequest, "emergency-hotfix") {
			if err := validateHotfixEvidence(repositoryRoot, pullRequest.Number, now); err != nil {
				return IntegrityReport{}, err
			}
			return IntegrityReport{CommitSHA: commitSHA, PullRequest: pullRequest.Number, Path: "hotfix"}, nil
		}
	}
	return IntegrityReport{}, errors.New("master commit is not associated with an approved staging release or documented hotfix PR")
}

func validateHotfixEvidence(repositoryRoot string, pullRequest int, now time.Time) error {
	relative := filepath.ToSlash(filepath.Join("docs", "evidence", "hotfixes", fmt.Sprintf("pr-%d.json", pullRequest)))
	path := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
	var evidence HotfixEvidence
	if err := decodeStrictFile(path, &evidence); err != nil {
		return fmt.Errorf("decode hotfix evidence %s: %w", relative, err)
	}
	approvedAt, err := parseTime("hotfix approvedAt", evidence.ApprovedAt)
	if err != nil {
		return err
	}
	if evidence.Version != 1 || evidence.PullRequest != pullRequest || len(strings.TrimSpace(evidence.Reason)) < 20 ||
		len(evidence.Reason) > 500 || !validHTTPSReference(evidence.IncidentReference) ||
		!validHTTPSReference(evidence.BackmergeReference) || approvedAt.After(now.UTC()) || len(evidence.Evidence) == 0 {
		return errors.New("hotfix evidence is incomplete or invalid")
	}
	for index, reference := range evidence.Evidence {
		name := fmt.Sprintf("hotfix.evidence[%d]", index)
		if err := validateReference(name, reference); err != nil {
			return err
		}
		if _, err := verifyEvidenceFile(repositoryRoot, name, reference); err != nil {
			return err
		}
	}
	return nil
}

func hasLabel(pullRequest pullRequest, expected string) bool {
	for _, label := range pullRequest.Labels {
		if label.Name == expected {
			return true
		}
	}
	return false
}

func OpenPullRequests(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open associated pull requests: %w", err)
	}
	return file, nil
}
