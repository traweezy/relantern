package manualcapture

import (
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestRobotsDisallowsOnlyActiveWildcardRules(t *testing.T) {
	t.Parallel()
	document := "User-agent: other\nDisallow: /private\nUser-agent: *\nDisallow: /blocked\n"
	if !robotsDisallows(document, "/blocked/story") {
		t.Fatal("wildcard robots rule did not block matching path")
	}
	if robotsDisallows(document, "/private/story") {
		t.Fatal("unrelated user-agent rule blocked path")
	}
}

func TestBoundedEmbeddingInputPreservesUTF8Boundary(t *testing.T) {
	t.Parallel()
	input := string(make([]byte, embedding.MaximumInputBytes-1)) + "é"
	bounded, err := boundedEmbeddingInput(input)
	if err != nil {
		t.Fatalf("boundedEmbeddingInput() error = %v", err)
	}
	if len([]byte(bounded)) > embedding.MaximumInputBytes {
		t.Fatalf("bounded input bytes = %d", len([]byte(bounded)))
	}
}

func TestCompleteQueuesOnlyAvailableCapabilities(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for manual-capture integration tests")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	jobs, err := jobqueue.NewIsolatedTestInserter("test_manualcapture_completion")
	if err != nil {
		t.Fatalf("NewIsolatedTestInserter() error = %v", err)
	}

	for _, test := range []struct {
		name           string
		extractEnabled bool
		wantExtraction int
	}{
		{name: "extraction_disabled", extractEnabled: false, wantExtraction: 0},
		{name: "extraction_enabled", extractEnabled: true, wantExtraction: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			captureID, itemID, clusterID, revisionID := insertCompletionFixture(t, pool, now)
			processor := &Processor{
				pool: pool, clock: clock.NewFixed(now), jobs: jobs,
				modelID: "manual-capture-test", extractEnabled: test.extractEnabled,
			}
			if err := processor.complete(context.Background(), captureID, itemID, clusterID, revisionID); err != nil {
				t.Fatalf("complete() error = %v", err)
			}
			var state string
			if err := pool.QueryRow(context.Background(), `
				select state from app.manual_captures where id = $1::uuid`, captureID,
			).Scan(&state); err != nil {
				t.Fatalf("select capture state: %v", err)
			}
			if state != "completed" {
				t.Fatalf("capture state = %q, want completed", state)
			}
			for _, job := range []struct {
				kind string
				want int
			}{
				{kind: jobqueue.ReembedEntityKind, want: 1},
				{kind: jobqueue.ExtractItemKind, want: test.wantExtraction},
			} {
				var count int
				if err := pool.QueryRow(context.Background(), `
					select count(*) from river.river_job
					where queue = 'test_manualcapture_completion' and kind = $1
						and args->>'revisionId' = $2`, job.kind, revisionID,
				).Scan(&count); err != nil {
					t.Fatalf("count %s jobs: %v", job.kind, err)
				}
				if count != job.want {
					t.Fatalf("%s jobs = %d, want %d", job.kind, count, job.want)
				}
			}
		})
	}
}

func insertCompletionFixture(t *testing.T, pool *pgxpool.Pool, now time.Time) (string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	identifier := strings.ReplaceAll(uuid.NewString(), "-", "")
	userID := uuid.NewString()
	sourceID := "manualcapture-" + identifier
	canonicalURL := "https://" + sourceID + ".example.test/article"
	var rawID, revisionID, itemID, clusterID, captureID string
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			query string
			key   string
		}{
			{`delete from river.river_job where queue = 'test_manualcapture_completion' and args->>'revisionId' = $1`, revisionID},
			{`delete from app.manual_captures where id = $1::uuid`, captureID},
			{`delete from app.story_clusters where id = $1::uuid`, clusterID},
			{`delete from app.items where id = $1::uuid`, itemID},
			{`delete from app.content_revisions where id = $1::uuid`, revisionID},
			{`delete from app.raw_documents where id = $1::uuid`, rawID},
			{`delete from app.sources where id = $1`, sourceID},
			{`delete from app.users where id = $1::uuid`, userID},
		} {
			if cleanup.key != "" {
				if _, err := pool.Exec(ctx, cleanup.query, cleanup.key); err != nil {
					t.Errorf("clean up manual-capture fixture: %v", err)
				}
			}
		}
	})
	if _, err := pool.Exec(ctx, `
		insert into app.users (
			id, github_user_id, login, display_name, timezone, email, email_verified
		) values ($1::uuid, $2, $3, 'Manual capture test', 'UTC', $4, true)`,
		userID, now.UnixNano(), sourceID, sourceID+"@tests.relantern.local",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state, homepage_url,
			content_policy, enabled, topics, reviewed_at
		) values (
			$1, 'Manual capture completion', 'T2', $2, 'owner', 'active', $3,
			'link-and-excerpt', false, array['testing']::text[], $4
		)`, sourceID, "owner:"+userID, canonicalURL, now,
	); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	rawDigest := sha256.Sum256([]byte(identifier))
	if err := pool.QueryRow(ctx, `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt')
		returning id::text`, sourceID, canonicalURL, "raw/manualcapture/"+identifier, rawDigest[:], now,
	).Scan(&rawID); err != nil {
		t.Fatalf("insert raw document: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, language, normalized_bytes, outline,
			offset_map, warnings, change_kind, change_reason, material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'manualcapture-test', '1', 'en', 1,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial', 'test fixture', false, $4
		) returning id::text`, rawID, rawDigest[:], "normalized/manualcapture/"+identifier, now,
	).Scan(&revisionID); err != nil {
		t.Fatalf("insert content revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title,
			normalized_author, slug, lifecycle_state, first_seen_at, status, simhash
		) values (
			$1::uuid, $2, 'Manual capture', 'manual capture', '', $3,
			'normalized', $4, 'active', $5
		) returning id::text`, revisionID, canonicalURL, sourceID, now, make([]byte, 8),
	).Scan(&itemID); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.story_clusters (
			primary_item_id, cluster_key, title, first_seen_at, last_changed_at
		) values ($1::uuid, $2, 'Manual capture', $3, $3)
		returning id::text`, itemID, sourceID, now,
	).Scan(&clusterID); err != nil {
		t.Fatalf("insert cluster: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.manual_captures (
			user_id, idempotency_key, requested_url, canonical_url,
			state, created_at, started_at, source_id, item_id, cluster_id
		) values (
			$1::uuid, $2, $3, $3, 'deduplicating', $4, $4, $5, null, null
		) returning id::text`, userID, "completion-"+identifier, canonicalURL, now, sourceID,
	).Scan(&captureID); err != nil {
		t.Fatalf("insert capture: %v", err)
	}
	return captureID, itemID, clusterID, revisionID
}
