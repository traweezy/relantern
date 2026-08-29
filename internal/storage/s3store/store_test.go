package s3store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/traweezy/relantern/internal/storage"
)

type fakeObjectClient struct {
	bucketExists bool
	bucketError  error
	putError     error
	putSize      int64
	copyError    error
	removeError  error
	openError    error
	payload      []byte
	readPayload  []byte
	putKey       string
	copySource   string
	copyTarget   string
	removed      []string
}

func (client *fakeObjectClient) OpenObject(context.Context, string, string) (io.ReadCloser, error) {
	if client.openError != nil {
		return nil, client.openError
	}
	return io.NopCloser(bytes.NewReader(client.readPayload)), nil
}

func (client *fakeObjectClient) BucketExists(context.Context, string) (bool, error) {
	return client.bucketExists, client.bucketError
}

func (client *fakeObjectClient) PutObject(_ context.Context, _ string, key string, body io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	client.payload = payload
	client.putKey = key
	if client.putError != nil {
		return minio.UploadInfo{}, client.putError
	}
	size := client.putSize
	if size == 0 {
		size = int64(len(payload))
	}
	return minio.UploadInfo{Size: size}, nil
}

func (client *fakeObjectClient) CopyObject(_ context.Context, destination minio.CopyDestOptions, source minio.CopySrcOptions) (minio.UploadInfo, error) {
	client.copySource = source.Object
	client.copyTarget = destination.Object
	return minio.UploadInfo{}, client.copyError
}

func (client *fakeObjectClient) RemoveObject(_ context.Context, _ string, key string, _ minio.RemoveObjectOptions) error {
	client.removed = append(client.removed, key)
	return client.removeError
}

func TestStoreCheckRequiresExistingBucket(t *testing.T) {
	tests := []struct {
		name    string
		client  *fakeObjectClient
		wantErr bool
	}{
		{name: "exists", client: &fakeObjectClient{bucketExists: true}},
		{name: "missing", client: &fakeObjectClient{}, wantErr: true},
		{name: "error", client: &fakeObjectClient{bucketError: errors.New("unavailable")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (&Store{client: test.client, bucket: "fixture"}).Check(context.Background())
			if (err != nil) != test.wantErr {
				t.Fatalf("Check() error = %v", err)
			}
		})
	}
}

func TestStoreStageHashesStreamAndRejectsMismatchedUpload(t *testing.T) {
	client := &fakeObjectClient{}
	store := &Store{client: client, bucket: "fixture"}
	staged, err := store.Stage(context.Background(), "text/plain", strings.NewReader("fixture"))
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if staged.Bytes != 7 || staged.SHA256 != sha256.Sum256([]byte("fixture")) || !bytes.Equal(client.payload, []byte("fixture")) || !strings.HasPrefix(staged.TemporaryKey, temporaryKeyPrefix) {
		t.Fatalf("Stage() = %+v, payload = %q", staged, client.payload)
	}

	mismatch := &fakeObjectClient{putSize: 99}
	if _, err := (&Store{client: mismatch, bucket: "fixture"}).Stage(context.Background(), "text/plain", strings.NewReader("fixture")); err == nil || len(mismatch.removed) != 1 {
		t.Fatalf("mismatched Stage() error = %v, removed = %v", err, mismatch.removed)
	}
	failing := &fakeObjectClient{putError: errors.New("upload failed")}
	if _, err := (&Store{client: failing, bucket: "fixture"}).Stage(context.Background(), "text/plain", strings.NewReader("fixture")); err == nil || len(failing.removed) != 1 {
		t.Fatalf("failed Stage() error = %v, removed = %v", err, failing.removed)
	}
}

func TestStoreReadsOnlyValidatedBoundedObjects(t *testing.T) {
	digest := sha256.Sum256([]byte("fixture"))
	key := "normalized/source/" + fmt.Sprintf("%x", digest) + ".txt"
	client := &fakeObjectClient{readPayload: []byte("fixture")}
	store := &Store{client: client, bucket: "fixture"}
	payload, err := store.Read(context.Background(), key, 7)
	if err != nil || string(payload) != "fixture" {
		t.Fatalf("Read() = %q, %v", payload, err)
	}
	if _, err := store.Read(context.Background(), key, 6); err == nil {
		t.Fatal("Read() accepted an object larger than its limit")
	}
	if _, err := store.Read(context.Background(), "private/secret", 7); err == nil {
		t.Fatal("Read() accepted an unapproved object key")
	}
	if _, err := store.Read(context.Background(), key, 0); err == nil {
		t.Fatal("Read() accepted an invalid limit")
	}
	store.client = &fakeObjectClient{openError: errors.New("unavailable")}
	if _, err := store.Read(context.Background(), key, 7); err == nil {
		t.Fatal("Read() ignored an object-open error")
	}
}

