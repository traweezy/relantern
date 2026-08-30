package pgstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/traweezy/relantern/internal/readingstate"
)

var feedbackTypes = map[readingstate.FeedbackType]struct{}{
	readingstate.FeedbackUseful:     {},
	readingstate.FeedbackKnown:      {},
	readingstate.FeedbackIrrelevant: {},
	readingstate.FeedbackShallow:    {},
	readingstate.FeedbackVerbose:    {},
	readingstate.FeedbackIncorrect:  {},
}

func (store *Store) RecordFeedback(
	ctx context.Context,
	command readingstate.FeedbackCommand,
) (readingstate.Feedback, error) {
	if err := validateIdentifier(command.UserID, "user ID"); err != nil {
		return readingstate.Feedback{}, err
	}
	if err := validateIdentifier(command.StoryID, "story ID"); err != nil {
		return readingstate.Feedback{}, err
	}
	if err := validateIdempotencyKey(command.IdempotencyKey, 255); err != nil {
		return readingstate.Feedback{}, err
	}
	if _, allowed := feedbackTypes[command.Type]; !allowed {
		return readingstate.Feedback{}, fmt.Errorf("%w: unsupported feedback type", readingstate.ErrInvalid)
	}
	var note *string
	if command.Note != nil {
		normalized := strings.TrimSpace(*command.Note)
		if len(normalized) > 2_000 {
			return readingstate.Feedback{}, fmt.Errorf("%w: feedback note exceeds 2000 characters", readingstate.ErrInvalid)
		}
		if normalized != "" {
			note = &normalized
		}
	}

	now := store.clock().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.Feedback{}, fmt.Errorf("begin story feedback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, _, err := resolveTarget(ctx, tx, command.UserID, command.StoryID); err != nil {
		return readingstate.Feedback{}, err
	}

	feedback := readingstate.Feedback{
		CreatedAt: now,
		Note:      note,
		StoryID:   command.StoryID,
		Type:      command.Type,
	}
	err = tx.QueryRow(ctx, `
		insert into app.feedback (
			user_id, target_type, target_id, feedback_type, note,
			idempotency_key, created_at
		) values ($1::uuid, 'story', $2::uuid, $3, $4, $5, $6)
		on conflict (user_id, idempotency_key) where idempotency_key is not null
		do nothing
		returning id::text`,
		command.UserID,
		command.StoryID,
		command.Type,
		note,
		command.IdempotencyKey,
		now,
	).Scan(&feedback.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var storedTarget string
		var storedType readingstate.FeedbackType
		var storedNote pgtype.Text
		if err := tx.QueryRow(ctx, `
			select id::text, target_id::text, feedback_type, note, created_at
			from app.feedback
			where user_id = $1::uuid and idempotency_key = $2
			for update`, command.UserID, command.IdempotencyKey).Scan(
			&feedback.ID,
			&storedTarget,
			&storedType,
			&storedNote,
			&feedback.CreatedAt,
		); err != nil {
			return readingstate.Feedback{}, fmt.Errorf("load idempotent story feedback: %w", err)
		}
		feedback.CreatedAt = feedback.CreatedAt.UTC()
		feedback.Note = textPointer(storedNote)
		if storedTarget != command.StoryID || storedType != command.Type || !sameOptionalText(feedback.Note, note) {
			return readingstate.Feedback{}, fmt.Errorf(
				"%w: idempotency key was already used for different feedback",
				readingstate.ErrConflict,
			)
		}
	} else if err != nil {
		return readingstate.Feedback{}, fmt.Errorf("record story feedback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.Feedback{}, fmt.Errorf("commit story feedback: %w", err)
	}
	return feedback, nil
}

func sameOptionalText(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
