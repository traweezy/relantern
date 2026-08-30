package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/readingstate"
)

const undoWindow = 10 * time.Second

type Store struct {
	clock func() time.Time
	pool  *pgxpool.Pool
}

type Option func(*Store)

func WithClock(clock func() time.Time) Option {
	return func(store *Store) {
		if clock != nil {
			store.clock = clock
		}
	}
}

func New(pool *pgxpool.Pool, configuredOptions ...Option) (*Store, error) {
	if pool == nil {
		return nil, errors.New("reading-state PostgreSQL store requires a database pool")
	}
	store := &Store{clock: time.Now, pool: pool}
	for _, configure := range configuredOptions {
		configure(store)
	}
	return store, nil
}

func (store *Store) Mutate(
	ctx context.Context,
	command readingstate.Command,
) (readingstate.MutationResult, error) {
	now := store.clock().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("begin reading-state mutation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := store.mutateTx(ctx, tx, command, nil, now)
	if err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("commit reading-state mutation: %w", err)
	}
	return result, nil
}

func (store *Store) State(
	ctx context.Context,
	userID string,
	storyID string,
) (readingstate.StoryState, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.StoryState{}, err
	}
	if err := validateIdentifier(storyID, "story ID"); err != nil {
		return readingstate.StoryState{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return readingstate.StoryState{}, fmt.Errorf("begin reading-state read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	itemID, _, err := resolveTarget(ctx, tx, userID, storyID)
	if err != nil {
		return readingstate.StoryState{}, err
	}
	state, err := loadState(ctx, tx, userID, storyID, itemID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		state = readingstate.DefaultState(storyID, store.clock().UTC())
		state.TagIDs, err = loadTagIDs(ctx, tx, userID, itemID)
	}
	if err != nil {
		return readingstate.StoryState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.StoryState{}, fmt.Errorf("commit reading-state read: %w", err)
	}
	return state, nil
}

func (store *Store) BulkMutate(
	ctx context.Context,
	command readingstate.BulkCommand,
) (readingstate.BulkMutationResult, error) {
	if err := validateIdentifier(command.UserID, "user ID"); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	if err := validateIdempotencyKey(command.IdempotencyKey, 200); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	if len(command.Items) == 0 || len(command.Items) > 200 || command.ExpectedCount != len(command.Items) {
		return readingstate.BulkMutationResult{}, fmt.Errorf(
			"%w: bulk selection must contain the confirmed count of 1 through 200 stories",
			readingstate.ErrInvalid,
		)
	}
	seen := make(map[string]struct{}, len(command.Items))
	for _, item := range command.Items {
		if err := validateIdentifier(item.StoryID, "story ID"); err != nil {
			return readingstate.BulkMutationResult{}, err
		}
		if _, duplicate := seen[item.StoryID]; duplicate {
			return readingstate.BulkMutationResult{}, fmt.Errorf("%w: duplicate story in bulk selection", readingstate.ErrInvalid)
		}
		seen[item.StoryID] = struct{}{}
	}

	now := store.clock().UTC()
	bulkID := uuid.NewSHA1(
		uuid.NameSpaceOID,
		[]byte(command.UserID+":"+command.IdempotencyKey),
	).String()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("begin bulk reading-state mutation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := readingstate.BulkMutationResult{
		AffectedCount: len(command.Items),
		BulkID:        bulkID,
		Mutations:     make([]readingstate.MutationResult, 0, len(command.Items)),
	}
	for _, item := range command.Items {
		mutation, mutationErr := store.mutateTx(ctx, tx, readingstate.Command{
			Action:          command.Action,
			DismissedReason: command.DismissedReason,
			ExpectedVersion: item.Version,
			IdempotencyKey:  command.IdempotencyKey + ":" + item.StoryID,
			SnoozedUntil:    command.SnoozedUntil,
			StoryID:         item.StoryID,
			TagID:           command.TagID,
			UserID:          command.UserID,
		}, &bulkID, now)
		if mutationErr != nil {
			return readingstate.BulkMutationResult{}, mutationErr
		}
		result.Mutations = append(result.Mutations, mutation)
	}
	if len(result.Mutations) > 0 {
		result.UndoDeadline = result.Mutations[0].UndoDeadline
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("commit bulk reading-state mutation: %w", err)
	}
	return result, nil
}

func (store *Store) ReorderLater(
	ctx context.Context,
	command readingstate.LaterOrderCommand,
) (readingstate.BulkMutationResult, error) {
	if err := validateIdentifier(command.UserID, "user ID"); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	if err := validateIdempotencyKey(command.IdempotencyKey, 200); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	if len(command.Items) == 0 || len(command.Items) > 200 || command.ExpectedCount != len(command.Items) {
		return readingstate.BulkMutationResult{}, fmt.Errorf(
			"%w: Later ordering must contain the confirmed count of 1 through 200 stories",
			readingstate.ErrInvalid,
		)
	}
	seen := make(map[string]struct{}, len(command.Items))
	for _, item := range command.Items {
		if err := validateIdentifier(item.StoryID, "story ID"); err != nil {
			return readingstate.BulkMutationResult{}, err
		}
		if _, duplicate := seen[item.StoryID]; duplicate {
			return readingstate.BulkMutationResult{}, fmt.Errorf("%w: duplicate story in Later ordering", readingstate.ErrInvalid)
		}
		seen[item.StoryID] = struct{}{}
	}

	now := store.clock().UTC()
	bulkID := uuid.NewSHA1(
		uuid.NameSpaceOID,
		[]byte(command.UserID+":later-order:"+command.IdempotencyKey),
	).String()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("begin Later reordering: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := readingstate.BulkMutationResult{
		AffectedCount: len(command.Items),
		BulkID:        bulkID,
		Mutations:     make([]readingstate.MutationResult, 0, len(command.Items)),
	}
	for index, item := range command.Items {
		position := int64(index+1) * 1024
		mutation, mutationErr := store.mutateTx(ctx, tx, readingstate.Command{
			Action:          readingstate.ActionReorderLater,
			ExpectedVersion: item.Version,
			IdempotencyKey:  command.IdempotencyKey + ":" + item.StoryID,
			LaterPosition:   &position,
			StoryID:         item.StoryID,
			UserID:          command.UserID,
		}, &bulkID, now)
		if mutationErr != nil {
			return readingstate.BulkMutationResult{}, mutationErr
		}
		result.Mutations = append(result.Mutations, mutation)
	}
	result.UndoDeadline = result.Mutations[0].UndoDeadline
	if err := tx.Commit(ctx); err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("commit Later reordering: %w", err)
	}
	return result, nil
}

func (store *Store) mutateTx(
	ctx context.Context,
	tx pgx.Tx,
	command readingstate.Command,
	bulkID *string,
	now time.Time,
) (readingstate.MutationResult, error) {
	if err := validateCommand(command); err != nil {
		return readingstate.MutationResult{}, err
	}
	itemID, _, err := resolveTarget(ctx, tx, command.UserID, command.StoryID)
	if err != nil {
		return readingstate.MutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into app.user_item_states (user_id, item_id, updated_at)
		values ($1::uuid, $2::uuid, $3)
		on conflict (user_id, item_id) do nothing`, command.UserID, itemID, now); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("ensure reading state: %w", err)
	}
	current, err := loadState(ctx, tx, command.UserID, command.StoryID, itemID, true)
	if err != nil {
		return readingstate.MutationResult{}, err
	}

	existing, found, err := existingMutation(
		ctx,
		tx,
		command.UserID,
		itemID,
		command.Action,
		command.IdempotencyKey,
		current,
	)
	if err != nil {
		return readingstate.MutationResult{}, err
	}
	if found {
		return existing, nil
	}
	if current.Version != command.ExpectedVersion {
		return readingstate.MutationResult{}, readingstate.ErrConflict
	}
	if command.Action == readingstate.ActionAddTag || command.Action == readingstate.ActionRemoveTag {
		belongs, tagErr := tagBelongsToUser(ctx, tx, command.UserID, *command.TagID)
		if tagErr != nil {
			return readingstate.MutationResult{}, tagErr
		}
		if !belongs {
			return readingstate.MutationResult{}, readingstate.ErrNotFound
		}
	}

	next, err := readingstate.Apply(current, command, now)
	if err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := persistState(ctx, tx, command.UserID, itemID, next); err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := persistTagMutation(ctx, tx, command, itemID); err != nil {
		return readingstate.MutationResult{}, err
	}
	beforeJSON, err := json.Marshal(current)
	if err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("encode prior reading state: %w", err)
	}
	afterJSON, err := json.Marshal(next)
	if err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("encode resulting reading state: %w", err)
	}
	deadline := now.Add(undoWindow)
	var mutationID string
	if err := tx.QueryRow(ctx, `
		insert into app.item_state_mutations (
			user_id, item_id, mutation_type, before_state, after_state,
			idempotency_key, bulk_id, undo_deadline, created_at
		) values ($1::uuid, $2::uuid, $3, $4::jsonb, $5::jsonb, $6, $7::uuid, $8, $9)
		returning id::text`,
		command.UserID,
		itemID,
		command.Action,
		beforeJSON,
		afterJSON,
		command.IdempotencyKey,
		bulkID,
		deadline,
		now,
	).Scan(&mutationID); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("record reading-state mutation: %w", err)
	}
	if err := persistFeedback(ctx, tx, command, mutationID, now); err != nil {
		return readingstate.MutationResult{}, err
	}
	return readingstate.MutationResult{
		MutationID: mutationID, State: next, UndoDeadline: deadline,
	}, nil
}

func (store *Store) Undo(
	ctx context.Context,
	userID string,
	storyID string,
	mutationID string,
) (readingstate.MutationResult, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := validateIdentifier(storyID, "story ID"); err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := validateIdentifier(mutationID, "mutation ID"); err != nil {
		return readingstate.MutationResult{}, err
	}
	now := store.clock().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("begin reading-state undo: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	itemID, _, err := resolveTarget(ctx, tx, userID, storyID)
	if err != nil {
		return readingstate.MutationResult{}, err
	}
	current, err := loadState(ctx, tx, userID, storyID, itemID, true)
	if err != nil {
		return readingstate.MutationResult{}, err
	}

	var beforeJSON []byte
	var afterJSON []byte
	var deadline time.Time
	var undoneAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		select before_state, after_state, undo_deadline, undone_at
		from app.item_state_mutations
		where id = $1::uuid and user_id = $2::uuid and item_id = $3::uuid
		for update`, mutationID, userID, itemID).Scan(
		&beforeJSON,
		&afterJSON,
		&deadline,
		&undoneAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return readingstate.MutationResult{}, readingstate.ErrNotFound
	}
	if err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("select reading-state mutation for undo: %w", err)
	}
	if undoneAt.Valid {
		return readingstate.MutationResult{
			MutationID: mutationID, State: current, UndoDeadline: deadline.UTC(),
		}, nil
	}
	if now.After(deadline) {
		return readingstate.MutationResult{}, readingstate.ErrUndoExpired
	}
	var before readingstate.StoryState
	var after readingstate.StoryState
	if err := json.Unmarshal(beforeJSON, &before); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("decode prior reading state: %w", err)
	}
	if err := json.Unmarshal(afterJSON, &after); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("decode resulting reading state: %w", err)
	}
	if current.Version != after.Version {
		return readingstate.MutationResult{}, readingstate.ErrUndoConflict
	}
	restored := before
	restored.StoryID = storyID
	restored.Version = current.Version + 1
	restored.UpdatedAt = now
	if err := readingstate.Validate(restored); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("validate prior reading state: %w", err)
	}
	if err := restoreTags(ctx, tx, userID, itemID, restored.TagIDs); err != nil {
		return readingstate.MutationResult{}, err
	}
	if err := persistState(ctx, tx, userID, itemID, restored); err != nil {
		return readingstate.MutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		update app.item_state_mutations set undone_at = $2 where id = $1::uuid`,
		mutationID,
		now,
	); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("mark reading-state mutation undone: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		delete from app.feedback where mutation_id = $1::uuid`, mutationID); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("remove undone story feedback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.MutationResult{}, fmt.Errorf("commit reading-state undo: %w", err)
	}
	return readingstate.MutationResult{
		MutationID: mutationID, State: restored, UndoDeadline: deadline.UTC(),
	}, nil
}

