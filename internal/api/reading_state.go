package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/readingstate"
)

type OwnerInternalInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type CollectionInput struct {
	Authorization string `header:"Authorization"`
	Cursor        string `query:"cursor" maxLength:"512"`
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type CollectionOutput struct {
	Body readingstate.Collection
}

type StateMutationInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.Command
}

type MutationOutput struct {
	Body readingstate.MutationResult
}

type StateOutput struct {
	Body readingstate.StoryState
}

type StoryStatesBody struct {
	StoryIDs []string `json:"storyIds" minItems:"1" maxItems:"100"`
}

type StoryStatesInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          StoryStatesBody
}

type StoryStatesOutput struct {
	Body readingstate.StoryStates
}

type UndoBody struct {
	MutationID string `json:"mutationId" minLength:"36" maxLength:"36"`
}

type UndoInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          UndoBody
}

type BulkUndoBody struct {
	BulkID string `json:"bulkId" minLength:"36" maxLength:"36"`
}

type BulkUndoInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          BulkUndoBody
}

type BulkMutationInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.BulkCommand
}

type BulkMutationOutput struct {
	Body readingstate.BulkMutationResult
}

type LaterOrderInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.LaterOrderCommand
}

type TagsOutput struct {
	Body struct {
		Tags []readingstate.Tag `json:"tags"`
	}
}

type CreateTagInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.TagInput
}

type TagInput struct {
	Authorization string `header:"Authorization"`
	TagID         string `path:"tagId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.TagInput
}

type DeleteTagInput struct {
	Authorization string `header:"Authorization"`
	TagID         string `path:"tagId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type TagOutput struct {
	Body readingstate.Tag
}

type FeedbackInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.FeedbackCommand
}

type FeedbackOutput struct {
	Body readingstate.Feedback
}

type DeleteOutput struct {
	Body struct {
		Deleted bool `json:"deleted"`
	}
}

type StoryAnnotationsInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type CreateAnnotationInput struct {
	Authorization string `header:"Authorization"`
	StoryID       string `path:"storyId" minLength:"36" maxLength:"36"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          readingstate.AnnotationInput
}

type AnnotationsOutput struct {
	Body struct {
		Annotations []readingstate.Annotation `json:"annotations"`
	}
}

type AnnotationOutput struct {
	Body readingstate.Annotation
}

type UpdateAnnotationBody struct {
	Body string `json:"body" minLength:"1" maxLength:"20000"`
}

type UpdateAnnotationInput struct {
	AnnotationID  string `path:"annotationId" minLength:"36" maxLength:"36"`
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          UpdateAnnotationBody
}

type DeleteAnnotationInput struct {
	AnnotationID  string `path:"annotationId" minLength:"36" maxLength:"36"`
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

func registerReadingState(api huma.API, configuration options, logger *slog.Logger) {
	registerCollection(api, configuration, logger, readingstate.CollectionInbox, "/api/v1/inbox")
	registerCollection(api, configuration, logger, readingstate.CollectionLater, "/api/v1/later")
	registerCollection(api, configuration, logger, readingstate.CollectionStarred, "/api/v1/starred")
	registerCollection(api, configuration, logger, readingstate.CollectionArchive, "/api/v1/archive")
	registerCollection(api, configuration, logger, readingstate.CollectionSnoozed, "/api/v1/snoozed")

	huma.Register(api, huma.Operation{
		OperationID: "get-story-state",
		Method:      http.MethodGet,
		Path:        "/api/v1/stories/{storyId}/state",
		Summary:     "Get the current owner reading state for one story",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *StoryAnnotationsInput) (*StateOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		state, err := configuration.readingState.State(ctx, input.UserID, input.StoryID)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "read story state", err)
		}
		return &StateOutput{Body: state}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-story-state",
		Method:      http.MethodPatch,
		Path:        "/api/v1/stories/{storyId}/state",
		Summary:     "Apply one optimistic owner reading-state command",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *StateMutationInput) (*MutationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		input.Body.UserID = input.UserID
		input.Body.StoryID = input.StoryID
		result, err := configuration.readingState.Mutate(ctx, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "update story state", err)
		}
		return &MutationOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "query-story-states",
		Method:      http.MethodPost,
		Path:        "/api/v1/stories/state-query",
		Summary:     "Read a bounded ordered set of owner story states",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *StoryStatesInput) (*StoryStatesOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		result, err := configuration.readingState.States(ctx, input.UserID, input.Body.StoryIDs)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "query story states", err)
		}
		return &StoryStatesOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "undo-story-state",
		Method:      http.MethodPost,
		Path:        "/api/v1/stories/{storyId}/undo",
		Summary:     "Undo the latest non-conflicting story mutation within ten seconds",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *UndoInput) (*MutationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		result, err := configuration.readingState.Undo(
			ctx,
			input.UserID,
			input.StoryID,
			input.Body.MutationID,
		)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "undo story state", err)
		}
		return &MutationOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "bulk-update-story-state",
		Method:      http.MethodPost,
		Path:        "/api/v1/stories/bulk-state",
		Summary:     "Apply an atomic state command to a confirmed story selection",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *BulkMutationInput) (*BulkMutationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		input.Body.UserID = input.UserID
		result, err := configuration.readingState.BulkMutate(ctx, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "bulk update story state", err)
		}
		return &BulkMutationOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "undo-bulk-story-state",
		Method:      http.MethodPost,
		Path:        "/api/v1/stories/bulk-state/undo",
		Summary:     "Atomically undo a bulk reading-state command within ten seconds",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *BulkUndoInput) (*BulkMutationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		result, err := configuration.readingState.UndoBulk(ctx, input.UserID, input.Body.BulkID)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "undo bulk story state", err)
		}
		return &BulkMutationOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "reorder-later",
		Method:      http.MethodPut,
		Path:        "/api/v1/later/order",
		Summary:     "Persist an optimistic owner-confirmed Later ordering",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *LaterOrderInput) (*BulkMutationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		input.Body.UserID = input.UserID
		result, err := configuration.readingState.ReorderLater(ctx, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "reorder Later", err)
		}
		return &BulkMutationOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-story-feedback",
		Method:      http.MethodPost,
		Path:        "/api/v1/stories/{storyId}/feedback",
		Summary:     "Record explicit owner relevance feedback independently of reading state",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *FeedbackInput) (*FeedbackOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		input.Body.UserID = input.UserID
		input.Body.StoryID = input.StoryID
		result, err := configuration.readingState.RecordFeedback(ctx, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "record story feedback", err)
		}
		return &FeedbackOutput{Body: result}, nil
	})

	registerTagEndpoints(api, configuration, logger)
	registerAnnotationEndpoints(api, configuration, logger)
}

func registerCollection(
	api huma.API,
	configuration options,
	logger *slog.Logger,
	kind readingstate.CollectionKind,
	path string,
) {
	huma.Register(api, huma.Operation{
		OperationID: "get-" + string(kind),
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "Get the owner " + string(kind) + " reading collection",
		Tags:        []string{"reading-state"},
	}, func(ctx context.Context, input *CollectionInput) (*CollectionOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		result, err := configuration.readingState.Collection(ctx, readingstate.CollectionQuery{
			Cursor: input.Cursor,
			Kind:   kind,
			Limit:  input.Limit,
			Now:    configuration.clock().UTC(),
			UserID: input.UserID,
		})
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "read owner collection", err)
		}
		return &CollectionOutput{Body: result}, nil
	})
}

func registerTagEndpoints(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "get-tags", Method: http.MethodGet, Path: "/api/v1/tags",
		Summary: "Get owner tags", Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*TagsOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		tags, err := configuration.readingState.Tags(ctx, input.UserID)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "read owner tags", err)
		}
		output := &TagsOutput{}
		output.Body.Tags = tags
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-tag", Method: http.MethodPost, Path: "/api/v1/tags",
		Summary: "Create an owner tag", Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *CreateTagInput) (*TagOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		tag, err := configuration.readingState.CreateTag(ctx, input.UserID, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "create owner tag", err)
		}
		return &TagOutput{Body: tag}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-tag", Method: http.MethodPatch, Path: "/api/v1/tags/{tagId}",
		Summary: "Update an owner tag", Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *TagInput) (*TagOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		tag, err := configuration.readingState.UpdateTag(ctx, input.UserID, input.TagID, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "update owner tag", err)
		}
		return &TagOutput{Body: tag}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-tag", Method: http.MethodDelete, Path: "/api/v1/tags/{tagId}",
		Summary: "Delete an owner tag", Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *DeleteTagInput) (*DeleteOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		if err := configuration.readingState.DeleteTag(ctx, input.UserID, input.TagID); err != nil {
			return nil, mapReadingStateError(ctx, logger, "delete owner tag", err)
		}
		output := &DeleteOutput{}
		output.Body.Deleted = true
		return output, nil
	})
}

func registerAnnotationEndpoints(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "get-story-annotations", Method: http.MethodGet,
		Path: "/api/v1/stories/{storyId}/annotations", Summary: "Get revision-bound story annotations",
		Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *StoryAnnotationsInput) (*AnnotationsOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		annotations, err := configuration.readingState.Annotations(ctx, input.UserID, input.StoryID)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "read story annotations", err)
		}
		output := &AnnotationsOutput{}
		output.Body.Annotations = annotations
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-story-annotation", Method: http.MethodPost,
		Path: "/api/v1/stories/{storyId}/annotations", Summary: "Create a revision-bound story annotation",
		Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *CreateAnnotationInput) (*AnnotationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		annotation, err := configuration.readingState.CreateAnnotation(ctx, input.UserID, input.StoryID, input.Body)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "create story annotation", err)
		}
		return &AnnotationOutput{Body: annotation}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-annotation", Method: http.MethodPatch,
		Path: "/api/v1/annotations/{annotationId}", Summary: "Update an owner annotation body",
		Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *UpdateAnnotationInput) (*AnnotationOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		annotation, err := configuration.readingState.UpdateAnnotation(
			ctx,
			input.UserID,
			input.AnnotationID,
			readingstate.AnnotationInput{Body: input.Body.Body},
		)
		if err != nil {
			return nil, mapReadingStateError(ctx, logger, "update story annotation", err)
		}
		return &AnnotationOutput{Body: annotation}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-annotation", Method: http.MethodDelete,
		Path: "/api/v1/annotations/{annotationId}", Summary: "Delete an owner annotation",
		Tags: []string{"reading-state"},
	}, func(ctx context.Context, input *DeleteAnnotationInput) (*DeleteOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.readingState != nil); err != nil {
			return nil, err
		}
		if err := configuration.readingState.DeleteAnnotation(ctx, input.UserID, input.AnnotationID); err != nil {
			return nil, mapReadingStateError(ctx, logger, "delete story annotation", err)
		}
		output := &DeleteOutput{}
		output.Body.Deleted = true
		return output, nil
	})
}

func mapReadingStateError(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	err error,
) error {
	switch {
	case errors.Is(err, readingstate.ErrInvalid):
		return huma.Error400BadRequest("The reading-state request is invalid.")
	case errors.Is(err, readingstate.ErrNotFound):
		return huma.Error404NotFound("The requested owner resource does not exist.")
	case errors.Is(err, readingstate.ErrUndoExpired):
		return huma.Error410Gone("The ten-second Undo window has expired.")
	case errors.Is(err, readingstate.ErrConflict),
		errors.Is(err, readingstate.ErrUndoConflict),
		errors.Is(err, readingstate.ErrUndoUnavailable):
		return huma.Error409Conflict("The resource changed. Refresh before retrying this command.")
	default:
		logger.ErrorContext(ctx, operation+" failed", "error", err)
		return huma.Error500InternalServerError("The private reading-state operation failed.")
	}
}
