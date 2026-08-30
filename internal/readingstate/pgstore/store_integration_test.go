package pgstore_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/readingstate"
	"github.com/traweezy/relantern/internal/readingstate/pgstore"
)

type readingFixture struct {
	clusterIDs  []string
	itemIDs     []string
	rawIDs      []string
	revisionIDs []string
	sourceIDs   []string
	storyIDs    []string
	userID      string
}

func TestStoreEnforcesAtomicStateUndoAndAnnotationContracts(t *testing.T) {
	pool := openReadingDatabase(t)
	currentTime := time.Date(2097, time.April, 5, 12, 0, 0, 0, time.UTC)
	fixture := insertReadingFixture(t, pool, currentTime, 2)
	store, err := pgstore.New(pool, pgstore.WithClock(func() time.Time { return currentTime }))
	if err != nil {
		t.Fatalf("pgstore.New() error = %v", err)
	}
	ctx := context.Background()

	initial, err := store.State(ctx, fixture.userID, fixture.storyIDs[0])
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if initial.Location != readingstate.LocationInbox || initial.Version != 0 || initial.IsRead {
		t.Fatalf("initial state = %+v", initial)
	}

	tag, err := store.CreateTag(ctx, fixture.userID, readingstate.TagInput{
		ColorToken: "accent",
		Name:       "Evaluate",
	})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}
	starKey := "integration-star-" + uuid.NewString()
	starred := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionStar,
		ExpectedVersion: initial.Version,
		IdempotencyKey:  starKey,
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	replayed, err := store.Mutate(ctx, readingstate.Command{
		Action:          readingstate.ActionStar,
		ExpectedVersion: initial.Version,
		IdempotencyKey:  starKey,
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	if err != nil {
		t.Fatalf("second independent star mutation error = %v", err)
	}
	if replayed.MutationID != starred.MutationID || replayed.State.Version != starred.State.Version {
		t.Fatalf("idempotent replay = %+v, want mutation %s", replayed, starred.MutationID)
	}

	_, err = store.Mutate(ctx, readingstate.Command{
		Action:          readingstate.ActionMarkRead,
		ExpectedVersion: 0,
		IdempotencyKey:  "integration-conflict-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	if !errors.Is(err, readingstate.ErrConflict) {
		t.Fatalf("stale Mutate() error = %v, want ErrConflict", err)
	}

	tagged := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionAddTag,
		ExpectedVersion: starred.State.Version,
		IdempotencyKey:  "integration-tag-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		TagID:           &tag.ID,
		UserID:          fixture.userID,
	})
	archived := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionArchive,
		ExpectedVersion: tagged.State.Version,
		IdempotencyKey:  "integration-archive-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	restored, err := store.Undo(ctx, fixture.userID, fixture.storyIDs[0], archived.MutationID)
	if err != nil {
		t.Fatalf("Undo(archive) error = %v", err)
	}
	if restored.State.Location != readingstate.LocationInbox ||
		restored.State.StarredAt == nil || len(restored.State.TagIDs) != 1 {
		t.Fatalf("restored archive state = %+v", restored.State)
	}

	known := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionAlreadyKnown,
		ExpectedVersion: restored.State.Version,
		IdempotencyKey:  "integration-known-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	if _, err := store.Undo(ctx, fixture.userID, fixture.storyIDs[0], known.MutationID); err != nil {
		t.Fatalf("Undo(already known) error = %v", err)
	}
	var feedbackCount int
	if err := pool.QueryRow(ctx, `
		select count(*) from app.feedback where mutation_id = $1::uuid`, known.MutationID).Scan(&feedbackCount); err != nil {
		t.Fatalf("count undone feedback: %v", err)
	}
	if feedbackCount != 0 {
		t.Fatalf("undone feedback count = %d, want 0", feedbackCount)
	}

	feedbackKey := "integration-feedback-" + uuid.NewString()
	feedback, err := store.RecordFeedback(ctx, readingstate.FeedbackCommand{
		IdempotencyKey: feedbackKey,
		StoryID:        fixture.storyIDs[0],
		Type:           readingstate.FeedbackUseful,
		UserID:         fixture.userID,
	})
	if err != nil {
		t.Fatalf("RecordFeedback() error = %v", err)
	}
	replayedFeedback, err := store.RecordFeedback(ctx, readingstate.FeedbackCommand{
		IdempotencyKey: feedbackKey,
		StoryID:        fixture.storyIDs[0],
		Type:           readingstate.FeedbackUseful,
		UserID:         fixture.userID,
	})
	if err != nil || replayedFeedback.ID != feedback.ID {
		t.Fatalf("idempotent RecordFeedback() = %+v, %v", replayedFeedback, err)
	}
	if _, err := store.RecordFeedback(ctx, readingstate.FeedbackCommand{
		IdempotencyKey: feedbackKey,
		StoryID:        fixture.storyIDs[0],
		Type:           readingstate.FeedbackIncorrect,
		UserID:         fixture.userID,
	}); !errors.Is(err, readingstate.ErrConflict) {
		t.Fatalf("reused feedback key error = %v, want ErrConflict", err)
	}

	states, err := store.States(ctx, fixture.userID, fixture.storyIDs)
	if err != nil || len(states.States) != 2 {
		t.Fatalf("States() = %+v, %v", states, err)
	}
	bulk := readingstate.BulkCommand{
		Action:         readingstate.ActionMarkRead,
		ExpectedCount:  2,
		IdempotencyKey: "integration-bulk-" + uuid.NewString(),
		Items:          []readingstate.BulkItem{{StoryID: fixture.storyIDs[0], Version: states.States[0].Version}, {StoryID: fixture.storyIDs[1], Version: states.States[1].Version}},
		UserID:         fixture.userID,
	}
	bulkResult, err := store.BulkMutate(ctx, bulk)
	if err != nil {
		t.Fatalf("BulkMutate() error = %v", err)
	}
	undoneBulk, err := store.UndoBulk(ctx, fixture.userID, bulkResult.BulkID)
	if err != nil {
		t.Fatalf("UndoBulk() error = %v", err)
	}
	if undoneBulk.AffectedCount != 2 || undoneBulk.Mutations[0].State.IsRead || undoneBulk.Mutations[1].State.IsRead {
		t.Fatalf("UndoBulk() = %+v", undoneBulk)
	}
	if _, err := store.UndoBulk(ctx, fixture.userID, bulkResult.BulkID); err != nil {
		t.Fatalf("idempotent UndoBulk() error = %v", err)
	}

	states, err = store.States(ctx, fixture.userID, fixture.storyIDs)
	if err != nil {
		t.Fatalf("States() after bulk undo error = %v", err)
	}
	_, err = store.BulkMutate(ctx, readingstate.BulkCommand{
		Action:         readingstate.ActionArchive,
		ExpectedCount:  2,
		IdempotencyKey: "integration-rollback-" + uuid.NewString(),
		Items: []readingstate.BulkItem{
			{StoryID: fixture.storyIDs[0], Version: states.States[0].Version},
			{StoryID: fixture.storyIDs[1], Version: 0},
		},
		UserID: fixture.userID,
	})
	if !errors.Is(err, readingstate.ErrConflict) {
		t.Fatalf("conflicting BulkMutate() error = %v, want ErrConflict", err)
	}
	unchanged, err := store.State(ctx, fixture.userID, fixture.storyIDs[0])
	if err != nil || unchanged.Version != states.States[0].Version || unchanged.Location != readingstate.LocationInbox {
		t.Fatalf("rolled back bulk state = %+v, %v", unchanged, err)
	}

	note, err := store.CreateAnnotation(ctx, fixture.userID, fixture.storyIDs[0], readingstate.AnnotationInput{
		Body: "Retain this implementation detail.",
		Type: readingstate.AnnotationDocumentNote,
	})
	if err != nil {
		t.Fatalf("CreateAnnotation(note) error = %v", err)
	}
	quote := "normalized"
	quoteHash := sha256.Sum256([]byte(quote))
	encodedHash := fmt.Sprintf("%x", quoteHash)
	start := 0
	end := len(quote)
	if _, err := store.CreateAnnotation(ctx, fixture.userID, fixture.storyIDs[0], readingstate.AnnotationInput{
		EndOffset:   &end,
		QuoteHash:   &encodedHash,
		StartOffset: &start,
		Type:        readingstate.AnnotationHighlight,
	}); err != nil {
		t.Fatalf("CreateAnnotation(highlight) error = %v", err)
	}
	newRevisionID := insertMaterialRevision(t, pool, fixture.rawIDs[0], fixture.revisionIDs[0], currentTime.Add(time.Minute))
	if _, err := pool.Exec(ctx, `update app.items set current_revision_id = $2::uuid where id = $1::uuid`, fixture.itemIDs[0], newRevisionID); err != nil {
		t.Fatalf("advance item revision: %v", err)
	}
	annotations, err := store.Annotations(ctx, fixture.userID, fixture.storyIDs[0])
	if err != nil {
		t.Fatalf("Annotations() error = %v", err)
	}
	if len(annotations) != 2 || !annotations[0].Orphaned || annotations[0].ID != note.ID {
		t.Fatalf("orphaned annotations = %+v", annotations)
	}
	if _, err := pool.Exec(ctx, `update app.items set current_revision_id = $2::uuid where id = $1::uuid`, fixture.itemIDs[0], fixture.revisionIDs[0]); err != nil {
		t.Fatalf("restore item revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, newRevisionID); err != nil {
		t.Fatalf("delete replacement revision: %v", err)
	}

	currentTime = currentTime.Add(2 * time.Minute)
	latest, err := store.State(ctx, fixture.userID, fixture.storyIDs[0])
	if err != nil {
		t.Fatalf("latest State() error = %v", err)
	}
	unstarred := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionUnstar,
		ExpectedVersion: latest.Version,
		IdempotencyKey:  "integration-expiry-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	currentTime = currentTime.Add(11 * time.Second)
	if _, err := store.Undo(ctx, fixture.userID, fixture.storyIDs[0], unstarred.MutationID); !errors.Is(err, readingstate.ErrUndoExpired) {
		t.Fatalf("expired Undo() error = %v, want ErrUndoExpired", err)
	}
}