type bulkUndoCandidate struct {
	after      readingstate.StoryState
	before     readingstate.StoryState
	deadline   time.Time
	itemID     string
	mutationID string
	undone     bool
}

func (store *Store) UndoBulk(
	ctx context.Context,
	userID string,
	bulkID string,
) (readingstate.BulkMutationResult, error) {
	if err := validateIdentifier(userID, "user ID"); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	if err := validateIdentifier(bulkID, "bulk ID"); err != nil {
		return readingstate.BulkMutationResult{}, err
	}
	now := store.clock().UTC()
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("begin bulk reading-state undo: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		select id::text, item_id::text, before_state, after_state, undo_deadline,
			undone_at is not null
		from app.item_state_mutations
		where user_id = $1::uuid and bulk_id = $2::uuid
		order by item_id, id`, userID, bulkID)
	if err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("select bulk mutations for undo: %w", err)
	}
	candidates := make([]bulkUndoCandidate, 0)
	for rows.Next() {
		var beforeJSON []byte
		var afterJSON []byte
		candidate := bulkUndoCandidate{}
		if err := rows.Scan(
			&candidate.mutationID,
			&candidate.itemID,
			&beforeJSON,
			&afterJSON,
			&candidate.deadline,
			&candidate.undone,
		); err != nil {
			rows.Close()
			return readingstate.BulkMutationResult{}, fmt.Errorf("scan bulk mutation for undo: %w", err)
		}
		if err := json.Unmarshal(beforeJSON, &candidate.before); err != nil {
			rows.Close()
			return readingstate.BulkMutationResult{}, fmt.Errorf("decode bulk prior reading state: %w", err)
		}
		if err := json.Unmarshal(afterJSON, &candidate.after); err != nil {
			rows.Close()
			return readingstate.BulkMutationResult{}, fmt.Errorf("decode bulk resulting reading state: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return readingstate.BulkMutationResult{}, fmt.Errorf("iterate bulk mutations for undo: %w", err)
	}
	rows.Close()
	if len(candidates) == 0 {
		return readingstate.BulkMutationResult{}, readingstate.ErrNotFound
	}
	if len(candidates) > 200 {
		return readingstate.BulkMutationResult{}, readingstate.ErrUndoUnavailable
	}

	allUndone := true
	anyUndone := false
	result := readingstate.BulkMutationResult{
		AffectedCount: len(candidates),
		BulkID:        bulkID,
		Mutations:     make([]readingstate.MutationResult, 0, len(candidates)),
		UndoDeadline:  candidates[0].deadline.UTC(),
	}
	for index := range candidates {
		candidate := &candidates[index]
		current, loadErr := loadState(
			ctx,
			tx,
			userID,
			candidate.after.StoryID,
			candidate.itemID,
			true,
		)
		if loadErr != nil {
			return readingstate.BulkMutationResult{}, loadErr
		}
		var undoneAt pgtype.Timestamptz
		if err := tx.QueryRow(ctx, `
			select undone_at from app.item_state_mutations
			where id = $1::uuid and user_id = $2::uuid and bulk_id = $3::uuid
			for update`, candidate.mutationID, userID, bulkID).Scan(&undoneAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return readingstate.BulkMutationResult{}, readingstate.ErrUndoUnavailable
			}
			return readingstate.BulkMutationResult{}, fmt.Errorf("lock bulk mutation for undo: %w", err)
		}
		candidate.undone = undoneAt.Valid
		allUndone = allUndone && candidate.undone
		anyUndone = anyUndone || candidate.undone
		if candidate.undone {
			result.Mutations = append(result.Mutations, readingstate.MutationResult{
				MutationID:   candidate.mutationID,
				State:        current,
				UndoDeadline: candidate.deadline.UTC(),
			})
			continue
		}
		if now.After(candidate.deadline) {
			return readingstate.BulkMutationResult{}, readingstate.ErrUndoExpired
		}
		if current.Version != candidate.after.Version {
			return readingstate.BulkMutationResult{}, readingstate.ErrUndoConflict
		}
		restored := candidate.before
		restored.StoryID = candidate.after.StoryID
		restored.Version = current.Version + 1
		restored.UpdatedAt = now
		if err := readingstate.Validate(restored); err != nil {
			return readingstate.BulkMutationResult{}, fmt.Errorf("validate bulk prior reading state: %w", err)
		}
		if err := restoreTags(ctx, tx, userID, candidate.itemID, restored.TagIDs); err != nil {
			return readingstate.BulkMutationResult{}, err
		}
		if err := persistState(ctx, tx, userID, candidate.itemID, restored); err != nil {
			return readingstate.BulkMutationResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			update app.item_state_mutations set undone_at = $2 where id = $1::uuid`,
			candidate.mutationID,
			now,
		); err != nil {
			return readingstate.BulkMutationResult{}, fmt.Errorf("mark bulk mutation undone: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			delete from app.feedback where mutation_id = $1::uuid`, candidate.mutationID); err != nil {
			return readingstate.BulkMutationResult{}, fmt.Errorf("remove bulk undone feedback: %w", err)
		}
		result.Mutations = append(result.Mutations, readingstate.MutationResult{
			MutationID:   candidate.mutationID,
			State:        restored,
			UndoDeadline: candidate.deadline.UTC(),
		})
	}
	if anyUndone && !allUndone {
		return readingstate.BulkMutationResult{}, readingstate.ErrUndoUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return readingstate.BulkMutationResult{}, fmt.Errorf("commit bulk reading-state undo: %w", err)
	}
	return result, nil
}

