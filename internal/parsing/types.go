package parsing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/traweezy/relantern/internal/sources"
	"github.com/traweezy/relantern/internal/storage"
)

const ParserVersion = "1.0.0"

type ErrorCode string

const (
	ErrorBodyTooLarge      ErrorCode = "parser_body_too_large"
	ErrorCanceled          ErrorCode = "parser_canceled"
	ErrorCharacterEncoding ErrorCode = "parser_character_encoding"
	ErrorEmptyDocument     ErrorCode = "parser_empty_document"
	ErrorInvalidDocument   ErrorCode = "parser_invalid_document"
	ErrorInvalidURL        ErrorCode = "parser_invalid_url"
	ErrorObjectStorage     ErrorCode = "parser_object_storage"
	ErrorUnsupported       ErrorCode = "parser_unsupported_connector"
)

type ParseError struct {
	Code ErrorCode
	Err  error
}

func (parseError *ParseError) Error() string {
	return parseError.Err.Error()
}

func (parseError *ParseError) Unwrap() error {
	return parseError.Err
}

type Request struct {
	Connector   sources.Connector
	URL         string
	ContentType string
	Body        io.Reader
	MaxBytes    int64
}

type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Anchor string `json:"anchor,omitempty"`
}

type OutlineBlock struct {
	Kind            string `json:"kind"`
	Level           int    `json:"level,omitempty"`
	Anchor          string `json:"anchor"`
	NormalizedStart int    `json:"normalized_start"`
	NormalizedEnd   int    `json:"normalized_end"`
}

type OffsetRange struct {
	NormalizedStart int  `json:"normalized_start"`
	NormalizedEnd   int  `json:"normalized_end"`
	SourceStart     int  `json:"source_start"`
	SourceEnd       int  `json:"source_end"`
	Approximate     bool `json:"approximate"`
}

type Result struct {
	ParserName        string
	ParserVersion     string
	Title             string
	Author            string
	Language          string
	CanonicalURL      string
	SourcePublishedAt time.Time
	SourceUpdatedAt   time.Time
	NormalizedText    string
	NormalizedSHA256  [sha256.Size]byte
	Outline           []OutlineBlock
	OffsetMap         []OffsetRange
	Warnings          []Warning
}

type RecordRequest struct {
	RawDocumentID string
	ObjectKey     string
	Result        Result
	AttemptedAt   time.Time
	CompletedAt   time.Time
}

type FailureRequest struct {
	RawDocumentID string
	ParserName    string
	ErrorCode     ErrorCode
	Warnings      []Warning
	AttemptedAt   time.Time
	CompletedAt   time.Time
}

type RecordResult struct {
	RevisionID         string
	PreviousRevisionID string
	Outcome            string
	MaterialChange     bool
	ChangeReason       string
}

type RevisionRepository interface {
	RecordSuccess(context.Context, RecordRequest) (RecordResult, error)
	RecordFailure(context.Context, FailureRequest) error
}

type ObjectStore interface {
	Stage(context.Context, string, io.Reader) (storage.StagedObject, error)
	Commit(context.Context, storage.StagedObject, string) error
	Abort(context.Context, storage.StagedObject) error
}

func parserError(code ErrorCode, format string, arguments ...any) error {
	return &ParseError{Code: code, Err: fmt.Errorf(format, arguments...)}
}

type extractedBlock struct {
	Kind   string
	Level  int
	Anchor string
	Text   string
}

type extractedDocument struct {
	ParserName        string
	Title             string
	Author            string
	Language          string
	CanonicalURL      string
	SourcePublishedAt time.Time
	SourceUpdatedAt   time.Time
	Blocks            []extractedBlock
	Warnings          []Warning
}
