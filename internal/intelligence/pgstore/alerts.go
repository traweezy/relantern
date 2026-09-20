package pgstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/traweezy/relantern/internal/intelligence"
)

const (
	defaultAlertHistoryLimit = 20
	maximumAlertHistoryLimit = 100
	maximumAlertCursorLength = 512
)

type alertCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

func (store *Store) AlertHistory(
	ctx context.Context,
	userID string,
	cursorValue string,
	limit int,
) (intelligence.AlertHistoryPage, error) {
	if parsed, err := uuid.Parse(userID); err != nil || parsed.String() != userID {
		return intelligence.AlertHistoryPage{}, fmt.Errorf("%w: owner ID is invalid", intelligence.ErrInvalidAlertHistory)
	}
	if limit == 0 {
		limit = defaultAlertHistoryLimit
	}
	if limit < 1 || limit > maximumAlertHistoryLimit {
		return intelligence.AlertHistoryPage{}, fmt.Errorf("%w: limit must be 1 through 100", intelligence.ErrInvalidAlertHistory)
	}

	const projection = `
		select id::text, advisory_id, title, ecosystem, package_name,
			current_version, vulnerable_range, patched_version, source_url,
			observed_at, created_at, correction_reason, corrected_at
		from app.critical_alerts
		where user_id = $1::uuid`
	const order = ` order by created_at desc, id desc`
	query := projection + order + ` limit $2`
	arguments := []any{userID, limit + 1}
	if cursorValue != "" {
		cursor, createdAt, err := decodeAlertCursor(cursorValue)
		if err != nil {
			return intelligence.AlertHistoryPage{}, err
		}
		query = projection + ` and (created_at, id) < ($2::timestamptz, $3::uuid)` + order + ` limit $4`
		arguments = []any{userID, createdAt, cursor.ID, limit + 1}
	}
	rows, err := store.pool.Query(ctx, query, arguments...)
	if err != nil {
		return intelligence.AlertHistoryPage{}, fmt.Errorf("select owner alert history: %w", err)
	}
	items := make([]intelligence.AlertHistoryItem, 0, limit+1)
	for rows.Next() {
		var item intelligence.AlertHistoryItem
		var correctionReason pgtype.Text
		var correctedAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID, &item.AdvisoryID, &item.Title, &item.Ecosystem,
			&item.PackageName, &item.CurrentVersion, &item.VersionRange,
			&item.PatchedVersion, &item.SourceURL, &item.ObservedAt, &item.AlertedAt,
			&correctionReason, &correctedAt,
		); err != nil {
			rows.Close()
			return intelligence.AlertHistoryPage{}, fmt.Errorf("scan owner alert history: %w", err)
		}
		item.ObservedAt = item.ObservedAt.UTC()
		item.AlertedAt = item.AlertedAt.UTC()
		if correctionReason.Valid {
			reason := correctionReason.String
			item.CorrectionReason = &reason
		}
		if correctedAt.Valid {
			at := correctedAt.Time.UTC()
			item.CorrectedAt = &at
		}
		item.Reason = "Confirmed critical advisory affects a watched dependency."
		item.Deliveries = []intelligence.AlertDeliveryStatus{}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return intelligence.AlertHistoryPage{}, fmt.Errorf("iterate owner alert history: %w", err)
	}
	rows.Close()

	page := intelligence.AlertHistoryPage{Alerts: items}
	if len(items) > limit {
		page.Alerts = items[:limit]
		last := page.Alerts[len(page.Alerts)-1]
		nextCursor, err := encodeAlertCursor(last.AlertedAt, last.ID)
		if err != nil {
			return intelligence.AlertHistoryPage{}, err
		}
		page.NextCursor = &nextCursor
	}
	if len(page.Alerts) == 0 {
		return page, nil
	}
	ids := make([]string, 0, len(page.Alerts))
	itemByID := make(map[string]int, len(page.Alerts))
	for index, item := range page.Alerts {
		ids = append(ids, item.ID)
		itemByID[item.ID] = index
	}
	deliveryRows, err := store.pool.Query(ctx, `
		select delivery.alert_id::text, delivery.channel, delivery.state,
			delivery.attempt_count, delivery.delivered_at, delivery.next_attempt_at
		from app.critical_alert_deliveries delivery
		join app.critical_alerts alert on alert.id = delivery.alert_id
		where alert.user_id = $1::uuid and delivery.alert_id = any($2::uuid[])
		order by delivery.alert_id, delivery.channel`, userID, ids)
	if err != nil {
		return intelligence.AlertHistoryPage{}, fmt.Errorf("select owner alert delivery states: %w", err)
	}
	defer deliveryRows.Close()
	for deliveryRows.Next() {
		var alertID string
		var delivery intelligence.AlertDeliveryStatus
		var deliveredAt pgtype.Timestamptz
		var nextAttemptAt time.Time
		if err := deliveryRows.Scan(
			&alertID, &delivery.Channel, &delivery.State, &delivery.AttemptCount,
			&deliveredAt, &nextAttemptAt,
		); err != nil {
			return intelligence.AlertHistoryPage{}, fmt.Errorf("scan owner alert delivery state: %w", err)
		}
		if deliveredAt.Valid {
			at := deliveredAt.Time.UTC()
			delivery.DeliveredAt = &at
		}
		if delivery.State == "pending" || delivery.State == "failed" {
			at := nextAttemptAt.UTC()
			delivery.NextAttemptAt = &at
		}
		index, found := itemByID[alertID]
		if !found {
			return intelligence.AlertHistoryPage{}, errors.New("owner alert delivery has no matching page item")
		}
		page.Alerts[index].Deliveries = append(page.Alerts[index].Deliveries, delivery)
	}
	if err := deliveryRows.Err(); err != nil {
		return intelligence.AlertHistoryPage{}, fmt.Errorf("iterate owner alert delivery states: %w", err)
	}
	return page, nil
}

func encodeAlertCursor(createdAt time.Time, id string) (string, error) {
	encoded, err := json.Marshal(alertCursor{
		Version: 1, CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: id,
	})
	if err != nil {
		return "", fmt.Errorf("encode owner alert cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeAlertCursor(value string) (alertCursor, time.Time, error) {
	invalid := func() (alertCursor, time.Time, error) {
		return alertCursor{}, time.Time{}, fmt.Errorf("%w: cursor is invalid", intelligence.ErrInvalidAlertHistory)
	}
	if len(value) == 0 || len(value) > maximumAlertCursorLength {
		return invalid()
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return invalid()
	}
	parser := json.NewDecoder(bytes.NewReader(decoded))
	parser.DisallowUnknownFields()
	var cursor alertCursor
	if err := parser.Decode(&cursor); err != nil {
		return invalid()
	}
	if err := parser.Decode(new(any)); !errors.Is(err, io.EOF) {
		return invalid()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil || cursor.Version != 1 || createdAt.IsZero() ||
		createdAt.UTC().Format(time.RFC3339Nano) != cursor.CreatedAt {
		return invalid()
	}
	id, err := uuid.Parse(cursor.ID)
	if err != nil || id.String() != cursor.ID {
		return invalid()
	}
	return cursor, createdAt, nil
}