func TestStoreCommitAndAbortConstrainPrefixes(t *testing.T) {
	staged := storage.StagedObject{TemporaryKey: temporaryKeyPrefix + strings.Repeat("a", 32), ContentType: "text/plain"}
	client := &fakeObjectClient{}
	store := &Store{client: client, bucket: "fixture"}
	rawDigest := sha256.Sum256([]byte("fixture"))
	rawKey := "raw/source/2026/08/29/" + fmt.Sprintf("%x", rawDigest) + ".txt"
	if err := store.Commit(context.Background(), staged, rawKey); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if client.copySource != staged.TemporaryKey || client.copyTarget != rawKey || len(client.removed) != 1 {
		t.Fatalf("copy source=%q target=%q removed=%v", client.copySource, client.copyTarget, client.removed)
	}
	if err := store.Commit(context.Background(), storage.StagedObject{TemporaryKey: "raw/not-staged"}, rawKey); err == nil {
		t.Fatal("Commit() accepted a non-staging source")
	}
	if err := store.Commit(context.Background(), staged, "other/key"); err == nil {
		t.Fatal("Commit() accepted an unapproved destination")
	}
	if err := store.Commit(context.Background(), staged, "raw/../source/2026/08/29/"+fmt.Sprintf("%x", rawDigest)+".txt"); err == nil {
		t.Fatal("Commit() accepted a path-traversal-shaped destination")
	}
	normalizedKey := "normalized/source/" + fmt.Sprintf("%x", rawDigest) + ".txt"
	if err := store.Commit(context.Background(), staged, normalizedKey); err != nil {
		t.Fatalf("Commit(normalized) error = %v", err)
	}
	if err := store.Abort(context.Background(), storage.StagedObject{TemporaryKey: "raw/not-staged"}); err == nil {
		t.Fatal("Abort() accepted a non-staging source")
	}
	if err := store.Abort(context.Background(), staged); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
}

func TestStorePropagatesCopyAndRemoveFailures(t *testing.T) {
	staged := storage.StagedObject{TemporaryKey: temporaryKeyPrefix + strings.Repeat("a", 32), ContentType: "text/plain"}
	digest := sha256.Sum256([]byte("fixture"))
	key := "raw/source/2026/08/29/" + fmt.Sprintf("%x", digest) + ".txt"
	if err := (&Store{client: &fakeObjectClient{copyError: errors.New("copy failed")}, bucket: "fixture"}).Commit(context.Background(), staged, key); err == nil {
		t.Fatal("Commit() ignored copy failure")
	}
	removeFailure := &Store{client: &fakeObjectClient{removeError: errors.New("remove failed")}, bucket: "fixture"}
	if err := removeFailure.Commit(context.Background(), staged, key); err == nil {
		t.Fatal("Commit() ignored staging cleanup failure")
	}
	if err := removeFailure.Abort(context.Background(), staged); err == nil {
		t.Fatal("Abort() ignored staging cleanup failure")
	}
}

func TestNewValidatesS3Configuration(t *testing.T) {
	tests := []Config{
		{Endpoint: "ftp://storage.example", Bucket: "fixture", AccessKey: "access", SecretKey: "secret"},
		{Endpoint: "https://storage.example/path", Bucket: "fixture", AccessKey: "access", SecretKey: "secret"},
		{Endpoint: "https://storage.example", AccessKey: "access", SecretKey: "secret"},
		{Endpoint: "https://storage.example", Bucket: "fixture"},
	}
	for _, configuration := range tests {
		if _, err := New(configuration); err == nil {
			t.Fatalf("New(%+v) succeeded", configuration)
		}
	}
	if _, err := New(Config{Endpoint: "https://storage.example", Bucket: "fixture", Region: "us-east-1", AccessKey: "access", SecretKey: "secret"}); err != nil {
		t.Fatalf("New(valid) error = %v", err)
	}
}

func TestRandomTemporaryKeyAndCounter(t *testing.T) {
	key, err := randomTemporaryKey()
	if err != nil || !strings.HasPrefix(key, temporaryKeyPrefix) || len(key) != len(temporaryKeyPrefix)+32 {
		t.Fatalf("randomTemporaryKey() = %q, %v", key, err)
	}
	counter := &byteCounter{}
	if written, err := counter.Write([]byte("fixture")); err != nil || written != 7 || counter.total != 7 {
		t.Fatalf("byteCounter.Write() = %d, %v, total %d", written, err, counter.total)
	}
	for _, invalid := range []string{"_incoming/fixture", "_incoming/../../raw", "raw/0123456789abcdef0123456789abcdef"} {
		if validTemporaryKey(invalid) {
			t.Fatalf("validTemporaryKey(%q) = true", invalid)
		}
	}
}
