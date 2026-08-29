package fetcher

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/traweezy/relantern/internal/storage"
)

type Outcome string

const (
	OutcomeStored       Outcome = "stored"
	OutcomeMetadataOnly Outcome = "metadata_only"
	OutcomeNotModified  Outcome = "not_modified"
	OutcomeFailed       Outcome = "failed"
)

type ErrorCode string

const (
	ErrorInvalidURL          ErrorCode = "invalid_url"
	ErrorDestinationDenied   ErrorCode = "destination_denied"
	ErrorDNS                 ErrorCode = "dns_failed"
	ErrorTransport           ErrorCode = "transport_failed"
	ErrorUnexpectedStatus    ErrorCode = "unexpected_status"
	ErrorContentType         ErrorCode = "content_type_denied"
	ErrorUnsupportedEncoding ErrorCode = "unsupported_content_encoding"
	ErrorCompressedTooLarge  ErrorCode = "compressed_body_too_large"
	ErrorBodyTooLarge        ErrorCode = "decompressed_body_too_large"
	ErrorCompressionRatio    ErrorCode = "compression_ratio_exceeded"
	ErrorObjectStorage       ErrorCode = "object_storage_failed"
)

type Endpoint struct {
	ID                   string
	SourceID             string
	URL                  string
	AllowedHosts         []string
	ExpectedContentTypes []string
	MaxBodyBytes         int64
	ContentPolicy        string
}

type Checkpoint struct {
	Cursor        string
	ETag          string
	LastModified  string
	ProviderState map[string]any
}

type Attempt struct {
	AttemptedAt     time.Time
	CompletedAt     time.Time
	StatusCode      int
	FinalURL        string
	ContentType     string
	CompressedBytes int64
	Bytes           int64
	Duration        time.Duration
	ErrorCode       ErrorCode
	RetryAfter      time.Time
	ETag            string
	LastModified    string
}

type Result struct {
	Outcome    Outcome
	Attempts   []Attempt
	Checkpoint Checkpoint
	ObjectKey  string
	SHA256     [sha256.Size]byte
	Bytes      int64
}

type FetchError struct {
	Code      ErrorCode
	Retryable bool
	Err       error
}

func (fetchError *FetchError) Error() string {
	return fetchError.Err.Error()
}

func (fetchError *FetchError) Unwrap() error {
	return fetchError.Err
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ObjectStore interface {
	Stage(context.Context, string, io.Reader) (storage.StagedObject, error)
	Commit(context.Context, storage.StagedObject, string) error
	Abort(context.Context, storage.StagedObject) error
}

type RequestLimiter interface {
	Acquire(context.Context, string) (func(), error)
}
