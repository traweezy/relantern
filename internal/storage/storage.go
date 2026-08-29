package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"regexp"
	"time"
)

var sourceIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type StagedObject struct {
	TemporaryKey string
	SHA256       [sha256.Size]byte
	Bytes        int64
	ContentType  string
}

type RawStore interface {
	Stage(context.Context, string, io.Reader) (StagedObject, error)
	Commit(context.Context, StagedObject, string) error
	Abort(context.Context, StagedObject) error
}

func RawObjectKey(sourceID string, observedAt time.Time, digest [sha256.Size]byte, contentType string) (string, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return "", fmt.Errorf("invalid source id %q", sourceID)
	}
	if observedAt.IsZero() {
		return "", errors.New("observed time is required")
	}
	extension := extensionFor(contentType)
	date := observedAt.UTC()
	return fmt.Sprintf(
		"raw/%s/%04d/%02d/%02d/%s.%s",
		sourceID,
		date.Year(),
		date.Month(),
		date.Day(),
		hex.EncodeToString(digest[:]),
		extension,
	), nil
}

func extensionFor(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "bin"
	}
	switch mediaType {
	case "application/atom+xml", "application/rss+xml", "application/xml", "text/xml":
		return "xml"
	case "application/feed+json", "application/json", "application/ld+json", "application/vnd.github+json":
		return "json"
	case "text/html", "application/xhtml+xml":
		return "html"
	case "text/plain", "text/markdown":
		return "txt"
	default:
		return "bin"
	}
}