func validateCommand(command readingstate.Command) error {
	if err := validateIdentifier(command.UserID, "user ID"); err != nil {
		return err
	}
	if err := validateIdentifier(command.StoryID, "story ID"); err != nil {
		return err
	}
	if command.ExpectedVersion < 0 {
		return fmt.Errorf("%w: state version may not be negative", readingstate.ErrInvalid)
	}
	if command.Action == readingstate.ActionAddTag || command.Action == readingstate.ActionRemoveTag {
		if command.TagID == nil {
			return fmt.Errorf("%w: tag mutation requires a tag ID", readingstate.ErrInvalid)
		}
		if err := validateIdentifier(*command.TagID, "tag ID"); err != nil {
			return err
		}
	}
	return validateIdempotencyKey(command.IdempotencyKey, 255)
}

func validateIdentifier(value string, name string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%w: %s must be a UUID", readingstate.ErrInvalid, name)
	}
	return nil
}

func validateIdempotencyKey(value string, maximum int) error {
	if len(value) < 16 || len(value) > maximum || strings.TrimSpace(value) != value ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf(
			"%w: idempotency key must contain 16 through %d safe characters",
			readingstate.ErrInvalid,
			maximum,
		)
	}
	return nil
}

func resolveTarget(
	ctx context.Context,
	queryer queryRower,
	userID string,
	storyID string,
) (string, string, error) {
	var itemID string
	var revisionID string
	err := queryer.QueryRow(ctx, `
		select cluster.primary_item_id::text, item.current_revision_id::text
		from app.story_clusters cluster
		join app.items item on item.id = cluster.primary_item_id
		join app.users owner on owner.id = $1::uuid
		where cluster.id = $2::uuid`, userID, storyID).Scan(&itemID, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", readingstate.ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve owner story target: %w", err)
	}
	return itemID, revisionID, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type rowScanner interface {
	Scan(...any) error
}

