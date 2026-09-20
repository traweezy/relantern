package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/traweezy/relantern/internal/intelligence"
)

type AlertHistoryInput struct {
	Authorization string `header:"Authorization"`
	// An explicit empty cursor selects the same first page as an omitted cursor.
	Cursor string `query:"cursor" maxLength:"512"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	UserID string `header:"X-Relantern-User-ID" minLength:"36" maxLength:"36"`
}

type AlertHistoryOutput struct {
	CacheControl string `header:"Cache-Control"`
	Link         string `header:"Link"`
	Body         intelligence.AlertHistoryPage
}

func registerAlertHistory(api huma.API, configuration options, logger *slog.Logger) {
	huma.Register(api, huma.Operation{
		OperationID: "get-alert-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/alerts",
		Summary:     "List confirmed owner advisory alerts with delivery status",
		Tags:        []string{"intelligence"},
	}, func(ctx context.Context, input *AlertHistoryInput) (*AlertHistoryOutput, error) {
		if err := authorizeInternal(input.Authorization, configuration, configuration.intelligence != nil); err != nil {
			return nil, err
		}
		if strings.TrimSpace(input.UserID) == "" {
			return nil, huma.Error400BadRequest("The owner ID is required.")
		}
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		page, err := configuration.intelligence.AlertHistory(ctx, input.UserID, input.Cursor, limit)
		if errors.Is(err, intelligence.ErrInvalidAlertHistory) {
			return nil, huma.Error400BadRequest("The alert history cursor or owner ID is invalid.")
		}
		if err != nil {
			logger.ErrorContext(ctx, "owner alert history read failed", "error", err)
			return nil, huma.Error500InternalServerError("The private alert history is unavailable.")
		}
		output := &AlertHistoryOutput{Body: page, CacheControl: "private, no-store"}
		if page.NextCursor != nil {
			output.Link = fmt.Sprintf("</api/v1/alerts?cursor=%s&limit=%d>; rel=\"next\"",
				url.QueryEscape(*page.NextCursor), limit)
		}
		return output, nil
	})
}
