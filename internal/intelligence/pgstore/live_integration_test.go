package pgstore_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/intelligence/pgstore"
)

func TestLiveReplayUsesDurableOutboxAndNotifications(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for live replay integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	insertedIDs := make([]int64, 0, 3)
	defer func() {
		for _, id := range insertedIDs {
			_, _ = pool.Exec(context.Background(), `delete from app.outbox_events where id = $1`, id)
		}
	}()
	insert := func(createdAt time.Time) int64 {
		t.Helper()
		storyID := uuid.NewString()
		payload := fmt.Sprintf(`{"observedAt":%q,"story":{"id":%q,"headline":"Replay test"}}`,
			createdAt.Format(time.RFC3339Nano), storyID)
		var id int64
		if err := pool.QueryRow(ctx, `
			insert into app.outbox_events (event_type, aggregate_type, aggregate_id, payload, created_at)
			values ('story-created', 'story', $1::uuid, $2::jsonb, $3)
			returning id`, storyID, payload, createdAt).Scan(&id); err != nil {
			t.Fatalf("insert live event: %v", err)
		}
		insertedIDs = append(insertedIDs, id)
		return id
	}

	first := insert(now)
	pooledQueriesBefore := pool.Stat().AcquiredConns()
	subscription, err := store.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if acquired := pool.Stat().AcquiredConns(); acquired != pooledQueriesBefore {
		t.Fatalf("LISTEN reserved %d query pool connections, want %d", acquired, pooledQueriesBefore)
	}
	second := insert(now.Add(time.Millisecond))
	if err := subscription.Wait(ctx); err != nil {
		t.Fatalf("wait for committed outbox notification: %v", err)
	}
	batch, err := store.Replay(ctx, first, now.Add(time.Second), 100)
	if err != nil {
		t.Fatal(err)
	}
	if batch.ResetRequired || len(batch.Events) != 1 || batch.Events[0].Cursor != second ||
		batch.Events[0].Event.ID != fmt.Sprint(second) {
		t.Fatalf("replayed events = %+v", batch)
	}
	snapshot, err := store.Live(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor != fmt.Sprint(second) {
		t.Fatalf("live snapshot cursor = %q, want %d", snapshot.Cursor, second)
	}
	old := insert(now.Add(-8 * 24 * time.Hour))
	snapshot, err = store.Live(ctx, now.Add(time.Second))
	if err != nil || snapshot.Cursor != fmt.Sprint(second) {
		t.Fatalf("live snapshot after an expired event = %+v, %v", snapshot, err)
	}
	expired, err := store.Replay(ctx, old, now, 100)
	if err != nil || !expired.ResetRequired {
		t.Fatalf("expired cursor replay = %+v, %v", expired, err)
	}
}
