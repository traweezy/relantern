package fetcher

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/storage"
)

const (
	ContentPolicyLinkAndExcerpt = "link-and-excerpt"
	ContentPolicyMetadataOnly   = "metadata-only"
)

type Config struct {
	UserAgent               string
	RetryAttempts           int
	BaseBackoff             time.Duration
	MaximumRetryDelay       time.Duration
	MaximumCompressionRatio int64
	Now                     func() time.Time
	Sleep                   func(context.Context, time.Duration) error
	Jitter                  func(time.Duration) time.Duration
}

type Fetcher struct {
	configuration Config
	policy        *Policy
	client        HTTPDoer
	limiter       RequestLimiter
	store         ObjectStore
}

func New(configuration Config, policy *Policy, client HTTPDoer, limiter RequestLimiter, store ObjectStore) (*Fetcher, error) {
	if strings.TrimSpace(configuration.UserAgent) == "" || !validHeaderValue(configuration.UserAgent) {
		return nil, errors.New("a single-line contact-bearing user agent is required")
	}
	if configuration.RetryAttempts < 1 || configuration.RetryAttempts > 5 {
		return nil, errors.New("retry attempts must be between one and five")
	}
	if configuration.BaseBackoff <= 0 || configuration.MaximumRetryDelay <= 0 || configuration.BaseBackoff > configuration.MaximumRetryDelay {
		return nil, errors.New("retry delays must be positive and ordered")
	}
	if configuration.MaximumCompressionRatio < 1 {
		return nil, errors.New("maximum compression ratio must be positive")
	}
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	if policy == nil {
		return nil, errors.New("network policy is required")
	}
	if limiter == nil {
		limiter = unlimitedLimiter{}
	}
	if store == nil {
		return nil, errors.New("object store is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	if configuration.Sleep == nil {
		configuration.Sleep = sleep
	}
	if configuration.Jitter == nil {
		configuration.Jitter = cryptoJitter
	}
	return &Fetcher{configuration: configuration, policy: policy, client: client, limiter: limiter, store: store}, nil
}

func DefaultConfig(userAgent string) Config {
	return Config{
		UserAgent:               userAgent,
		RetryAttempts:           5,
		BaseBackoff:             500 * time.Millisecond,
		MaximumRetryDelay:       30 * time.Second,
		MaximumCompressionRatio: 100,
	}
}

func (fetcher *Fetcher) Fetch(ctx context.Context, endpoint Endpoint, checkpoint Checkpoint) (Result, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return Result{Outcome: OutcomeFailed}, err
	}
	targetURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return Result{Outcome: OutcomeFailed}, newFetchError(ErrorInvalidURL, false, fmt.Errorf("parse endpoint URL: %w", err))
	}
	requestContext, err := withAllowedHosts(ctx, endpoint.AllowedHosts)
	if err != nil {
		return Result{Outcome: OutcomeFailed}, newFetchError(ErrorInvalidURL, false, err)
	}
	if _, err := fetcher.policy.ValidateURL(requestContext, targetURL); err != nil {
		return Result{Outcome: OutcomeFailed}, err
	}
	result := Result{Outcome: OutcomeFailed, Checkpoint: cloneCheckpoint(checkpoint)}
	for attemptNumber := 1; attemptNumber <= fetcher.configuration.RetryAttempts; attemptNumber++ {
		attempt, response, requestErr := fetcher.request(requestContext, endpoint, targetURL, checkpoint)
		result.Attempts = append(result.Attempts, attempt)
		if requestErr != nil {
			if !retryable(requestErr) || attemptNumber == fetcher.configuration.RetryAttempts {
				return result, requestErr
			}
			if err := fetcher.waitToRetry(requestContext, attemptNumber, attempt.RetryAfter); err != nil {
				return result, retryWaitError(err)
			}
			continue
		}

		processed, processErr := fetcher.processResponse(requestContext, endpoint, checkpoint, response, attempt)
		result.Attempts[len(result.Attempts)-1] = processed.attempt
		if processErr == nil {
			result.Outcome = processed.outcome
			result.Checkpoint = processed.checkpoint
			result.ObjectKey = processed.objectKey
			result.SHA256 = processed.digest
			result.Bytes = processed.bytes
			return result, nil
		}
		if !retryable(processErr) || attemptNumber == fetcher.configuration.RetryAttempts {
			return result, processErr
		}
		if err := fetcher.waitToRetry(requestContext, attemptNumber, processed.attempt.RetryAfter); err != nil {
			return result, retryWaitError(err)
		}
	}
	return result, newFetchError(ErrorTransport, true, errors.New("retry loop ended without a result"))
}

