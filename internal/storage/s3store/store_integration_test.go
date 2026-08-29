package s3store

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/traweezy/relantern/internal/storage"
)

func TestS3StoreStagesAndCommitsContentAddressedObject(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT is not configured")
	}
	configuration := Config{
		Endpoint:  endpoint,
		Bucket:    os.Getenv("S3_TEST_BUCKET"),
		Region:    "us-east-1",
		AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_TEST_SECRET_KEY"),
	}
	store, err := New(configuration)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adapter, ok := store.client.(minioObjectClient)
	if !ok {
		t.Fatal("New() did not configure the MinIO client adapter")
	}
	client := adapter.Client
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, configuration.Bucket)
	if err != nil {
		t.Fatalf("BucketExists() error = %v", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, configuration.Bucket, minio.MakeBucketOptions{Region: configuration.Region}); err != nil {
			t.Fatalf("MakeBucket() error = %v", err)
		}
	}
	payload := []byte("immutable fixture")
	staged, err := store.Stage(ctx, "text/plain", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	objectKey, err := storage.RawObjectKey("integration-source", time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC), staged.SHA256, staged.ContentType)
	if err != nil {
		t.Fatalf("RawObjectKey() error = %v", err)
	}
	if err := store.Commit(ctx, staged, objectKey); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.RemoveObject(context.Background(), configuration.Bucket, objectKey, minio.RemoveObjectOptions{})
	})
	object, err := client.GetObject(ctx, configuration.Bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer object.Close()
	stored, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read committed object: %v", err)
	}
	if !bytes.Equal(stored, payload) {
		t.Fatalf("stored payload = %q, want %q", stored, payload)
	}
	if _, err := client.StatObject(ctx, configuration.Bucket, staged.TemporaryKey, minio.StatObjectOptions{}); err == nil {
		t.Fatal("staging object still exists after commit")
	}
}