func loadState(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	storyID string,
	itemID string,
	forUpdate bool,
) (readingstate.StoryState, error) {
	query := `
		select location, is_read, read_at, starred_at, snoozed_until,
			snoozed_from_location, reading_progress::float8, last_paragraph_id,
			later_position, dismissed_reason, version, updated_at
		from app.user_item_states
		where user_id = $1::uuid and item_id = $2::uuid`
	if forUpdate {
		query += " for update"
	}
	state, err := scanState(tx.QueryRow(ctx, query, userID, itemID), storyID)
	if err != nil {
		return readingstate.StoryState{}, fmt.Errorf("load reading state: %w", err)
	}
	state.TagIDs, err = loadTagIDs(ctx, tx, userID, itemID)
	if err != nil {
		return readingstate.StoryState{}, err
	}
	return state, nil
}

func scanState(row rowScanner, storyID string) (readingstate.StoryState, error) {
	state := readingstate.StoryState{StoryID: storyID, TagIDs: []string{}}
	var readAt pgtype.Timestamptz
	var starredAt pgtype.Timestamptz
	var snoozedUntil pgtype.Timestamptz
	var snoozedFrom pgtype.Text
	var lastParagraphID pgtype.Text
	var laterPosition pgtype.Int8
	var dismissedReason pgtype.Text
	err := row.Scan(
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
	)
	if err != nil {
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
	return state, nil
}

func loadTagIDs(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	itemID string,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
		select tag_id::text from app.item_tags
		where user_id = $1::uuid and item_id = $2::uuid
		order by tag_id`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("select story tags: %w", err)
	}
	defer rows.Close()
	tagIDs := make([]string, 0)
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			return nil, fmt.Errorf("scan story tag: %w", err)
		}
		tagIDs = append(tagIDs, tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate story tags: %w", err)
	}
	return tagIDs, nil
}

func existingMutation(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	itemID string,
	action readingstate.Action,
	idempotencyKey string,
	current readingstate.StoryState,
) (readingstate.MutationResult, bool, error) {
	var mutationID string
	var mutationType string
	var afterJSON []byte
	var deadline time.Time
	var undoneAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
		select id::text, mutation_type, after_state, undo_deadline, undone_at
		from app.item_state_mutations
		where user_id = $1::uuid and idempotency_key = $2
		for update`, userID, idempotencyKey).Scan(
		&mutationID,
		&mutationType,
		&afterJSON,
		&deadline,
		&undoneAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return readingstate.MutationResult{}, false, nil
	}
	if err != nil {
		return readingstate.MutationResult{}, false, fmt.Errorf("select idempotent reading-state mutation: %w", err)
	}
	var storedItemID string
	if err := tx.QueryRow(ctx, `
		select item_id::text from app.item_state_mutations where id = $1::uuid`, mutationID).Scan(&storedItemID); err != nil {
		return readingstate.MutationResult{}, false, fmt.Errorf("verify idempotent reading-state mutation: %w", err)
	}
	if storedItemID != itemID || mutationType != string(action) {
		return readingstate.MutationResult{}, false, fmt.Errorf(
			"%w: idempotency key was already used for another command",
			readingstate.ErrConflict,
		)
	}
	state := current
	if !undoneAt.Valid {
		if err := json.Unmarshal(afterJSON, &state); err != nil {
			return readingstate.MutationResult{}, false, fmt.Errorf("decode idempotent reading-state mutation: %w", err)
		}
	}
	return readingstate.MutationResult{
		MutationID: mutationID, State: state, UndoDeadline: deadline.UTC(),
	}, true, nil
}

