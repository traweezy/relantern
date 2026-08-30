package pgstore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/traweezy/relantern/internal/readingstate"
)

var tagColors = map[string]struct{}{
	"accent": {},
	"blue":   {},
	"green":  {},
	"orange": {},
	"purple": {},
	"red":    {},
	"slate":  {},
}

func (store *Store) Tags(ctx context.Context, userID string) ([]readingstate.Tag, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		select id::text, name, color_token, created_at, updated_at
		from app.tags where user_id = $1::uuid
		order by normalized_name, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("select owner tags: %w", err)
	}
	defer rows.Close()
	tags := make([]readingstate.Tag, 0)
	for rows.Next() {
		tag := readingstate.Tag{}
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.ColorToken, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan owner tag: %w", err)
		}
		tag.CreatedAt = tag.CreatedAt.UTC()
		tag.UpdatedAt = tag.UpdatedAt.UTC()
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owner tags: %w", err)
	}
	return tags, nil
}

func (store *Store) CreateTag(
	ctx context.Context,
	userID string,
	input readingstate.TagInput,
) (readingstate.Tag, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.Tag{}, err
	}
	name, normalized, color, err := normalizeTag(input)
	if err != nil {
		return readingstate.Tag{}, err
	}
	now := store.clock().UTC()
	tag := readingstate.Tag{Name: name, ColorToken: color, CreatedAt: now, UpdatedAt: now}
	err = store.pool.QueryRow(ctx, `
		insert into app.tags (
			user_id, name, normalized_name, color_token, created_at, updated_at
		) values ($1::uuid, $2, $3, $4, $5, $5)
		returning id::text`, userID, name, normalized, color, now).Scan(&tag.ID)
	if err != nil {
		return readingstate.Tag{}, mapTagWriteError("create owner tag", err)
	}
	return tag, nil
}

func (store *Store) UpdateTag(
	ctx context.Context,
	userID string,
	tagID string,
	input readingstate.TagInput,
) (readingstate.Tag, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.Tag{}, err
	}
	if err := validateIdentifier(tagID, "tag ID"); err != nil {
		return readingstate.Tag{}, err
	}
	name, normalized, color, err := normalizeTag(input)
	if err != nil {
		return readingstate.Tag{}, err
	}
	now := store.clock().UTC()
	tag := readingstate.Tag{ID: tagID, Name: name, ColorToken: color, UpdatedAt: now}
	err = store.pool.QueryRow(ctx, `
		update app.tags set
			name = $3, normalized_name = $4, color_token = $5, updated_at = $6
		where id = $1::uuid and user_id = $2::uuid
		returning created_at`, tagID, userID, name, normalized, color, now).Scan(&tag.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return readingstate.Tag{}, readingstate.ErrNotFound
	}
	if err != nil {
		return readingstate.Tag{}, mapTagWriteError("update owner tag", err)
	}
	tag.CreatedAt = tag.CreatedAt.UTC()
	return tag, nil
}

func (store *Store) DeleteTag(ctx context.Context, userID string, tagID string) error {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return err
	}
	if err := validateIdentifier(tagID, "tag ID"); err != nil {
		return err
	}
	result, err := store.pool.Exec(ctx, `
		delete from app.tags where id = $1::uuid and user_id = $2::uuid`, tagID, userID)
	if err != nil {
		return fmt.Errorf("delete owner tag: %w", err)
	}
	if result.RowsAffected() != 1 {
		return readingstate.ErrNotFound
	}
	return nil
}

func normalizeTag(input readingstate.TagInput) (string, string, string, error) {
	name := strings.Join(strings.Fields(input.Name), " ")
	if len(name) < 1 || len(name) > 80 {
		return "", "", "", fmt.Errorf("%w: tag name must contain 1 through 80 characters", readingstate.ErrInvalid)
	}
	normalized := strings.ToLower(name)
	color := strings.TrimSpace(input.ColorToken)
	if _, allowed := tagColors[color]; !allowed {
		return "", "", "", fmt.Errorf("%w: unsupported tag color token", readingstate.ErrInvalid)
	}
	return name, normalized, color, nil
}

func mapTagWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("%w: a tag with that name already exists", readingstate.ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (store *Store) Annotations(
	ctx context.Context,
	userID string,
	storyID string,
) ([]readingstate.Annotation, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return nil, err
	}
	if err := validateIdentifier(storyID, "story ID"); err != nil {
		return nil, err
	}
	itemID, currentRevisionID, err := resolveTarget(ctx, store.pool, userID, storyID)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		select id::text, revision_id::text, annotation_type, start_offset,
			end_offset, quote_hash, body, created_at, updated_at
		from app.annotations
		where user_id = $1::uuid and item_id = $2::uuid
		order by created_at, id`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("select story annotations: %w", err)
	}
	defer rows.Close()
	annotations := make([]readingstate.Annotation, 0)
	for rows.Next() {
		annotation, scanErr := scanAnnotation(rows, storyID, currentRevisionID)
		if scanErr != nil {
			return nil, fmt.Errorf("scan story annotation: %w", scanErr)
		}
		annotations = append(annotations, annotation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate story annotations: %w", err)
	}
	return annotations, nil
}

func (store *Store) CreateAnnotation(
	ctx context.Context,
	userID string,
	storyID string,
	input readingstate.AnnotationInput,
) (readingstate.Annotation, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.Annotation{}, err
	}
	if err := validateIdentifier(storyID, "story ID"); err != nil {
		return readingstate.Annotation{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.Annotation{}, fmt.Errorf("begin annotation creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	itemID, currentRevisionID, err := resolveTarget(ctx, tx, userID, storyID)
	if err != nil {
		return readingstate.Annotation{}, err
	}
	normalized, quoteHash, err := normalizeAnnotation(input, currentRevisionID, true)
	if err != nil {
		return readingstate.Annotation{}, err
	}
	if quoteHash != nil {
		var normalizedContent string
		if err := tx.QueryRow(ctx, `
			select normalized_content from app.search_documents
			where item_id = $1::uuid and revision_id = $2::uuid`, itemID, currentRevisionID).Scan(&normalizedContent); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return readingstate.Annotation{}, fmt.Errorf("%w: normalized source content is unavailable", readingstate.ErrConflict)
			}
			return readingstate.Annotation{}, fmt.Errorf("load normalized source for highlight: %w", err)
		}
		contentRunes := []rune(normalizedContent)
		if *normalized.EndOffset > len(contentRunes) {
			return readingstate.Annotation{}, fmt.Errorf("%w: highlight offsets exceed the current revision", readingstate.ErrInvalid)
		}
		selectedHash := sha256.Sum256([]byte(string(contentRunes[*normalized.StartOffset:*normalized.EndOffset])))
		if subtle.ConstantTimeCompare(selectedHash[:], quoteHash) != 1 {
			return readingstate.Annotation{}, fmt.Errorf("%w: highlight quote does not match the current revision", readingstate.ErrConflict)
		}
	}
	now := store.clock().UTC()
	annotation := readingstate.Annotation{
		Body: normalized.Body, CreatedAt: now, EndOffset: normalized.EndOffset,
		Orphaned: false, QuoteHash: normalized.QuoteHash, RevisionID: currentRevisionID,
		StartOffset: normalized.StartOffset, StoryID: storyID, Type: normalized.Type,
		UpdatedAt: now,
	}
	err = tx.QueryRow(ctx, `
		insert into app.annotations (
			user_id, item_id, revision_id, annotation_type, start_offset,
			end_offset, quote_hash, body, created_at, updated_at
		) values ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $9)
		returning id::text`,
		userID,
		itemID,
		currentRevisionID,
		normalized.Type,
		normalized.StartOffset,
		normalized.EndOffset,
		quoteHash,
		normalized.Body,
		now,
	).Scan(&annotation.ID)
	if err != nil {
		return readingstate.Annotation{}, fmt.Errorf("create story annotation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.Annotation{}, fmt.Errorf("commit annotation creation: %w", err)
	}
	return annotation, nil
}

func (store *Store) UpdateAnnotation(
	ctx context.Context,
	userID string,
	annotationID string,
	input readingstate.AnnotationInput,
) (readingstate.Annotation, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.Annotation{}, err
	}
	if err := validateIdentifier(annotationID, "annotation ID"); err != nil {
		return readingstate.Annotation{}, err
	}
	if len(strings.TrimSpace(input.Body)) == 0 || len(input.Body) > 20_000 {
		return readingstate.Annotation{}, fmt.Errorf("%w: annotation body must contain 1 through 20000 characters", readingstate.ErrInvalid)
	}
	now := store.clock().UTC()
	annotation := readingstate.Annotation{ID: annotationID, Body: strings.TrimSpace(input.Body), UpdatedAt: now}
	var startOffset pgtype.Int4
	var endOffset pgtype.Int4
	var quoteHash []byte
	var currentRevisionID string
	err := store.pool.QueryRow(ctx, `
		update app.annotations annotation set body = $3, updated_at = $4
		from app.items item, app.story_clusters cluster
		where annotation.id = $1::uuid
			and annotation.user_id = $2::uuid
			and item.id = annotation.item_id
			and cluster.primary_item_id = item.id
		returning annotation.revision_id::text, annotation.annotation_type,
			annotation.start_offset, annotation.end_offset, annotation.quote_hash,
			annotation.created_at, cluster.id::text, item.current_revision_id::text`,
		annotationID,
		userID,
		annotation.Body,
		now,
	).Scan(
		&annotation.RevisionID,
		&annotation.Type,
		&startOffset,
		&endOffset,
		&quoteHash,
		&annotation.CreatedAt,
		&annotation.StoryID,
		&currentRevisionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return readingstate.Annotation{}, readingstate.ErrNotFound
	}
	if err != nil {
		return readingstate.Annotation{}, fmt.Errorf("update story annotation: %w", err)
	}
	annotation.StartOffset = intPointer(startOffset)
	annotation.EndOffset = intPointer(endOffset)
	annotation.QuoteHash = encodedHashPointer(quoteHash)
	annotation.CreatedAt = annotation.CreatedAt.UTC()
	annotation.Orphaned = annotation.RevisionID != currentRevisionID
	return annotation, nil
}

func (store *Store) DeleteAnnotation(
	ctx context.Context,
	userID string,
	annotationID string,
) error {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return err
	}
	if err := validateIdentifier(annotationID, "annotation ID"); err != nil {
		return err
	}
	result, err := store.pool.Exec(ctx, `
		delete from app.annotations where id = $1::uuid and user_id = $2::uuid`,
		annotationID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("delete story annotation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return readingstate.ErrNotFound
	}
	return nil
}

func normalizeAnnotation(
	input readingstate.AnnotationInput,
	currentRevisionID string,
	create bool,
) (readingstate.AnnotationInput, []byte, error) {
	input.Body = strings.TrimSpace(input.Body)
	if len(input.Body) > 20_000 {
		return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: annotation body exceeds 20000 characters", readingstate.ErrInvalid)
	}
	if input.RevisionID != nil && *input.RevisionID != currentRevisionID {
		return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: new annotations must bind to the current revision", readingstate.ErrConflict)
	}
	if create {
		input.RevisionID = &currentRevisionID
	}
	switch input.Type {
	case readingstate.AnnotationDocumentNote:
		if input.Body == "" || input.StartOffset != nil || input.EndOffset != nil || input.QuoteHash != nil {
			return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: document notes require only a non-empty body", readingstate.ErrInvalid)
		}
		return input, nil, nil
	case readingstate.AnnotationHighlight, readingstate.AnnotationHighlightNote:
		if input.StartOffset == nil || input.EndOffset == nil || *input.StartOffset < 0 ||
			*input.EndOffset <= *input.StartOffset || *input.EndOffset > 10_000_000 || input.QuoteHash == nil {
			return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: highlights require bounded offsets and a quote hash", readingstate.ErrInvalid)
		}
		if input.Type == readingstate.AnnotationHighlightNote && input.Body == "" {
			return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: highlight notes require a body", readingstate.ErrInvalid)
		}
		quoteHash, err := hex.DecodeString(*input.QuoteHash)
		if err != nil || len(quoteHash) != 32 {
			return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: quote hash must be 64 hexadecimal characters", readingstate.ErrInvalid)
		}
		normalizedHash := hex.EncodeToString(quoteHash)
		input.QuoteHash = &normalizedHash
		return input, quoteHash, nil
	default:
		return readingstate.AnnotationInput{}, nil, fmt.Errorf("%w: unsupported annotation type", readingstate.ErrInvalid)
	}
}

func scanAnnotation(
	row pgx.Row,
	storyID string,
	currentRevisionID string,
) (readingstate.Annotation, error) {
	annotation := readingstate.Annotation{StoryID: storyID}
	var startOffset pgtype.Int4
	var endOffset pgtype.Int4
	var quoteHash []byte
	if err := row.Scan(
		&annotation.ID,
		&annotation.RevisionID,
		&annotation.Type,
		&startOffset,
		&endOffset,
		&quoteHash,
		&annotation.Body,
		&annotation.CreatedAt,
		&annotation.UpdatedAt,
	); err != nil {
		return readingstate.Annotation{}, err
	}
	annotation.StartOffset = intPointer(startOffset)
	annotation.EndOffset = intPointer(endOffset)
	annotation.QuoteHash = encodedHashPointer(quoteHash)
	annotation.CreatedAt = annotation.CreatedAt.UTC()
	annotation.UpdatedAt = annotation.UpdatedAt.UTC()
	annotation.Orphaned = annotation.RevisionID != currentRevisionID
	return annotation, nil
}

func intPointer(value pgtype.Int4) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int32)
	return &result
}

func encodedHashPointer(value []byte) *string {
	if len(value) == 0 {
		return nil
	}
	result := hex.EncodeToString(value)
	return &result
}
