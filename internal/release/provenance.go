package release

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type ProvenanceSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type Provenance struct {
	Type          string              `json:"_type"`
	Subject       []ProvenanceSubject `json:"subject"`
	PredicateType string              `json:"predicateType"`
	Predicate     struct {
		BuildDefinition struct {
			BuildType          string `json:"buildType"`
			ExternalParameters struct {
				Tag                string `json:"tag"`
				ApprovedReleaseSHA string `json:"approvedReleaseSha"`
				ApprovedTreeSHA    string `json:"approvedTreeSha"`
				SourceBranch       string `json:"sourceBranch"`
			} `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI    string            `json:"uri"`
				Digest map[string]string `json:"digest"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
			Metadata struct {
				InvocationID string `json:"invocationId"`
				FinishedOn   string `json:"finishedOn"`
			} `json:"metadata"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func NewProvenance(
	manifest Manifest,
	repository string,
	builderID string,
	invocationID string,
	archiveName string,
	archiveSHA256 string,
	sbomName string,
	sbomSHA256 string,
	generatedAt time.Time,
) (Provenance, error) {
	if _, err := validateManifest(manifest, farFuture()); err != nil {
		return Provenance{}, err
	}
	if !repositoryName.MatchString(repository) || invalidProvenanceText(builderID) ||
		invalidProvenanceText(invocationID) || invalidProvenanceText(archiveName) ||
		invalidProvenanceText(sbomName) || !sha256Pattern.MatchString(archiveSHA256) ||
		!sha256Pattern.MatchString(sbomSHA256) || generatedAt.IsZero() {
		return Provenance{}, errors.New("provenance inputs are incomplete or invalid")
	}

	statement := Provenance{
		Type: "https://in-toto.io/Statement/v1",
		Subject: []ProvenanceSubject{
			{Name: archiveName, Digest: map[string]string{"sha256": archiveSHA256}},
			{Name: sbomName, Digest: map[string]string{"sha256": sbomSHA256}},
		},
		PredicateType: "https://slsa.dev/provenance/v1",
	}
	statement.Predicate.BuildDefinition.BuildType = "https://github.com/traweezy/relantern/release-archive/v1"
	statement.Predicate.BuildDefinition.ExternalParameters.Tag = manifest.Tag
	statement.Predicate.BuildDefinition.ExternalParameters.ApprovedReleaseSHA = manifest.ApprovedReleaseSHA
	statement.Predicate.BuildDefinition.ExternalParameters.ApprovedTreeSHA = manifest.ApprovedTreeSHA
	statement.Predicate.BuildDefinition.ExternalParameters.SourceBranch = manifest.SourceBranch
	statement.Predicate.BuildDefinition.ResolvedDependencies = []struct {
		URI    string            `json:"uri"`
		Digest map[string]string `json:"digest"`
	}{
		{
			URI:    fmt.Sprintf("git+https://github.com/%s@%s", repository, manifest.ApprovedReleaseSHA),
			Digest: map[string]string{"gitTree": manifest.ApprovedTreeSHA},
		},
	}
	statement.Predicate.RunDetails.Builder.ID = builderID
	statement.Predicate.RunDetails.Metadata.InvocationID = invocationID
	statement.Predicate.RunDetails.Metadata.FinishedOn = generatedAt.UTC().Format(time.RFC3339)
	return statement, nil
}

func invalidProvenanceText(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed != value || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n")
}