func persistState(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	itemID string,
	state readingstate.StoryState,
) error {
	result, err := tx.Exec(ctx, `
		update app.user_item_states set
			location = $3,
			is_read = $4,
			read_at = $5,
			starred_at = $6,
			snoozed_until = $7,
			snoozed_from_location = $8,
			reading_progress = $9,
			last_paragraph_id = $10,
			later_position = $11,
			dismissed_reason = $12,
			version = $13,
			updated_at = $14
		where user_id = $1::uuid and item_id = $2::uuid`,
		userID,
		itemID,
		state.Location,
		state.IsRead,
		state.ReadAt,
		state.StarredAt,
		state.SnoozedUntil,
		locationStringPointer(state.SnoozedFromLocation),
		state.ReadingProgress,
		state.LastParagraphID,
		state.LaterPosition,
		state.DismissedReason,
		state.Version,
		state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("persist reading state: %w", err)
	}
	if result.RowsAffected() != 1 {
		return readingstate.ErrConflict
	}
	return nil
}

func persistTagMutation(
	ctx context.Context,
	tx pgx.Tx,
	command readingstate.Command,
	itemID string,
) error {
	if command.TagID == nil {
		return nil
	}
	switch command.Action {
	case readingstate.ActionAddTag:
		if _, err := tx.Exec(ctx, `
			insert into app.item_tags (user_id, item_id, tag_id)
			values ($1::uuid, $2::uuid, $3::uuid)
			on conflict (user_id, item_id, tag_id) do nothing`,
			command.UserID,
			itemID,
			*command.TagID,
		); err != nil {
			return fmt.Errorf("add story tag: %w", err)
		}
	case readingstate.ActionRemoveTag:
		if _, err := tx.Exec(ctx, `
			delete from app.item_tags
			where user_id = $1::uuid and item_id = $2::uuid and tag_id = $3::uuid`,
			command.UserID,
			itemID,
			*command.TagID,
		); err != nil {
			return fmt.Errorf("remove story tag: %w", err)
		}
	}
	return nil
}