func (fetcher *Fetcher) request(ctx context.Context, endpoint Endpoint, targetURL *url.URL, checkpoint Checkpoint) (Attempt, *http.Response, error) {
	attemptedAt := fetcher.configuration.Now().UTC()
	release, err := fetcher.limiter.Acquire(ctx, strings.ToLower(targetURL.Hostname()))
	if err != nil {
		attempt := completedAttempt(attemptedAt, fetcher.configuration.Now(), 0)
		attempt.ErrorCode = ErrorTransport
		return attempt, nil, newFetchError(ErrorTransport, true, fmt.Errorf("acquire request capacity: %w", err))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		release()
		attempt := completedAttempt(attemptedAt, fetcher.configuration.Now(), 0)
		attempt.ErrorCode = ErrorInvalidURL
		return attempt, nil, newFetchError(ErrorInvalidURL, false, fmt.Errorf("create fetch request: %w", err))
	}
	request.Header.Set("User-Agent", fetcher.configuration.UserAgent)
	request.Header.Set("Accept", strings.Join(endpoint.ExpectedContentTypes, ", "))
	request.Header.Set("Accept-Encoding", "gzip")
	if etag := boundedHeader(checkpoint.ETag); etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if checkpoint.LastModified != "" {
		if modifiedAt, parseErr := http.ParseTime(checkpoint.LastModified); parseErr == nil {
			request.Header.Set("If-Modified-Since", modifiedAt.UTC().Format(http.TimeFormat))
		}
	}
	response, err := fetcher.client.Do(request)
	completedAt := fetcher.configuration.Now().UTC()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		release()
		attempt := completedAttempt(attemptedAt, completedAt, 0)
		attempt.ErrorCode = errorCode(err, ErrorTransport)
		return attempt, nil, newFetchError(attempt.ErrorCode, retryableTransport(err), fmt.Errorf("fetch %s: %w", endpoint.ID, err))
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	response.Body = newReleaseReadCloser(response.Body, release)
	attempt := completedAttempt(attemptedAt, completedAt, response.StatusCode)
	attempt.FinalURL = response.Request.URL.String()
	attempt.ContentType = response.Header.Get("Content-Type")
	attempt.ETag = boundedHeader(response.Header.Get("ETag"))
	attempt.LastModified = validLastModified(response.Header.Get("Last-Modified"))
	attempt.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), completedAt)
	return attempt, response, nil
}

type processedResponse struct {
	attempt    Attempt
	outcome    Outcome
	checkpoint Checkpoint
	objectKey  string
	digest     [sha256.Size]byte
	bytes      int64
}

