package pgstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/traweezy/relantern/internal/readingstate"
)

const collectionProjection = `
	select
		story.story_id::text,
		story.headline,
		story.summary,
		story.why_it_matters,
		story.recommended_action,
		story.confidence,
		story.first_seen_at,
		story.last_changed_at,
		story.status,
		story.signal,
		story.source_tier,
		story.source_count,
		story.read_time_minutes,
		story.primary_source_url,
		coalesce(state.location, 'inbox'),
		coalesce(state.is_read, false),
		state.read_at,
		state.starred_at,
		state.snoozed_until,
		state.snoozed_from_location,
		coalesce(state.reading_progress, 0)::float8,
		state.last_paragraph_id,
		state.later_position,
		state.dismissed_reason,
		coalesce(state.version, 0),
		coalesce(state.updated_at, story.first_seen_at),
		coalesce(array(
			select item_tag.tag_id::text
			from app.item_tags item_tag
			where item_tag.user_id = $1::uuid and item_tag.item_id = story.item_id
			order by item_tag.tag_id
		), array[]::text[])
	from app.v_story_summaries story
	left join app.user_item_states state
		on state.user_id = $1::uuid and state.item_id = story.item_id`

type collectionCursor struct {
	Kind      readingstate.CollectionKind `json:"kind"`
	Position  *int64                      `json:"position,omitempty"`
	StoryID   string                      `json:"storyId"`
	Timestamp *time.Time                  `json:"timestamp,omitempty"`
}

