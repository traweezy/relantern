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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/research"
	"github.com/traweezy/relantern/internal/research/pgstore"
)

type researchFixture struct {
	SourceID        string
	RawDocumentID   string
	RevisionID      string
	ItemID          string
	ClusterID       string
	ExtractionRunID string
	ClaimID         string
	SourceURL       string
}

func TestStorePersistsBackgroundResearchAndEnforcesSearchBudget(t *testing.T) {
	pool := openIntegrationDatabase(t)
	now := time.Date(2097, time.March, 14, 12, 0, 0, 0, time.UTC)
	fixture := insertResearchFixture(t, pool, now)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New() error = %v", err)
	}
	configured := research.Config{
		MaximumToolCalls:    4,
		DailyWebSearchLimit: 4,
		MonthlySoftUSD:      "25.00",
		MonthlyHardUSD:      "100.00",
	}

	prepared, err := store.Prepare(
		context.Background(),
		research.ProcessRequest{ClusterID: fixture.ClusterID},
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(prepared.Claims) != 1 || prepared.Claims[0].ID != fixture.ClaimID {
		t.Fatalf("prepared claims = %+v", prepared.Claims)
	}
	reservation, err := reserveWithSerializationRetry(
		t,
		store,
		context.Background(), prepared, 100, configured, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("ReserveAttempt() error = %v", err)
	}
	if reservation.Number != 1 || reservation.MaximumToolCalls != 4 {
		t.Fatalf("reservation = %+v", reservation)
	}
	queued := research.ProviderResponse{ID: "resp_research_" + uuid.NewString(), Status: "queued"}
	if err := store.AttachProvider(
		context.Background(), prepared, reservation, queued, now.Add(3*time.Minute),
	); err != nil {
		t.Fatalf("AttachProvider() error = %v", err)
	}
	loaded, err := store.LoadPending(context.Background(), research.PollRequest{ResponseID: queued.ID})
	if err != nil {
		t.Fatalf("LoadPending() without run ID error = %v", err)
	}
	if loaded.RunID != prepared.RunID || loaded.Reservation.ID != reservation.ID {
		t.Fatalf("loaded pending run = %+v", loaded)
	}
	completed := research.ProviderResponse{
		ID: queued.ID, Status: "completed",
		Usage: research.Usage{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 50, ToolCalls: 1},
	}
	if err := store.RecordAttempt(
		context.Background(), loaded, loaded.Reservation, completed, "", now.Add(4*time.Minute),
	); err != nil {
		t.Fatalf("RecordAttempt() error = %v", err)
	}
	source := research.Source{URL: "https://go.dev/doc/go1.27", Domain: "go.dev"}
	result, err := store.Complete(context.Background(), loaded, research.Completion{
		ProviderID: queued.ID,
		Sources:    []research.Source{source},
		Usage:      completed.Usage,
		Output: research.Output{
			Headline:          "Go 1.27 is available",
			Summary:           "The validated release claim is available for review.",
			WhyItMatters:      "The release changes the supported toolchain baseline.",
			RecommendedAction: "Review the official release documentation before upgrading.",
			Confidence:        "high",
			Assertions: []research.Assertion{{
				Text:       "Go 1.27 is the released version.",
				Material:   true,
				ClaimIDs:   []string{fixture.ClaimID},
				SourceURLs: []string{source.URL},
			}},
			Uncertainties: []string{},
		},
	}, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if result.AssertionCount != 1 || result.RunID != prepared.RunID {
		t.Fatalf("completion result = %+v", result)
	}
	assertPersistedResearch(t, pool, prepared.RunID, queued.ID, fixture.ClaimID, source.URL)

	replayed, err := store.Complete(
		context.Background(), loaded, research.Completion{ProviderID: queued.ID}, now.Add(6*time.Minute),
	)
	if err != nil || !replayed.AlreadyCompleted || replayed.AssertionCount != 1 {
		t.Fatalf("replayed Complete() = %+v, %v", replayed, err)
	}

	insertAdditionalVerifiedClaim(t, pool, fixture, 1)
	changed, err := store.Prepare(
		context.Background(),
		research.ProcessRequest{ClusterID: fixture.ClusterID},
		now.Add(7*time.Minute),
	)
	if err != nil {
		t.Fatalf("changed Prepare() error = %v", err)
	}
	if changed.RunID == prepared.RunID || len(changed.Claims) != 2 {
		t.Fatalf("changed preparation reused stale input: %+v", changed)
	}
	_, err = reserveWithSerializationRetry(
		t,
		store,
		context.Background(), changed, 100, configured, now.Add(8*time.Minute),
	)
	if !errors.Is(err, research.ErrWebSearchLimit) {
		t.Fatalf("ReserveAttempt() error = %v, want ErrWebSearchLimit", err)
	}
	var blockedState string
	var errorCode string
	if err := pool.QueryRow(context.Background(), `
		select state, error_code from app.ai_runs where id = $1::uuid`, changed.RunID).Scan(
		&blockedState, &errorCode,
	); err != nil {
		t.Fatalf("inspect blocked research run: %v", err)
	}
	if blockedState != "budget_blocked" || errorCode != "web_search_limit" {
		t.Fatalf("blocked research run = %q/%q", blockedState, errorCode)
	}
}

func reserveWithSerializationRetry(
	t *testing.T,
	store *pgstore.Store,
	ctx context.Context,
	prepared research.PreparedRun,
	estimatedInputTokens int64,
	configured research.Config,
	startedAt time.Time,
) (research.AttemptReservation, error) {
	t.Helper()
	var lastError error
	for attempt := 1; attempt <= 5; attempt++ {
		reservation, err := store.ReserveAttempt(
			ctx, prepared, estimatedInputTokens, configured, startedAt,
		)
		if err == nil {
			return reservation, nil
		}
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
			return research.AttemptReservation{}, err
		}
		lastError = err
		time.Sleep(time.Duration(attempt) * 10 * time.Millisecond)
	}
	return research.AttemptReservation{}, lastError
}

