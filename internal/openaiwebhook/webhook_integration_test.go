package openaiwebhook_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/openaiwebhook"
)

func TestDuplicateVerifiedWebhookProducesOneEventAndOnePollJob(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()
	inserter, err := jobqueue.NewInserter()
	if err != nil {
		t.Fatalf("jobqueue.NewInserter() error = %v", err)
	}
	store, err := openaiwebhook.NewStore(pool, inserter)
	if err != nil {
		t.Fatalf("openaiwebhook.NewStore() error = %v", err)
	}
	nonce := time.Now().UTC().Format("20060102150405.000000000")
	event := openaiwebhook.VerifiedEvent{
		WebhookID:      "wh_integration_" + nonce,
		EventID:        "evt_integration_" + nonce,
		EventType:      "response.completed",
		ResponseID:     "resp_integration_" + nonce,
		EventCreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from river.river_job where kind = $1 and args->>'responseId' = $2`, jobqueue.PollOpenAIBackgroundKind, event.ResponseID)
		_, _ = pool.Exec(ctx, `delete from app.openai_webhook_events where webhook_id = $1`, event.WebhookID)
	}
	cleanup()
	t.Cleanup(cleanup)

	first, err := store.Accept(ctx, event, event.EventCreatedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("first Accept() error = %v", err)
	}
	second, err := store.Accept(ctx, event, event.EventCreatedAt.Add(2*time.Second))
	if err != nil {
		t.Fatalf("duplicate Accept() error = %v", err)
	}
	if !first.Accepted || first.Duplicate || !second.Accepted || !second.Duplicate {
		t.Fatalf("first = %+v, second = %+v", first, second)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `select count(*) from app.openai_webhook_events where webhook_id = $1`, event.WebhookID).Scan(&eventCount); err != nil {
		t.Fatalf("count webhook events: %v", err)
	}
	var jobCount int
	if err := pool.QueryRow(ctx, `
		select count(*) from river.river_job
		where kind = $1 and args->>'responseId' = $2`, jobqueue.PollOpenAIBackgroundKind, event.ResponseID).Scan(&jobCount); err != nil {
		t.Fatalf("count poll jobs: %v", err)
	}
	if eventCount != 1 || jobCount != 1 {
		t.Fatalf("event count = %d, job count = %d", eventCount, jobCount)
	}

	collision := event
	collision.EventID += "_changed"
	if _, err := store.Accept(ctx, collision, event.EventCreatedAt.Add(3*time.Second)); err == nil {
		t.Fatal("Accept() accepted a webhook identifier collision")
	}
}
