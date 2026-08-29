package fetcher

import (
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
)

type bodyStream struct {
	reader     io.Reader
	compressed *countingReader
	close      func() error
}

func decodeBody(body io.Reader, contentEncoding string, compressedLimit int64, decompressedLimit int64) (*bodyStream, error) {
	if compressedLimit <= 0 || decompressedLimit <= 0 {
		return nil, errors.New("body limits must be positive")
	}
	compressed := &countingReader{reader: io.LimitReader(body, compressedLimit+1)}
	encoding := strings.ToLower(strings.TrimSpace(contentEncoding))
	switch encoding {
	case "", "identity":
		return &bodyStream{
			reader:     io.LimitReader(compressed, decompressedLimit+1),
			compressed: compressed,
			close:      func() error { return nil },
		}, nil
	case "gzip":
		decompressor, err := gzip.NewReader(compressed)
		if err != nil {
			return nil, fmt.Errorf("open gzip response: %w", err)
		}
		return &bodyStream{
			reader:     io.LimitReader(decompressor, decompressedLimit+1),
			compressed: compressed,
			close:      decompressor.Close,
		}, nil
	default:
		return nil, newFetchError(ErrorUnsupportedEncoding, false, fmt.Errorf("content encoding %q is not supported", contentEncoding))
	}
}

func hashMetadataOnly(body io.Reader) ([sha256.Size]byte, int64, error) {
	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, body)
	if err != nil {
		return [sha256.Size]byte{}, bytesRead, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, bytesRead, nil
}

type countingReader struct {
	reader io.Reader
	total  int64
}

func (reader *countingReader) Read(payload []byte) (int, error) {
	read, err := reader.reader.Read(payload)
	reader.total += int64(read)
	return read, err
}