func assertPersistedResearch(
	t *testing.T,
	pool *pgxpool.Pool,
	runID string,
	providerID string,
	claimID string,
	sourceURL string,
) {
	t.Helper()
	var state string
	var storedProviderID string
	var inputTokens int64
	var cachedTokens int64
	var outputTokens int64
	var toolCalls int
	var cost string
	var linkedClaimID string
	var linkedSourceURL string
	if err := pool.QueryRow(context.Background(), `
		select run.state, run.provider_response_id, run.input_tokens,
			run.cached_input_tokens, run.output_tokens, run.tool_calls,
			run.estimated_cost_usd::text, assertion_claim.claim_id::text,
			source.source_url
		from app.ai_runs run
		join app.research_briefs brief on brief.ai_run_id = run.id
		join app.research_assertions assertion on assertion.research_brief_id = brief.id
		join app.research_assertion_claims assertion_claim
			on assertion_claim.research_assertion_id = assertion.id
		join app.research_assertion_sources assertion_source
			on assertion_source.research_assertion_id = assertion.id
		join app.research_sources source on source.id = assertion_source.research_source_id
		where run.id = $1::uuid`, runID).Scan(
		&state,
		&storedProviderID,
		&inputTokens,
		&cachedTokens,
		&outputTokens,
		&toolCalls,
		&cost,
		&linkedClaimID,
		&linkedSourceURL,
	); err != nil {
		t.Fatalf("inspect persisted research: %v", err)
	}
	if state != "completed" || storedProviderID != providerID || inputTokens != 100 ||
		cachedTokens != 20 || outputTokens != 50 || toolCalls != 1 || cost != "0.01076400" ||
		linkedClaimID != claimID || linkedSourceURL != sourceURL {
		t.Fatalf(
			"research = %q/%q usage %d/%d/%d/%d cost %s claim %q source %q",
			state,
			storedProviderID,
			inputTokens,
			cachedTokens,
			outputTokens,
			toolCalls,
			cost,
			linkedClaimID,
			linkedSourceURL,
		)
	}
}