func TestStoreReturnsDueSnoozesWithMutationAndOutbox(t *testing.T) {
	pool := openReadingDatabase(t)
	currentTime := time.Date(2097, time.May, 10, 12, 0, 0, 0, time.UTC)
	fixture := insertReadingFixture(t, pool, currentTime, 1)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			delete from app.outbox_events
			where event_type = 'reading_state.snooze_returned'
				and aggregate_id = $1::uuid`, fixture.itemIDs[0])
	})
	store, err := pgstore.New(pool, pgstore.WithClock(func() time.Time { return currentTime }))
	if err != nil {
		t.Fatalf("pgstore.New() error = %v", err)
	}
	initial, err := store.State(context.Background(), fixture.userID, fixture.storyIDs[0])
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	later := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionMoveLater,
		ExpectedVersion: initial.Version,
		IdempotencyKey:  "return-snooze-later-" + uuid.NewString(),
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	unreadUntil := currentTime.Add(time.Hour)
	snoozed := mutateFixture(t, store, readingstate.Command{
		Action:          readingstate.ActionSnooze,
		ExpectedVersion: later.State.Version,
		IdempotencyKey:  "return-snooze-command-" + uuid.NewString(),
		SnoozedUntil:    &unreadUntil,
		StoryID:         fixture.storyIDs[0],
		UserID:          fixture.userID,
	})
	currentTime = unreadUntil.Add(time.Minute)
	returned, err := store.ReturnDueSnoozes(context.Background(), 200)
	if err != nil {
		t.Fatalf("ReturnDueSnoozes() error = %v", err)
	}
	if returned != 1 {
		t.Fatalf("ReturnDueSnoozes() = %d, want 1", returned)
	}
	state, err := store.State(context.Background(), fixture.userID, fixture.storyIDs[0])
	if err != nil {
		t.Fatalf("returned State() error = %v", err)
	}
	if state.Location != readingstate.LocationLater || state.IsRead ||
		state.SnoozedUntil != nil || state.SnoozedFromLocation != nil ||
		state.Version != snoozed.State.Version+1 {
		t.Fatalf("returned state = %+v", state)
	}
	if second, secondErr := store.ReturnDueSnoozes(context.Background(), 200); secondErr != nil || second != 0 {
		t.Fatalf("idempotent ReturnDueSnoozes() = %d, %v", second, secondErr)
	}

	var mutationCount int
	var outboxCount int
	var outboxStoryID string
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from app.item_state_mutations
		where user_id = $1::uuid and item_id = $2::uuid and mutation_type = 'return_snoozed'`,
		fixture.userID, fixture.itemIDs[0]).Scan(&mutationCount); err != nil {
		t.Fatalf("count returned-snooze mutations: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		select count(*), min(payload ->> 'storyId')
		from app.outbox_events
		where event_type = 'reading_state.snooze_returned'
			and aggregate_id = $1::uuid`, fixture.itemIDs[0]).Scan(&outboxCount, &outboxStoryID); err != nil {
		t.Fatalf("inspect returned-snooze outbox: %v", err)
	}
	if mutationCount != 1 || outboxCount != 1 || outboxStoryID != fixture.storyIDs[0] {
		t.Fatalf(
			"returned-snooze audit = mutations %d, outbox %d, story %q",
			mutationCount,
			outboxCount,
			outboxStoryID,
		)
	}
}

func mutateFixture(t *testing.T, store *pgstore.Store, command readingstate.Command) readingstate.MutationResult {
	t.Helper()
	result, err := store.Mutate(context.Background(), command)
	if err != nil {
		t.Fatalf("Mutate(%s) error = %v", command.Action, err)
	}
	return result
}

func openReadingDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for reading-state integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("config.LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertReadingFixture(t *testing.T, pool *pgxpool.Pool, now time.Time, count int) readingFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.NewString()
	fixture := readingFixture{}
	if err := pool.QueryRow(ctx, `
		insert into app.users (
			github_user_id, login, display_name, timezone, email, email_verified
		) values ($1, $2, $2, 'UTC', $3, true)
		returning id::text`, now.UnixNano(), "reading-"+nonce, nonce+"@example.test").Scan(&fixture.userID); err != nil {
		t.Fatalf("insert reading user: %v", err)
	}
	for index := range count {
		sourceID := fmt.Sprintf("reading-%s-%d", nonce, index)
		sourceURL := fmt.Sprintf("https://example.test/%s/%d", nonce, index)
		content := []byte(fmt.Sprintf("normalized reading fixture %d", index))
		digest := sha256.Sum256(content)
		if _, err := pool.Exec(ctx, `
			insert into app.sources (
				id, name, trust_tier, owner, origin, validation_state, homepage_url,
				content_policy, enabled, topics, reviewed_at
			) values ($1, $2, 'T0', 'integration', 'system', 'active', $3,
				'link-and-excerpt', true, array['testing'], $4)`,
			sourceID, "Reading fixture "+sourceID, "https://example.test/", now); err != nil {
			t.Fatalf("insert reading source: %v", err)
		}
		var rawID string
		if err := pool.QueryRow(ctx, `
			insert into app.raw_documents (
				source_id, canonical_url, object_key, raw_sha256, first_seen_at,
				first_fetched_at, content_policy
			) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt')
			returning id::text`, sourceID, sourceURL, "raw/reading/"+sourceID, digest[:], now).Scan(&rawID); err != nil {
			t.Fatalf("insert reading raw document: %v", err)
		}
		var revisionID string
		if err := pool.QueryRow(ctx, `
			insert into app.content_revisions (
				raw_document_id, normalized_sha256, normalized_text_object_key,
				parser_name, parser_version, title, language, normalized_bytes,
				outline, offset_map, warnings, change_kind, change_reason,
				material_change, observed_at
			) values (
				$1::uuid, $2, $3, 'fixture', '1.0.0', $4, 'en', $5,
				'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial', 'integration fixture', false, $6
			) returning id::text`, rawID, digest[:], "normalized/reading/"+sourceID, "Reading story", len(content), now).Scan(&revisionID); err != nil {
			t.Fatalf("insert reading revision: %v", err)
		}
		var itemID string
		if err := pool.QueryRow(ctx, `
			insert into app.items (
				current_revision_id, canonical_url, title, normalized_title, normalized_author,
				package_name, version, slug, lifecycle_state, first_seen_at, status, simhash
			) values (
				$1::uuid, $2, $3, $3, '', '', '', $4, 'published', $5, 'active', $6
			) returning id::text`, revisionID, sourceURL, fmt.Sprintf("reading story %d", index), fmt.Sprintf("reading-%s-%d", nonce, index), now, make([]byte, 8)).Scan(&itemID); err != nil {
			t.Fatalf("insert reading item: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			insert into app.search_documents (
				item_id, revision_id, title, summary, entity_text, package_name,
				normalized_content, source_tier, lifecycle_state, first_seen_at
			) values ($1::uuid, $2::uuid, $3, '', '', '', $4, 'T0', 'published', $5)`,
			itemID, revisionID, fmt.Sprintf("reading story %d", index), string(content), now); err != nil {
			t.Fatalf("insert reading search document: %v", err)
		}
		var storyID string
		if err := pool.QueryRow(ctx, `
			insert into app.story_clusters (
				primary_item_id, cluster_key, title, first_seen_at, last_changed_at
			) values ($1::uuid, $2, $3, $4, $4)
			returning id::text`, itemID, fmt.Sprintf("reading-%s-%d", nonce, index), fmt.Sprintf("Reading story %d", index), now).Scan(&storyID); err != nil {
			t.Fatalf("insert reading cluster: %v", err)
		}
		fixture.sourceIDs = append(fixture.sourceIDs, sourceID)
		fixture.rawIDs = append(fixture.rawIDs, rawID)
		fixture.revisionIDs = append(fixture.revisionIDs, revisionID)
		fixture.itemIDs = append(fixture.itemIDs, itemID)
		fixture.clusterIDs = append(fixture.clusterIDs, storyID)
		fixture.storyIDs = append(fixture.storyIDs, storyID)
	}
	t.Cleanup(func() { cleanupReadingFixture(pool, fixture) })
	return fixture
}