func (fetcher *Fetcher) processResponse(ctx context.Context, endpoint Endpoint, prior Checkpoint, response *http.Response, attempt Attempt) (processed processedResponse, returnedErr error) {
	defer response.Body.Close()
	processed = processedResponse{attempt: attempt, checkpoint: cloneCheckpoint(prior)}
	defer func() {
		completedAt := fetcher.configuration.Now().UTC()
		processed.attempt.CompletedAt = completedAt
		processed.attempt.Duration = completedAt.Sub(processed.attempt.AttemptedAt)
		if processed.attempt.Duration < 0 {
			processed.attempt.Duration = 0
		}
	}()
	processed.checkpoint.ETag = firstNonEmpty(attempt.ETag, prior.ETag)
	processed.checkpoint.LastModified = firstNonEmpty(attempt.LastModified, prior.LastModified)
	if response.StatusCode == http.StatusNotModified {
		processed.outcome = OutcomeNotModified
		return processed, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		processed.attempt.ErrorCode = ErrorUnexpectedStatus
		retryableStatus := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		return processed, newFetchError(ErrorUnexpectedStatus, retryableStatus, fmt.Errorf("source returned HTTP %d", response.StatusCode))
	}
	if !allowedContentType(attempt.ContentType, endpoint.ExpectedContentTypes) {
		processed.attempt.ErrorCode = ErrorContentType
		return processed, newFetchError(ErrorContentType, false, fmt.Errorf("content type %q is not allowed", attempt.ContentType))
	}
	if response.ContentLength > endpoint.MaxBodyBytes {
		processed.attempt.ErrorCode = ErrorCompressedTooLarge
		return processed, newFetchError(ErrorCompressedTooLarge, false, fmt.Errorf("declared body size %d exceeds limit %d", response.ContentLength, endpoint.MaxBodyBytes))
	}
	body, err := decodeBody(response.Body, response.Header.Get("Content-Encoding"), endpoint.MaxBodyBytes, endpoint.MaxBodyBytes)
	if err != nil {
		processed.attempt.ErrorCode = errorCode(err, ErrorUnsupportedEncoding)
		return processed, err
	}
	defer body.close()

	if endpoint.ContentPolicy == ContentPolicyMetadataOnly {
		processed.digest, processed.bytes, err = hashMetadataOnly(body.reader)
		processed.outcome = OutcomeMetadataOnly
	} else {
		staged, stageErr := fetcher.store.Stage(ctx, attempt.ContentType, body.reader)
		if stageErr != nil {
			processed.attempt.ErrorCode = ErrorObjectStorage
			return processed, newFetchError(ErrorObjectStorage, true, stageErr)
		}
		processed.digest = staged.SHA256
		processed.bytes = staged.Bytes
		if validationErr := fetcher.validateBodySize(staged.Bytes, body.compressed.total, endpoint.MaxBodyBytes); validationErr != nil {
			_ = fetcher.abort(ctx, staged)
			processed.attempt.ErrorCode = errorCode(validationErr, ErrorBodyTooLarge)
			return processed, validationErr
		}
		objectKey, keyErr := storage.RawObjectKey(endpoint.SourceID, attempt.AttemptedAt, staged.SHA256, attempt.ContentType)
		if keyErr != nil {
			_ = fetcher.abort(ctx, staged)
			processed.attempt.ErrorCode = ErrorObjectStorage
			return processed, newFetchError(ErrorObjectStorage, false, keyErr)
		}
		if commitErr := fetcher.store.Commit(ctx, staged, objectKey); commitErr != nil {
			_ = fetcher.abort(ctx, staged)
			processed.attempt.ErrorCode = ErrorObjectStorage
			return processed, newFetchError(ErrorObjectStorage, true, commitErr)
		}
		processed.objectKey = objectKey
		processed.outcome = OutcomeStored
	}
	if err != nil {
		processed.attempt.ErrorCode = ErrorTransport
		return processed, newFetchError(ErrorTransport, true, fmt.Errorf("read response body: %w", err))
	}
	if validationErr := fetcher.validateBodySize(processed.bytes, body.compressed.total, endpoint.MaxBodyBytes); validationErr != nil {
		processed.attempt.ErrorCode = errorCode(validationErr, ErrorBodyTooLarge)
		return processed, validationErr
	}
	processed.attempt.CompressedBytes = body.compressed.total
	processed.attempt.Bytes = processed.bytes
	return processed, nil
}

func (fetcher *Fetcher) validateBodySize(decompressed int64, compressed int64, maximum int64) error {
	if compressed > maximum {
		return newFetchError(ErrorCompressedTooLarge, false, fmt.Errorf("compressed body exceeds %d bytes", maximum))
	}
	if decompressed > maximum {
		return newFetchError(ErrorBodyTooLarge, false, fmt.Errorf("decompressed body exceeds %d bytes", maximum))
	}
	if compressed > 0 && decompressed > compressed*fetcher.configuration.MaximumCompressionRatio {
		return newFetchError(ErrorCompressionRatio, false, fmt.Errorf("compression ratio exceeds %d:1", fetcher.configuration.MaximumCompressionRatio))
	}
	return nil
}

func (fetcher *Fetcher) abort(ctx context.Context, staged storage.StagedObject) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return fetcher.store.Abort(cleanupContext, staged)
}

