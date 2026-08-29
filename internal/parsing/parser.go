package parsing

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/traweezy/relantern/internal/sources"
	"golang.org/x/net/html/charset"
)

const (
	maximumFeedBytes = int64(5 << 20)
	maximumPageBytes = int64(15 << 20)
	maximumJSONBytes = int64(10 << 20)
)

type Parser struct{}

func New() Parser {
	return Parser{}
}

func (Parser) Parse(ctx context.Context, request Request) (Result, error) {
	if request.Body == nil {
		return Result{}, parserError(ErrorInvalidDocument, "parser body is required")
	}
	parsedURL, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.Fragment != "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return Result{}, parserError(ErrorInvalidURL, "parser URL must be an absolute HTTP(S) URL")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, parserError(ErrorCanceled, "parse canceled: %w", err)
	}
	limit, err := parseLimit(request.Connector, request.MaxBytes)
	if err != nil {
		return Result{}, err
	}
	rawBody, err := readBounded(request.Body, limit)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, parserError(ErrorCanceled, "parse canceled: %w", err)
	}
	decodedBody, err := decodeToUTF8(rawBody, request.ContentType, limit)
	if err != nil {
		return Result{}, err
	}

	var extracted extractedDocument
	switch request.Connector {
	case sources.ConnectorAtom, sources.ConnectorRSS, sources.ConnectorJSONFeed:
		extracted, err = parseFeed(request.Connector, parsedURL, decodedBody)
	case sources.ConnectorPage:
		extracted, err = parsePage(parsedURL, decodedBody)
	case sources.ConnectorGitHubReleases, sources.ConnectorGitHubAdvisories, sources.ConnectorRegistry, sources.ConnectorStructuredAPI:
		extracted, err = parseJSON(request.Connector, parsedURL, decodedBody)
	default:
		err = parserError(ErrorUnsupported, "unsupported connector %q", request.Connector)
	}
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, parserError(ErrorCanceled, "parse canceled: %w", err)
	}
	return finalize(extracted, string(decodedBody))
}

func parseLimit(connector sources.Connector, requested int64) (int64, error) {
	var maximum int64
	switch connector {
	case sources.ConnectorAtom, sources.ConnectorRSS, sources.ConnectorJSONFeed:
		maximum = maximumFeedBytes
	case sources.ConnectorPage:
		maximum = maximumPageBytes
	case sources.ConnectorGitHubReleases, sources.ConnectorGitHubAdvisories, sources.ConnectorRegistry, sources.ConnectorStructuredAPI:
		maximum = maximumJSONBytes
	default:
		return 0, parserError(ErrorUnsupported, "unsupported connector %q", connector)
	}
	if requested <= 0 {
		return maximum, nil
	}
	if requested > maximum {
		return 0, parserError(ErrorBodyTooLarge, "parser byte limit %d exceeds connector maximum %d", requested, maximum)
	}
	return requested, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, parserError(ErrorInvalidDocument, "read parser body: %v", err)
	}
	if int64(len(payload)) > limit {
		return nil, parserError(ErrorBodyTooLarge, "parser body exceeds %d bytes", limit)
	}
	return payload, nil
}

func decodeToUTF8(payload []byte, contentType string, limit int64) ([]byte, error) {
	if len(payload) == 0 {
		return []byte{}, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, parserError(ErrorCharacterEncoding, "invalid parser content type: %v", err)
	}
	if strings.Contains(mediaType, "json") {
		charsetName := strings.ToLower(strings.TrimSpace(parameters["charset"]))
		if charsetName != "" && charsetName != "utf-8" && charsetName != "utf8" {
			return nil, parserError(ErrorCharacterEncoding, "JSON parser input must use UTF-8")
		}
		if !utf8.Valid(payload) {
			return nil, parserError(ErrorCharacterEncoding, "JSON parser input is not valid UTF-8")
		}
		return payload, nil
	}
	decoded, err := charset.NewReader(bytes.NewReader(payload), contentType)
	if err != nil {
		return nil, parserError(ErrorCharacterEncoding, "select parser character decoder: %v", err)
	}
	result, err := io.ReadAll(io.LimitReader(decoded, limit+1))
	if err != nil {
		return nil, parserError(ErrorCharacterEncoding, "decode parser body: %v", err)
	}
	if int64(len(result)) > limit {
		return nil, parserError(ErrorBodyTooLarge, "decoded parser body exceeds %d bytes", limit)
	}
	if !utf8.Valid(result) {
		return nil, parserError(ErrorCharacterEncoding, "decoded parser body is not valid UTF-8")
	}
	return result, nil
}

func errorCode(err error) ErrorCode {
	var parseError *ParseError
	if errors.As(err, &parseError) {
		return parseError.Code
	}
	return ErrorInvalidDocument
}
