package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type TagReference struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}

type TagObject struct {
	Tag    string `json:"tag"`
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
	Verification struct {
		Verified bool   `json:"verified"`
		Reason   string `json:"reason"`
	} `json:"verification"`
}

type TagReport struct {
	Tag       string `json:"tag"`
	CommitSHA string `json:"commitSha"`
	Verified  bool   `json:"verified"`
}

func ValidateSignedTag(
	referenceReader io.Reader,
	tagReader io.Reader,
	expectedTag string,
	expectedCommit string,
) (TagReport, error) {
	if !stableTag.MatchString(expectedTag) || !fullGitSHA.MatchString(expectedCommit) {
		return TagReport{}, errors.New("signed tag validation requires a stable tag and full commit SHA")
	}
	var reference TagReference
	if err := decodeAPIJSON(referenceReader, &reference); err != nil {
		return TagReport{}, fmt.Errorf("decode tag reference: %w", err)
	}
	if reference.Ref != "refs/tags/"+expectedTag || reference.Object.Type != "tag" ||
		!fullGitSHA.MatchString(reference.Object.SHA) {
		return TagReport{}, errors.New("release ref must resolve to an annotated tag object")
	}
	var tag TagObject
	if err := decodeAPIJSON(tagReader, &tag); err != nil {
		return TagReport{}, fmt.Errorf("decode annotated tag: %w", err)
	}
	if tag.Tag != expectedTag || tag.Object.Type != "commit" || tag.Object.SHA != expectedCommit ||
		!tag.Verification.Verified || tag.Verification.Reason != "valid" {
		return TagReport{}, errors.New("annotated release tag is not a valid verified signature over the exact commit")
	}
	return TagReport{Tag: expectedTag, CommitSHA: expectedCommit, Verified: true}, nil
}

func decodeAPIJSON(reader io.Reader, destination any) error {
	raw, err := io.ReadAll(io.LimitReader(reader, MaximumManifestBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > MaximumManifestBytes {
		return errors.New("GitHub response is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}