func (fetcher *Fetcher) waitToRetry(ctx context.Context, attempt int, retryAfter time.Time) error {
	now := fetcher.configuration.Now().UTC()
	delay := time.Duration(1<<(attempt-1)) * fetcher.configuration.BaseBackoff
	if retryAfter.After(now) {
		delay = retryAfter.Sub(now)
	} else {
		delay += fetcher.configuration.Jitter(delay / 2)
	}
	if delay > fetcher.configuration.MaximumRetryDelay {
		return newFetchError(ErrorUnexpectedStatus, true, fmt.Errorf("retry is deferred until %s", now.Add(delay).Format(time.RFC3339)))
	}
	return fetcher.configuration.Sleep(ctx, delay)
}

func validateEndpoint(endpoint Endpoint) error {
	if endpoint.ID == "" || endpoint.SourceID == "" || endpoint.URL == "" {
		return newFetchError(ErrorInvalidURL, false, errors.New("endpoint identity and URL are required"))
	}
	if len(endpoint.ExpectedContentTypes) == 0 || endpoint.MaxBodyBytes <= 0 {
		return newFetchError(ErrorInvalidURL, false, errors.New("endpoint content types and body limit are required"))
	}
	if endpoint.ContentPolicy != ContentPolicyLinkAndExcerpt && endpoint.ContentPolicy != ContentPolicyMetadataOnly {
		return newFetchError(ErrorInvalidURL, false, fmt.Errorf("unsupported content policy %q", endpoint.ContentPolicy))
	}
	return nil
}

func allowedContentType(actual string, expected []string) bool {
	actualMediaType, _, err := mime.ParseMediaType(actual)
	if err != nil {
		return false
	}
	for _, candidate := range expected {
		candidateMediaType, _, candidateErr := mime.ParseMediaType(candidate)
		if candidateErr == nil && strings.EqualFold(actualMediaType, candidateMediaType) {
			return true
		}
	}
	return false
}

func completedAttempt(started time.Time, completed time.Time, status int) Attempt {
	duration := completed.Sub(started)
	if duration < 0 {
		duration = 0
	}
	return Attempt{AttemptedAt: started, CompletedAt: completed, Duration: duration, StatusCode: status}
}

func parseRetryAfter(raw string, now time.Time) time.Time {
	trimmed := strings.TrimSpace(raw)
	if seconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if retryAt, err := http.ParseTime(trimmed); err == nil && retryAt.After(now) {
		return retryAt.UTC()
	}
	return time.Time{}
}

func validLastModified(raw string) string {
	if parsed, err := http.ParseTime(raw); err == nil {
		return parsed.UTC().Format(http.TimeFormat)
	}
	return ""
}

func boundedHeader(raw string) string {
	if len(raw) > 1024 || !validHeaderValue(raw) {
		return ""
	}
	return strings.TrimSpace(raw)
}

func validHeaderValue(value string) bool {
	for _, character := range []byte(value) {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneCheckpoint(checkpoint Checkpoint) Checkpoint {
	state := make(map[string]any, len(checkpoint.ProviderState))
	for key, value := range checkpoint.ProviderState {
		state[key] = value
	}
	checkpoint.ProviderState = state
	return checkpoint
}

func retryable(err error) bool {
	var fetchError *FetchError
	return errors.As(err, &fetchError) && fetchError.Retryable
}

func retryableTransport(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var fetchError *FetchError
	if errors.As(err, &fetchError) {
		return fetchError.Retryable
	}
	return true
}

func retryWaitError(err error) error {
	var fetchError *FetchError
	if errors.As(err, &fetchError) {
		return err
	}
	return newFetchError(ErrorTransport, true, fmt.Errorf("wait to retry: %w", err))
}

func errorCode(err error, fallback ErrorCode) ErrorCode {
	var fetchError *FetchError
	if errors.As(err, &fetchError) {
		return fetchError.Code
	}
	return fallback
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cryptoJitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	limit := uint64(maximum)
	var randomBytes [8]byte
	if _, err := io.ReadFull(rand.Reader, randomBytes[:]); err != nil {
		return 0
	}
	value := uint64(randomBytes[0])<<56 |
		uint64(randomBytes[1])<<48 |
		uint64(randomBytes[2])<<40 |
		uint64(randomBytes[3])<<32 |
		uint64(randomBytes[4])<<24 |
		uint64(randomBytes[5])<<16 |
		uint64(randomBytes[6])<<8 |
		uint64(randomBytes[7])
	return time.Duration(value % (limit + 1))
}
