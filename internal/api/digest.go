package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/digest"
)

type DigestSnapshotOutput struct {
	Body digest.DigestSnapshot
}

type DigestInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	DigestID      string `path:"digestId" minLength:"36" maxLength:"36"`
}

type DigestOutput struct {
	Body digest.DigestRecord
}

func registerDigest(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "list-digests", Method: http.MethodGet, Path: "/api/v1/digests",
		Summary: "List immutable owner digest payloads and delivery states", Tags: []string{"digests"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*DigestSnapshotOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.digest != nil); err != nil {
			return nil, err
		}
		result, err := configuration.digest.Snapshot(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapDigestError(ctx, logger, "list digests", err)
		}
		return &DigestSnapshotOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-digest", Method: http.MethodGet, Path: "/api/v1/digests/{digestId}",
		Summary: "Get one immutable owner digest and its selected evidence", Tags: []string{"digests"},
	}, func(ctx context.Context, input *DigestInput) (*DigestOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.digest != nil); err != nil {
			return nil, err
		}
		result, err := configuration.digest.Get(ctx, input.UserID, input.DigestID)
		if err != nil {
			return nil, mapDigestError(ctx, logger, "get digest", err)
		}
		return &DigestOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "retry-digest-delivery", Method: http.MethodPost,
		Path:    "/api/v1/digests/{digestId}/retry-delivery",
		Summary: "Retry a failed external delivery without regenerating its payload", Tags: []string{"digests"},
	}, func(ctx context.Context, input *DigestInput) (*DigestOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.digest != nil); err != nil {
			return nil, err
		}
		result, err := configuration.digest.Retry(ctx, input.UserID, input.DigestID, configuration.clock().UTC())
		if err != nil {
			return nil, mapDigestError(ctx, logger, "retry digest delivery", err)
		}
		return &DigestOutput{Body: result}, nil
	})
}

func mapDigestError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	switch {
	case errors.Is(err, digest.ErrInvalid):
		return huma.Error400BadRequest("The digest request is invalid.")
	case errors.Is(err, digest.ErrNotFound):
		return huma.Error404NotFound("The requested digest was not found.")
	case errors.Is(err, digest.ErrConflict):
		return huma.Error409Conflict("Only failed external digest deliveries can be retried.")
	default:
		logger.ErrorContext(ctx, operation+" failed", "error", err)
		return huma.Error500InternalServerError("The private digest service is unavailable.")
	}
}