func persistFeedback(
	ctx context.Context,
	tx pgx.Tx,
	command readingstate.Command,
	mutationID string,
	now time.Time,
) error {
	feedbackType := ""
	var note *string
	switch command.Action {
	case readingstate.ActionAlreadyKnown:
		feedbackType = "already_known"
	case readingstate.ActionDismiss:
		feedbackType = "dismissed"
		note = command.DismissedReason
	default:
		return nil
	}
	if _, err := tx.Exec(ctx, `
		insert into app.feedback (
			user_id, target_type, target_id, feedback_type, note, mutation_id,
			idempotency_key, created_at
		) values ($1::uuid, 'story', $2::uuid, $3, $4, $5::uuid, $6, $7)`,
		command.UserID,
		command.StoryID,
		feedbackType,
		note,
		mutationID,
		command.IdempotencyKey,
		now,
	); err != nil {
		return fmt.Errorf("record story feedback: %w", err)
	}
	return nil
}

func tagBelongsToUser(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	tagID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from app.tags where id = $1::uuid and user_id = $2::uuid
		)`, tagID, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("validate story tag ownership: %w", err)
	}
	return exists, nil
}

func restoreTags(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	itemID string,
	tagIDs []string,
) error {
	if len(tagIDs) > 0 {
		var count int
		if err := tx.QueryRow(ctx, `
			select count(*) from app.tags
			where user_id = $1::uuid and id = any($2::uuid[])`, userID, tagIDs).Scan(&count); err != nil {
			return fmt.Errorf("validate undo tags: %w", err)
		}
		if count != len(tagIDs) {
			return readingstate.ErrUndoConflict
		}
	}
	if _, err := tx.Exec(ctx, `
		delete from app.item_tags where user_id = $1::uuid and item_id = $2::uuid`,
		userID,
		itemID,
	); err != nil {
		return fmt.Errorf("clear tags for reading-state undo: %w", err)
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx, `
			insert into app.item_tags (user_id, item_id, tag_id)
			values ($1::uuid, $2::uuid, $3::uuid)`, userID, itemID, tagID); err != nil {
			return fmt.Errorf("restore tag for reading-state undo: %w", err)
		}
	}
	return nil
}

func timestampPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func locationStringPointer(value *readingstate.Location) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}
