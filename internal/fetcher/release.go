package fetcher

import (
	"io"
	"sync"
)

type releaseReadCloser struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func newReleaseReadCloser(body io.ReadCloser, release func()) *releaseReadCloser {
	return &releaseReadCloser{ReadCloser: body, release: release}
}

func (body *releaseReadCloser) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(body.release)
	return err
}