func insertMaterialRevision(t *testing.T, pool *pgxpool.Pool, rawID string, previousID string, now time.Time) string {
	t.Helper()
	content := []byte("normalized replacement content")
	digest := sha256.Sum256(content)
	var revisionID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.content_revisions (
			raw_document_id, previous_revision_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, language, normalized_bytes, outline, offset_map,
			warnings, change_kind, change_reason, material_change, observed_at
		) values (
			$1::uuid, $2::uuid, $3, $4, 'fixture', '1.0.0', 'Replacement', 'en', $5,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'material', 'integration replacement', true, $6
		) returning id::text`, rawID, previousID, digest[:], "normalized/reading/"+uuid.NewString(), len(content), now).Scan(&revisionID); err != nil {
		t.Fatalf("insert material revision: %v", err)
	}
	return revisionID
}

func cleanupReadingFixture(pool *pgxpool.Pool, fixture readingFixture) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `delete from app.users where id = $1::uuid`, fixture.userID)
	_, _ = pool.Exec(ctx, `delete from app.story_clusters where id = any($1::uuid[])`, fixture.clusterIDs)
	_, _ = pool.Exec(ctx, `delete from app.items where id = any($1::uuid[])`, fixture.itemIDs)
	_, _ = pool.Exec(ctx, `delete from app.content_revisions where id = any($1::uuid[])`, fixture.revisionIDs)
	_, _ = pool.Exec(ctx, `delete from app.raw_documents where id = any($1::uuid[])`, fixture.rawIDs)
	_, _ = pool.Exec(ctx, `delete from app.sources where id = any($1::text[])`, fixture.sourceIDs)
}
