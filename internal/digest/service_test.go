package digest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/traweezy/relantern/internal/digest"
)

type repositoryFixture struct{}

func (repositoryFixture) Snapshot(_ context.Context, _ string, now time.Time) (digest.DigestSnapshot, error) {
	return digest.DigestSnapshot{GeneratedAt: now}, nil
}

func (repositoryFixture) Get(_ context.Context, _, digestID string) (digest.DigestRecord, error) {
	return digest.DigestRecord{ID: digestID}, nil
}

func (repositoryFixture) Retry(_ context.Context, _, digestID string, _ time.Time) (digest.DigestRecord, error) {
	return digest.DigestRecord{ID: digestID, State: "ready"}, nil
}

func TestServiceValidatesOwnerAndDigestIdentity(t *testing.T) {
	t.Parallel()
	service, err := digest.NewService(repositoryFixture{})
	if err != nil {
		t.Fatal(err)
	}
	userID := "01900000-0000-7000-8000-000000000001"
	digestID := "01900000-0000-7000-8000-000000000002"
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if result, err := service.Retry(context.Background(), userID, digestID, now); err != nil || result.State != "ready" {
		t.Fatalf("Retry() = %+v, %v", result, err)
	}
	if _, err := service.Get(context.Background(), userID, "not-a-uuid"); !errors.Is(err, digest.ErrInvalid) {
		t.Fatalf("Get() error = %v, want ErrInvalid", err)
	}
	if _, err := service.Snapshot(context.Background(), "", now); !errors.Is(err, digest.ErrInvalid) {
		t.Fatalf("Snapshot() error = %v, want ErrInvalid", err)
	}
}
