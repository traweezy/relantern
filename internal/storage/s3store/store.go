package s3store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/traweezy/relantern/internal/storage"
)

const temporaryKeyPrefix = "_incoming/"

type Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

type Store struct {
	client objectClient
	bucket string
}

type objectClient interface {
	BucketExists(context.Context, string) (bool, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObject(context.Context, minio.CopyDestOptions, minio.CopySrcOptions) (minio.UploadInfo, error)
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
}

func (store *Store) Check(ctx context.Context) error {
	exists, err := store.client.BucketExists(ctx, store.bucket)
	if err != nil {
		return fmt.Errorf("check object-storage bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("object-storage bucket %q does not exist", store.bucket)
	}
	return nil
}

func New(configuration Config) (*Store, error) {
	endpoint, secure, err := parseEndpoint(configuration.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(configuration.Bucket) == "" {
		return nil, errors.New("object-storage bucket is required")
	}
	if configuration.AccessKey == "" || configuration.SecretKey == "" {
		return nil, errors.New("object-storage credentials are required")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(configuration.AccessKey, configuration.SecretKey, ""),
		Secure:    secure,
		Region:    configuration.Region,
		Transport: objectStorageTransport(),
	})
	if err != nil {
		return nil, fmt.Errorf("create S3-compatible client: %w", err)
	}
	client.SetAppInfo("relantern", "0.0.0")
	return &Store{client: client, bucket: configuration.Bucket}, nil
}

func (store *Store) Stage(ctx context.Context, contentType string, body io.Reader) (storage.StagedObject, error) {
	temporaryKey, err := randomTemporaryKey()
	if err != nil {
		return storage.StagedObject{}, err
	}
	hasher := sha256.New()
	counter := &byteCounter{}
	tee := io.TeeReader(body, io.MultiWriter(hasher, counter))
	upload, err := store.client.PutObject(ctx, store.bucket, temporaryKey, tee, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		_ = store.removeStaged(ctx, temporaryKey)
		return storage.StagedObject{}, fmt.Errorf("stage raw object: %w", err)
	}
	if upload.Size != counter.total {
		_ = store.removeStaged(ctx, temporaryKey)
		return storage.StagedObject{}, fmt.Errorf("staged object size %d does not match streamed size %d", upload.Size, counter.total)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return storage.StagedObject{
		TemporaryKey: temporaryKey,
		SHA256:       digest,
		Bytes:        counter.total,
		ContentType:  contentType,
	}, nil
}

func (store *Store) Commit(ctx context.Context, staged storage.StagedObject, objectKey string) error {
	if !strings.HasPrefix(staged.TemporaryKey, temporaryKeyPrefix) {
		return errors.New("refusing to commit an object outside the staging prefix")
	}
	if !strings.HasPrefix(objectKey, "raw/") {
		return errors.New("refusing to commit an object outside the raw prefix")
	}
	_, err := store.client.CopyObject(ctx, minio.CopyDestOptions{
		Bucket:      store.bucket,
		Object:      objectKey,
		ContentType: staged.ContentType,
	}, minio.CopySrcOptions{
		Bucket: store.bucket,
		Object: staged.TemporaryKey,
	})
	if err != nil {
		return fmt.Errorf("commit raw object: %w", err)
	}
	if err := store.client.RemoveObject(ctx, store.bucket, staged.TemporaryKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove committed staging object: %w", err)
	}
	return nil
}

func (store *Store) Abort(ctx context.Context, staged storage.StagedObject) error {
	if !strings.HasPrefix(staged.TemporaryKey, temporaryKeyPrefix) {
		return errors.New("refusing to abort an object outside the staging prefix")
	}
	if err := store.client.RemoveObject(ctx, store.bucket, staged.TemporaryKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove staged object: %w", err)
	}
	return nil
}

func parseEndpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", false, errors.New("object-storage endpoint must be an absolute HTTP(S) origin")
	}
	switch parsed.Scheme {
	case "https":
		return parsed.Host, true, nil
	case "http":
		return parsed.Host, false, nil
	default:
		return "", false, errors.New("object-storage endpoint must use HTTP or HTTPS")
	}
}

func (store *Store) removeStaged(ctx context.Context, temporaryKey string) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return store.client.RemoveObject(cleanupContext, store.bucket, temporaryKey, minio.RemoveObjectOptions{})
}

func objectStorageTransport() *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           20,
		MaxIdleConnsPerHost:    4,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func randomTemporaryKey() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate staging object key: %w", err)
	}
	return temporaryKeyPrefix + hex.EncodeToString(random), nil
}

type byteCounter struct {
	total int64
}

func (counter *byteCounter) Write(payload []byte) (int, error) {
	counter.total += int64(len(payload))
	return len(payload), nil
}
