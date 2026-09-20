package pgstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/traweezy/relantern/internal/controlplane"
	controlplanestore "github.com/traweezy/relantern/internal/controlplane/pgstore"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestResumeRearmsFailedSourceEndpoint(t *testing.T) {
	pool := openControlPlanePool(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	userID, _, _ := seedControlPlaneFixture(t, pool, now)
	cleanupControlPlaneFixture(t, pool, userID)
	sourceID := "test-resume-" + uuid.NewString()
	registryID := sourceID + "-rss"
	ctx := context.Background()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, sourceID) })
	_, err := pool.Exec(ctx, `
		insert into app.sources (id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at)
		values ($1, 'Resume fixture', 'T0', 'owner', 'owner', 'active',
			'https://example.com', 'link-and-excerpt', true, array['go'], $2)`, sourceID, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		insert into app.source_endpoints (registry_id, source_id, connector, url,
			poll_interval, priority, robots_policy, expected_content_types,
			max_response_bytes, fixture_suite, health_state)
		values ($1, $2, 'rss', 'https://example.com/feed.xml',
			interval '5 minutes', 'critical', 'feed', array['application/rss+xml'],
			1048576, 'rss-v1', 'failed')`, registryID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := jobqueue.NewIsolatedTestInserter("test_resume_source")
	if err != nil {
		t.Fatal(err)
	}
	store, err := controlplanestore.New(pool, jobs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ActOnSource(ctx, controlplane.SourceActionRequest{
		UserID: userID, SourceID: sourceID, Action: "resume", Reason: "recover reviewed endpoint",
	}, now)
	if err != nil {
		t.Fatal(fmt.Errorf("resume failed source: %w", err))
	}
	var health string
	var nextPoll *time.Time
	if err := pool.QueryRow(ctx, `select health_state, next_poll_at
		from app.source_endpoints where registry_id = $1`, registryID).Scan(&health, &nextPoll); err != nil {
		t.Fatal(err)
	}
	if health != "unverified" || nextPoll != nil {
		t.Fatalf("resumed endpoint health=%q next_poll=%v", health, nextPoll)
	}
}
