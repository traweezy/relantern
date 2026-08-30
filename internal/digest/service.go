package digest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Snapshot(context.Context, string, time.Time) (DigestSnapshot, error)
	Get(context.Context, string, string) (DigestRecord, error)
	Retry(context.Context, string, string, time.Time) (DigestRecord, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("digest repository is required")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Snapshot(ctx context.Context, userID string, now time.Time) (DigestSnapshot, error) {
	if !validID(userID) || now.IsZero() {
		return DigestSnapshot{}, ErrInvalid
	}
	return service.repository.Snapshot(ctx, userID, now.UTC())
}

func (service *Service) Get(ctx context.Context, userID string, digestID string) (DigestRecord, error) {
	if !validID(userID) || !validID(digestID) {
		return DigestRecord{}, ErrInvalid
	}
	return service.repository.Get(ctx, userID, digestID)
}

func (service *Service) Retry(
	ctx context.Context,
	userID string,
	digestID string,
	now time.Time,
) (DigestRecord, error) {
	if !validID(userID) || !validID(digestID) || now.IsZero() {
		return DigestRecord{}, ErrInvalid
	}
	return service.repository.Retry(ctx, userID, digestID, now.UTC())
}

func validID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}