func (store *Store) Collection(
	ctx context.Context,
	query readingstate.CollectionQuery,
) (readingstate.Collection, error) {
	if err := validateIdentifier(query.UserID, "user ID"); err != nil {
		return readingstate.Collection{}, err
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 1 || query.Limit > 100 {
		return readingstate.Collection{}, fmt.Errorf("%w: collection limit must be 1 through 100", readingstate.ErrInvalid)
	}
	if query.Now.IsZero() {
		query.Now = store.clock().UTC()
	} else {
		query.Now = query.Now.UTC()
	}
	filter, order, cursorCondition, err := collectionSQL(query.Kind)
	if err != nil {
		return readingstate.Collection{}, err
	}
	var total int
	if err := store.pool.QueryRow(ctx, `
		select count(*)
		from app.v_story_summaries story
		left join app.user_item_states state
			on state.user_id = $1::uuid and state.item_id = story.item_id
		where `+filter, query.UserID, query.Now).Scan(&total); err != nil {
		return readingstate.Collection{}, fmt.Errorf("count reading collection: %w", err)
	}

	arguments := []any{query.UserID, query.Now, query.Limit + 1}
	cursorSQL := ""
	if query.Cursor != "" {
		cursor, decodeErr := decodeCollectionCursor(query.Cursor, query.Kind)
		if decodeErr != nil {
			return readingstate.Collection{}, decodeErr
		}
		cursorSQL = " and " + cursorCondition
		switch query.Kind {
		case readingstate.CollectionLater:
			arguments = append(arguments, *cursor.Position, cursor.StoryID)
		default:
			arguments = append(arguments, *cursor.Timestamp, cursor.StoryID)
		}
	}
	rows, err := store.pool.Query(ctx, collectionProjection+`
		where `+filter+cursorSQL+`
		order by `+order+`
		limit $3`, arguments...)
	if err != nil {
		return readingstate.Collection{}, fmt.Errorf("select reading collection: %w", err)
	}
	defer rows.Close()
	items := make([]readingstate.StoryListItem, 0, query.Limit+1)
	for rows.Next() {
		item, scanErr := scanCollectionItem(rows)
		if scanErr != nil {
			return readingstate.Collection{}, fmt.Errorf("scan reading collection item: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return readingstate.Collection{}, fmt.Errorf("iterate reading collection: %w", err)
	}

	result := readingstate.Collection{Items: items, Total: total}
	if len(items) > query.Limit {
		result.Items = items[:query.Limit]
		cursor, encodeErr := encodeCollectionCursor(query.Kind, result.Items[len(result.Items)-1])
		if encodeErr != nil {
			return readingstate.Collection{}, encodeErr
		}
		result.NextCursor = &cursor
	}
	return result, nil
}

func (store *Store) States(
	ctx context.Context,
	userID string,
	storyIDs []string,
) (readingstate.StoryStates, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.StoryStates{}, err
	}
	if len(storyIDs) == 0 || len(storyIDs) > 100 {
		return readingstate.StoryStates{}, fmt.Errorf(
			"%w: state query must contain 1 through 100 stories",
			readingstate.ErrInvalid,
		)
	}
	seen := make(map[string]struct{}, len(storyIDs))
	for _, storyID := range storyIDs {
		if err := validateIdentifier(storyID, "story ID"); err != nil {
			return readingstate.StoryStates{}, err
		}
		if _, duplicate := seen[storyID]; duplicate {
			return readingstate.StoryStates{}, fmt.Errorf("%w: duplicate story in state query", readingstate.ErrInvalid)
		}
		seen[storyID] = struct{}{}
	}
	rows, err := store.pool.Query(ctx, `
		select
			cluster.id::text,
			coalesce(state.location, 'inbox'),
			coalesce(state.is_read, false),
			state.read_at,
			state.starred_at,
			state.snoozed_until,
			state.snoozed_from_location,
			coalesce(state.reading_progress, 0)::float8,
			state.last_paragraph_id,
			state.later_position,
			state.dismissed_reason,
			coalesce(state.version, 0),
			coalesce(state.updated_at, item.first_seen_at),
			coalesce(array(
				select item_tag.tag_id::text
				from app.item_tags item_tag
				where item_tag.user_id = $1::uuid and item_tag.item_id = item.id
				order by item_tag.tag_id
			), array[]::text[])
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		left join app.user_item_states state
			on state.user_id = $1::uuid and state.item_id = item.id
		where cluster.id = any($2::uuid[])
		order by array_position($2::uuid[], cluster.id)`, userID, storyIDs)
	if err != nil {
		return readingstate.StoryStates{}, fmt.Errorf("select story states: %w", err)
	}
	defer rows.Close()
	states := make([]readingstate.StoryState, 0, len(storyIDs))
	for rows.Next() {
		state, scanErr := scanProjectedState(rows)
		if scanErr != nil {
			return readingstate.StoryStates{}, fmt.Errorf("scan story state: %w", scanErr)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return readingstate.StoryStates{}, fmt.Errorf("iterate story states: %w", err)
	}
	if len(states) != len(storyIDs) {
		return readingstate.StoryStates{}, readingstate.ErrNotFound
	}
	return readingstate.StoryStates{States: states}, nil
}

func scanProjectedState(row rowScanner) (readingstate.StoryState, error) {
	state := readingstate.StoryState{}
	var readAt pgtype.Timestamptz
	var starredAt pgtype.Timestamptz
	var snoozedUntil pgtype.Timestamptz
	var snoozedFrom pgtype.Text
	var lastParagraphID pgtype.Text
	var laterPosition pgtype.Int8
	var dismissedReason pgtype.Text
	if err := row.Scan(
		&state.StoryID,
		&state.Location,
		&state.IsRead,
		&readAt,
		&starredAt,
		&snoozedUntil,
		&snoozedFrom,
		&state.ReadingProgress,
		&lastParagraphID,
		&laterPosition,
		&dismissedReason,
		&state.Version,
		&state.UpdatedAt,
		&state.TagIDs,
	); err != nil {
		return readingstate.StoryState{}, err
	}
	state.ReadAt = timestampPointer(readAt)
	state.StarredAt = timestampPointer(starredAt)
	state.SnoozedUntil = timestampPointer(snoozedUntil)
	if snoozedFrom.Valid {
		location := readingstate.Location(snoozedFrom.String)
		state.SnoozedFromLocation = &location
	}
	state.LastParagraphID = textPointer(lastParagraphID)
	if laterPosition.Valid {
		position := laterPosition.Int64
		state.LaterPosition = &position
	}
	state.DismissedReason = textPointer(dismissedReason)
	state.UpdatedAt = state.UpdatedAt.UTC()
	if state.TagIDs == nil {
		state.TagIDs = []string{}
	}
	return state, nil
}

func collectionSQL(
	kind readingstate.CollectionKind,
) (string, string, string, error) {
	switch kind {
	case readingstate.CollectionInbox:
		return `coalesce(state.location, 'inbox') = 'inbox'
			and state.snoozed_until is null
			and $2::timestamptz is not null`,
			"story.last_changed_at desc, story.story_id desc",
			"(story.last_changed_at, story.story_id) < ($4::timestamptz, $5::uuid)", nil
	case readingstate.CollectionLater:
		return `state.location = 'later'
			and state.snoozed_until is null
			and $2::timestamptz is not null`,
			"coalesce(state.later_position, 9223372036854775807), story.story_id",
			"(coalesce(state.later_position, 9223372036854775807), story.story_id) > ($4::bigint, $5::uuid)", nil
	case readingstate.CollectionStarred:
		return "state.starred_at is not null",
			"story.last_changed_at desc, story.story_id desc",
			"(story.last_changed_at, story.story_id) < ($4::timestamptz, $5::uuid)", nil
	case readingstate.CollectionArchive:
		return "state.location = 'archive'",
			"story.last_changed_at desc, story.story_id desc",
			"(story.last_changed_at, story.story_id) < ($4::timestamptz, $5::uuid)", nil
	case readingstate.CollectionSnoozed:
		return "state.snoozed_until > $2",
			"state.snoozed_until, story.story_id",
			"(state.snoozed_until, story.story_id) > ($4::timestamptz, $5::uuid)", nil
	default:
		return "", "", "", fmt.Errorf("%w: unsupported reading collection", readingstate.ErrInvalid)
	}
}

func scanCollectionItem(row rowScanner) (readingstate.StoryListItem, error) {
	item := readingstate.StoryListItem{}
	var readAt pgtype.Timestamptz
	var starredAt pgtype.Timestamptz
	var snoozedUntil pgtype.Timestamptz
	var snoozedFrom pgtype.Text
	var lastParagraphID pgtype.Text
	var laterPosition pgtype.Int8
	var dismissedReason pgtype.Text
	err := row.Scan(
		&item.Story.ID,
		&item.Story.Headline,
		&item.Story.Summary,
		&item.Story.WhyItMatters,
		&item.Story.RecommendedAction,
		&item.Story.Confidence,
		&item.Story.FirstSeenAt,
		&item.Story.LastChangedAt,
		&item.Story.Status,
		&item.Story.Signal,
		&item.Story.SourceTier,
		&item.Story.SourceCount,
		&item.Story.ReadTimeMinutes,
		&item.Story.PrimarySourceURL,
		&item.State.Location,
		&item.State.IsRead,
		&readAt,
		&starredAt,
		&snoozedUntil,
		&snoozedFrom,
		&item.State.ReadingProgress,
		&lastParagraphID,
		&laterPosition,
		&dismissedReason,
		&item.State.Version,
		&item.State.UpdatedAt,
		&item.State.TagIDs,
	)
	if err != nil {
		return readingstate.StoryListItem{}, err
	}
	item.Story.FirstSeenAt = item.Story.FirstSeenAt.UTC()
	item.Story.LastChangedAt = item.Story.LastChangedAt.UTC()
	item.State.StoryID = item.Story.ID
	item.State.ReadAt = timestampPointer(readAt)
	item.State.StarredAt = timestampPointer(starredAt)
	item.State.SnoozedUntil = timestampPointer(snoozedUntil)
	if snoozedFrom.Valid {
		location := readingstate.Location(snoozedFrom.String)
		item.State.SnoozedFromLocation = &location
	}
	item.State.LastParagraphID = textPointer(lastParagraphID)
	if laterPosition.Valid {
		position := laterPosition.Int64
		item.State.LaterPosition = &position
	}
	item.State.DismissedReason = textPointer(dismissedReason)
	item.State.UpdatedAt = item.State.UpdatedAt.UTC()
	if item.State.TagIDs == nil {
		item.State.TagIDs = []string{}
	}
	return item, nil
}

func encodeCollectionCursor(
	kind readingstate.CollectionKind,
	item readingstate.StoryListItem,
) (string, error) {
	cursor := collectionCursor{Kind: kind, StoryID: item.Story.ID}
	switch kind {
	case readingstate.CollectionLater:
		position := int64(math.MaxInt64)
		if item.State.LaterPosition != nil {
			position = *item.State.LaterPosition
		}
		cursor.Position = &position
	case readingstate.CollectionSnoozed:
		cursor.Timestamp = item.State.SnoozedUntil
	default:
		timestamp := item.Story.LastChangedAt.UTC()
		cursor.Timestamp = &timestamp
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode collection cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCollectionCursor(
	value string,
	kind readingstate.CollectionKind,
) (collectionCursor, error) {
	if len(value) > 512 {
		return collectionCursor{}, fmt.Errorf("%w: collection cursor is too large", readingstate.ErrInvalid)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return collectionCursor{}, fmt.Errorf("%w: collection cursor is malformed", readingstate.ErrInvalid)
	}
	cursor := collectionCursor{}
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return collectionCursor{}, fmt.Errorf("%w: collection cursor is malformed", readingstate.ErrInvalid)
	}
	if cursor.Kind != kind {
		return collectionCursor{}, fmt.Errorf("%w: collection cursor belongs to another view", readingstate.ErrInvalid)
	}
	if err := validateIdentifier(cursor.StoryID, "cursor story ID"); err != nil {
		return collectionCursor{}, err
	}
	if kind == readingstate.CollectionLater && cursor.Position == nil {
		return collectionCursor{}, fmt.Errorf("%w: Later cursor is incomplete", readingstate.ErrInvalid)
	}
	if kind != readingstate.CollectionLater && cursor.Timestamp == nil {
		return collectionCursor{}, fmt.Errorf("%w: collection cursor is incomplete", readingstate.ErrInvalid)
	}
	return cursor, nil
}
