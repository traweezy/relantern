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

var rawObjectKeyPattern = regexp.MustCompile(
	`^raw/[a-z0-9]+(?:-[a-z0-9]+)*/([0-9]{4}/[0-9]{2}/[0-9]{2})/[a-f0-9]{64}\.(?:bin|html|json|txt|xml)$`,
)

var normalizedObjectKeyPattern = regexp.MustCompile(
	`^normalized/[a-z0-9]+(?:-[a-z0-9]+)*/[a-f0-9]{64}\.txt$`,
)

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

type ObjectReader interface {
	Read(context.Context, string, int64) ([]byte, error)
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

func NormalizedObjectKey(sourceID string, digest [sha256.Size]byte) (string, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return "", fmt.Errorf("invalid source id %q", sourceID)
	}
	return fmt.Sprintf("normalized/%s/%s.txt", sourceID, hex.EncodeToString(digest[:])), nil
}

func ValidateObjectKey(objectKey string) error {
	if normalizedObjectKeyPattern.MatchString(objectKey) {
		return nil
	}
	matches := rawObjectKeyPattern.FindStringSubmatch(objectKey)
	if len(matches) == 2 {
		if _, err := time.Parse("2006/01/02", matches[1]); err == nil {
			return nil
		}
	}
	return fmt.Errorf("object key %q is outside approved content-addressed namespaces", objectKey)
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