func insertResearchFixture(t *testing.T, pool *pgxpool.Pool, now time.Time) researchFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.NewString()
	content := []byte("Go 1.27 is the stable released version.")
	digest := sha256.Sum256(content)
	fixture := researchFixture{
		SourceID:  "research-" + nonce,
		SourceURL: "https://go.dev/doc/go1.27",
	}
	if _, err := pool.Exec(ctx, `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state, homepage_url,
			content_policy, enabled, topics, reviewed_at
		) values ($1, $2, 'T0', 'integration', 'system', 'active', $3,
			'link-and-excerpt', true, array['go'], $4)`,
		fixture.SourceID, "Research fixture "+nonce, "https://go.dev/", now,
	); err != nil {
		t.Fatalf("insert research source: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.raw_documents (
			source_id, canonical_url, object_key, raw_sha256, first_seen_at,
			first_fetched_at, content_policy
		) values ($1, $2, $3, $4, $5, $5, 'link-and-excerpt')
		returning id::text`,
		fixture.SourceID, fixture.SourceURL, "raw/research/"+nonce+".txt", digest[:], now,
	).Scan(&fixture.RawDocumentID); err != nil {
		t.Fatalf("insert research raw document: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.content_revisions (
			raw_document_id, normalized_sha256, normalized_text_object_key,
			parser_name, parser_version, title, language, normalized_bytes,
			outline, offset_map, warnings, change_kind, change_reason,
			material_change, observed_at
		) values (
			$1::uuid, $2, $3, 'fixture', '1.0.0', 'Go 1.27 release', 'en', $4,
			'[]'::jsonb, '[]'::jsonb, '[]'::jsonb, 'initial', 'integration fixture', false, $5
		) returning id::text`,
		fixture.RawDocumentID, digest[:], "normalized/research/"+nonce+".txt", len(content), now,
	).Scan(&fixture.RevisionID); err != nil {
		t.Fatalf("insert research revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title, normalized_author,
			package_name, version, slug, lifecycle_state, first_seen_at, status, simhash
		) values (
			$1::uuid, $2, 'Go 1.27 release', 'go 1.27 release', 'go team',
			'go', '1.27', $3, 'ready', $4, 'active', $5
		) returning id::text`,
		fixture.RevisionID, fixture.SourceURL, "research-"+nonce, now, make([]byte, 8),
	).Scan(&fixture.ItemID); err != nil {
		t.Fatalf("insert research item: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		) values ($1::uuid, $2::uuid, $3, 'primary', 'T0', 0)`,
		fixture.RevisionID, fixture.ItemID, fixture.SourceURL,
	); err != nil {
		t.Fatalf("insert research item source: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.story_clusters (
			primary_item_id, cluster_key, title, first_seen_at, last_changed_at
		) values ($1::uuid, $2, 'Go 1.27 release', $3, $3)
		returning id::text`, fixture.ItemID, "research-"+nonce, now).Scan(&fixture.ClusterID); err != nil {
		t.Fatalf("insert research cluster: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into app.cluster_members (cluster_id, item_id, similarity, method)
		values ($1::uuid, $2::uuid, 1, 'integration')`, fixture.ClusterID, fixture.ItemID,
	); err != nil {
		t.Fatalf("insert research cluster member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into app.ai_runs (
			item_id, revision_id, purpose, model_config_id, prompt_version_id,
			background, state, input_sha256, started_at, completed_at
		) select $1::uuid, $2::uuid, 'structured_extraction', model.id, prompt.id,
			false, 'completed', $3, $4, $4
		from app.model_configs model
		cross join app.prompt_versions prompt
		where model.role = 'fast' and model.enabled
			and prompt.purpose = 'structured_extraction' and prompt.active
		returning id::text`, fixture.ItemID, fixture.RevisionID, digest[:], now).Scan(&fixture.ExtractionRunID); err != nil {
		t.Fatalf("insert extraction run for research: %v", err)
	}
	fixture.ClaimID = insertVerifiedClaim(t, pool, fixture, 0, "Go 1.27 is the released version.")
	t.Cleanup(func() { cleanupResearchFixture(pool, fixture) })
	return fixture
}

func insertAdditionalVerifiedClaim(t *testing.T, pool *pgxpool.Pool, fixture researchFixture, index int) {
	t.Helper()
	insertVerifiedClaim(t, pool, fixture, index, "Go 1.27 includes an updated runtime scheduler.")
}

func insertVerifiedClaim(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture researchFixture,
	index int,
	claimText string,
) string {
	t.Helper()
	var claimID string
	if err := pool.QueryRow(context.Background(), `
		insert into app.claims (
			item_id, revision_id, ai_run_id, claim_index, claim_type, claim_text,
			normalized_value, confidence, material, verification_state
		) values ($1::uuid, $2::uuid, $3::uuid, $4, 'version', $5, $6::jsonb,
			'high', true, 'verified_span')
		returning id::text`,
		fixture.ItemID,
		fixture.RevisionID,
		fixture.ExtractionRunID,
		index,
		claimText,
		fmt.Sprintf("%q", claimText),
	).Scan(&claimID); err != nil {
		t.Fatalf("insert verified claim: %v", err)
	}
	quoteHash := sha256.Sum256([]byte(claimText))
	if _, err := pool.Exec(context.Background(), `
		insert into app.evidence_spans (
			claim_id, revision_id, span_identifier, section_path,
			start_offset, end_offset, quote_hash
		) values ($1::uuid, $2::uuid, $3, 'Release', 0, $4, $5)`,
		claimID,
		fixture.RevisionID,
		fmt.Sprintf("span_%04d", index+1),
		len(claimText),
		quoteHash[:],
	); err != nil {
		t.Fatalf("insert verified evidence span: %v", err)
	}
	return claimID
}

func cleanupResearchFixture(pool *pgxpool.Pool, fixture researchFixture) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `delete from app.research_assertion_sources where research_assertion_id in (
		select assertion.id from app.research_assertions assertion
		join app.research_briefs brief on brief.id = assertion.research_brief_id
		where brief.cluster_id = $1::uuid
	)`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.research_assertion_claims where research_assertion_id in (
		select assertion.id from app.research_assertions assertion
		join app.research_briefs brief on brief.id = assertion.research_brief_id
		where brief.cluster_id = $1::uuid
	)`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.research_assertions where research_brief_id in (
		select id from app.research_briefs where cluster_id = $1::uuid
	)`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.research_sources where ai_run_id in (
		select id from app.ai_runs where cluster_id = $1::uuid
	)`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.research_briefs where cluster_id = $1::uuid`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.evidence_spans where revision_id = $1::uuid`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.claims where revision_id = $1::uuid`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.ai_run_attempts where ai_run_id in (
		select id from app.ai_runs where revision_id = $1::uuid
	)`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.ai_runs where revision_id = $1::uuid`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.cluster_members where cluster_id = $1::uuid`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.story_clusters where id = $1::uuid`, fixture.ClusterID)
	_, _ = pool.Exec(ctx, `delete from app.item_sources where revision_id = $1::uuid`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.items where id = $1::uuid`, fixture.ItemID)
	_, _ = pool.Exec(ctx, `delete from app.content_revisions where id = $1::uuid`, fixture.RevisionID)
	_, _ = pool.Exec(ctx, `delete from app.raw_documents where id = $1::uuid`, fixture.RawDocumentID)
	_, _ = pool.Exec(ctx, `delete from app.sources where id = $1`, fixture.SourceID)
}

func openIntegrationDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for research integration tests")
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
