package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/discovery"
)

type SearchInput struct {
	Authorization  string `header:"Authorization"`
	UserID         string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Query          string `query:"q" minLength:"2" maxLength:"500"`
	Topic          string `query:"topic" maxLength:"100"`
	SourceTier     string `query:"sourceTier" enum:"T0,T1,T2,T3"`
	LifecycleState string `query:"lifecycle" enum:"stable,preview,release_candidate,deprecated,eol,unknown"`
	RadarState     string `query:"radar" enum:"adopt,trial,assess,hold,reject"`
	Action         string `query:"action" maxLength:"120"`
	Saved          string `query:"saved" enum:"true,false"`
	After          string `query:"after" maxLength:"35"`
	Before         string `query:"before" maxLength:"35"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type SearchOutput struct {
	Body discovery.SearchResponse
}

type SavedSearchesOutput struct {
	Body struct {
		Searches []discovery.SavedSearch `json:"searches"`
	}
}

type SaveSearchInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          struct {
		Name    string                  `json:"name" minLength:"1" maxLength:"100"`
		Query   string                  `json:"query" minLength:"2" maxLength:"500"`
		Filters discovery.SearchFilters `json:"filters"`
	}
}

type SavedSearchOutput struct {
	Body discovery.SavedSearch
}

type DeleteSavedSearchInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	SavedSearchID string `path:"savedSearchId" minLength:"36" maxLength:"36"`
}

type ReleasesOutput struct {
	Body discovery.ReleaseCatalog
}

type ManualCaptureInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          struct {
		URL            string `json:"url" minLength:"9" maxLength:"4096" format:"uri"`
		IdempotencyKey string `json:"idempotencyKey" minLength:"16" maxLength:"200"`
	}
}

type ManualCaptureOutput struct {
	Body discovery.ManualCapture
}

type OPMLPreviewInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          struct {
		OPML string `json:"opml" minLength:"1" maxLength:"2097152"`
	}
}

type OPMLPreviewOutput struct {
	Body discovery.ImportPreview
}

type OPMLCommitInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          struct {
		PreviewID   string   `json:"previewId" minLength:"36" maxLength:"36"`
		ApprovedIDs []string `json:"approvedIds" minItems:"1" maxItems:"500"`
	}
}

type OPMLCommitOutput struct {
	Body discovery.ImportCommitResult
}

type MetadataExportInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Format        string `query:"format" enum:"json,csv" default:"json"`
}

type MarkdownExportInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
	Body          struct {
		StoryIDs []string `json:"storyIds" minItems:"1" maxItems:"100"`
	}
}

type ExportInput struct {
	Authorization string `header:"Authorization"`
	UserID        string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type ExportOutput struct {
	ContentDisposition string `header:"Content-Disposition"`
	ContentType        string `header:"Content-Type"`
	Body               []byte
}

func registerDiscovery(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "search-intelligence", Method: http.MethodGet, Path: "/api/v1/search",
		Summary: "Search published owner intelligence with hybrid retrieval", Tags: []string{"discovery"},
	}, func(ctx context.Context, input *SearchInput) (*SearchOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		after, err := optionalTime(input.After, false)
		if err != nil {
			return nil, huma.Error400BadRequest("The search start date must be RFC 3339 or YYYY-MM-DD.")
		}
		before, err := optionalTime(input.Before, true)
		if err != nil {
			return nil, huma.Error400BadRequest("The search end date must be RFC 3339 or YYYY-MM-DD.")
		}
		if after != nil && before != nil && !after.Before(*before) {
			return nil, huma.Error400BadRequest("The search date range must be increasing.")
		}
		saved, err := optionalBool(input.Saved)
		if err != nil {
			return nil, huma.Error400BadRequest("The saved filter must be true or false.")
		}
		result, err := configuration.discovery.Search(ctx, discovery.SearchRequest{
			UserID: input.UserID, Query: input.Query, Topic: input.Topic,
			SourceTier: input.SourceTier, LifecycleState: input.LifecycleState,
			RadarState: input.RadarState, Action: input.Action, Saved: saved,
			After: after, Before: before, Limit: input.Limit,
		})
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "search intelligence", err)
		}
		return &SearchOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-saved-searches", Method: http.MethodGet, Path: "/api/v1/searches",
		Summary: "List durable owner search queries", Tags: []string{"discovery"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*SavedSearchesOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		results, err := configuration.discovery.ListSavedSearches(ctx, input.UserID)
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "list saved searches", err)
		}
		output := &SavedSearchesOutput{}
		output.Body.Searches = results
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-search", Method: http.MethodPost, Path: "/api/v1/searches",
		Summary: "Create or update a durable owner search query", Tags: []string{"discovery"},
	}, func(ctx context.Context, input *SaveSearchInput) (*SavedSearchOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.SaveSearch(ctx, discovery.SaveSearchRequest{
			UserID: input.UserID, Name: input.Body.Name, Query: input.Body.Query, Filters: input.Body.Filters,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "save search", err)
		}
		return &SavedSearchOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-saved-search", Method: http.MethodDelete,
		Path: "/api/v1/searches/{savedSearchId}", Summary: "Delete an owner saved search",
		Tags: []string{"discovery"},
	}, func(ctx context.Context, input *DeleteSavedSearchInput) (*DeleteOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		if err := configuration.discovery.DeleteSavedSearch(ctx, input.UserID, input.SavedSearchID); err != nil {
			return nil, mapDiscoveryError(ctx, logger, "delete saved search", err)
		}
		output := &DeleteOutput{}
		output.Body.Deleted = true
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-releases", Method: http.MethodGet, Path: "/api/v1/releases",
		Summary: "Get the verified release and coming-soon matrix", Tags: []string{"discovery"},
	}, func(ctx context.Context, input *OwnerInternalInput) (*ReleasesOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.Releases(ctx, input.UserID, configuration.clock().UTC())
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "read releases", err)
		}
		return &ReleasesOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "import-inbox-url", Method: http.MethodPost, Path: "/api/v1/inbox/import-url",
		Summary: "Queue a URL for the bounded source ingestion pipeline", Tags: []string{"portability"},
	}, func(ctx context.Context, input *ManualCaptureInput) (*ManualCaptureOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.ImportURL(ctx, discovery.ManualCaptureRequest{
			UserID: input.UserID, URL: input.Body.URL, IdempotencyKey: input.Body.IdempotencyKey,
		})
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "import URL", err)
		}
		return &ManualCaptureOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "preview-opml-import", Method: http.MethodPost, Path: "/api/v1/imports/opml/preview",
		Summary: "Preview and validate an OPML source import", Tags: []string{"portability"},
	}, func(ctx context.Context, input *OPMLPreviewInput) (*OPMLPreviewOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.PreviewOPML(ctx, input.UserID, []byte(input.Body.OPML), configuration.clock().UTC())
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "preview OPML", err)
		}
		return &OPMLPreviewOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "commit-opml-import", Method: http.MethodPost, Path: "/api/v1/imports/opml/commit",
		Summary: "Create approved OPML entries as disabled pending sources", Tags: []string{"portability"},
	}, func(ctx context.Context, input *OPMLCommitInput) (*OPMLCommitOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.CommitOPML(ctx, discovery.ImportCommitRequest{
			UserID: input.UserID, PreviewID: input.Body.PreviewID, ApprovedIDs: input.Body.ApprovedIDs,
		}, configuration.clock().UTC())
		if err != nil {
			return nil, mapDiscoveryError(ctx, logger, "commit OPML", err)
		}
		return &OPMLCommitOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-opml", Method: http.MethodGet, Path: "/api/v1/exports/opml",
		Summary: "Export owner-added sources as OPML", Tags: []string{"portability"},
	}, func(ctx context.Context, input *ExportInput) (*ExportOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.ExportOPML(ctx, input.UserID)
		return exportOutput(ctx, logger, "export OPML", result, err)
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-metadata", Method: http.MethodGet, Path: "/api/v1/exports/metadata",
		Summary: "Export bounded story metadata as JSON or CSV", Tags: []string{"portability"},
	}, func(ctx context.Context, input *MetadataExportInput) (*ExportOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.ExportMetadata(ctx, discovery.MetadataExportRequest{UserID: input.UserID, Format: input.Format})
		return exportOutput(ctx, logger, "export metadata", result, err)
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-markdown", Method: http.MethodPost, Path: "/api/v1/exports/markdown",
		Summary: "Export selected briefs, annotations, and citations as Markdown", Tags: []string{"portability"},
	}, func(ctx context.Context, input *MarkdownExportInput) (*ExportOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.discovery != nil); err != nil {
			return nil, err
		}
		result, err := configuration.discovery.ExportMarkdown(ctx, discovery.MarkdownExportRequest{UserID: input.UserID, StoryIDs: input.Body.StoryIDs})
		return exportOutput(ctx, logger, "export Markdown", result, err)
	})
}

func optionalTime(value string, endOfDay bool) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse(time.DateOnly, value)
		if err != nil {
			return nil, err
		}
		if endOfDay {
			parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		}
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func optionalBool(value string) (*bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, nil
	case "true":
		result := true
		return &result, nil
	case "false":
		result := false
		return &result, nil
	default:
		return nil, errors.New("invalid boolean")
	}
}

func exportOutput(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	result discovery.Export,
	err error,
) (*ExportOutput, error) {
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, operation, err)
	}
	return &ExportOutput{
		ContentDisposition: fmt.Sprintf("attachment; filename=%q", result.Filename),
		ContentType:        result.ContentType, Body: result.Payload,
	}, nil
}

func mapDiscoveryError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	switch {
	case errors.Is(err, discovery.ErrInvalid):
		return huma.Error400BadRequest("The discovery request is invalid.")
	case errors.Is(err, discovery.ErrNotFound):
		return huma.Error404NotFound("The requested discovery resource was not found.")
	case errors.Is(err, discovery.ErrConflict):
		return huma.Error409Conflict("The discovery resource changed or expired; refresh and try again.")
	default:
		logger.ErrorContext(ctx, operation+" failed", "error", err)
		return huma.Error500InternalServerError("The private discovery service is unavailable.")
	}
}
