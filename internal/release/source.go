package release

import (
	"errors"
	"strings"
)

type SourceInput struct {
	EventName      string
	BaseRef        string
	HeadRef        string
	BaseRepository string
	HeadRepository string
	Draft          bool
}

func ValidateSource(input SourceInput) error {
	if input.EventName != "pull_request" {
		return errors.New("release source validation requires a pull_request event")
	}
	if input.BaseRef != "master" || input.HeadRef != "staging" {
		return errors.New("release pull request must originate from staging and target master")
	}
	if strings.TrimSpace(input.BaseRepository) == "" || input.BaseRepository != input.HeadRepository {
		return errors.New("release pull request must originate from the same repository")
	}
	if input.Draft {
		return errors.New("draft release pull requests cannot be promoted")
	}
	return nil
}
