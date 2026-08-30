package pgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/discovery"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/jobqueue"
	"github.com/traweezy/relantern/internal/search"
	searchstore "github.com/traweezy/relantern/internal/search/pgstore"
)

const importPreviewLifetime = 30 * time.Minute

type Store struct {
	pool               *pgxpool.Pool
	search             *searchstore.Store
	jobs               *jobqueue.Inserter
	allowLocalFixtures bool
}

func New(
	pool *pgxpool.Pool,
	searchIndex *searchstore.Store,
	jobs *jobqueue.Inserter,
	allowLocalFixtures bool,
) (*Store, error) {
	if pool == nil || searchIndex == nil || jobs == nil {
		return nil, errors.New("discovery PostgreSQL store requires database, search, and queue dependencies")
	}
	return &Store{pool: pool, search: searchIndex, jobs: jobs, allowLocalFixtures: allowLocalFixtures}, nil
}

func (store *Store) Search(
	ctx context.Context,
	request discovery.SearchRequest,
	vector embedding.Vector,
) (discovery.SearchResponse, error) {
	if _, err := uuid.Parse(request.UserID); err != nil {
		return discovery.SearchResponse{}, fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	if err := search.ValidateQuery(request.Query); err != nil {
		return discovery.SearchResponse{}, fmt.Errorf("%w: %v", discovery.ErrInvalid, err)
	}
	if request.Topic != "" && (len(request.Topic) > 100 || strings.ContainsAny(request.Topic, "\x00\r\n")) {
		return discovery.SearchResponse{}, fmt.Errorf("%w: topic filter is invalid", discovery.ErrInvalid)
	}
	if request.SourceTier != "" && request.SourceTier != "T0" && request.SourceTier != "T1" && request.SourceTier != "T2" && request.SourceTier != "T3" {
		return discovery.SearchResponse{}, fmt.Errorf("%w: source-tier filter is invalid", discovery.ErrInvalid)
	}
	if !validSemanticLifecycle(request.LifecycleState) || !validRadarState(request.RadarState) {
		return discovery.SearchResponse{}, fmt.Errorf("%w: lifecycle or radar filter is invalid", discovery.ErrInvalid)
	}
	if len(request.Action) > 120 || strings.ContainsAny(request.Action, "\x00\r\n") {
		return discovery.SearchResponse{}, fmt.Errorf("%w: action filter is invalid", discovery.ErrInvalid)
	}
	if request.After != nil && request.Before != nil && !request.After.Before(*request.Before) {
		return discovery.SearchResponse{}, fmt.Errorf("%w: search date range must be increasing", discovery.ErrInvalid)
	}
	limit := request.Limit
	if limit == 0 {
		limit = search.DefaultResultLimit
	}
	if limit < 1 || limit > search.MaximumResultLimit {
		return discovery.SearchResponse{}, fmt.Errorf("%w: search limit must be between 1 and 100", discovery.ErrInvalid)
	}
	candidateLimit := limit * 5
	if candidateLimit > search.MaximumResultLimit {
		candidateLimit = search.MaximumResultLimit
	}
	candidates, err := store.search.Search(ctx, searchstore.Request{
		Query: request.Query, QueryEmbedding: vector, SourceTier: request.SourceTier,
		After: request.After, Before: request.Before, Limit: candidateLimit,
	})
	if err != nil {
		return discovery.SearchResponse{}, err
	}
	if len(candidates) == 0 {
		return emptySearchResponse(request.Query), nil
	}
	ids := make([]string, len(candidates))
	ranking := make(map[string]searchstore.Result, len(candidates))
	for index, candidate := range candidates {
		ids[index] = candidate.ItemID
		ranking[candidate.ItemID] = candidate
	}
	rows, err := store.pool.Query(ctx, `
		with candidate_ids as (
			select item_id, ordinal
			from unnest($1::uuid[]) with ordinality as candidate(item_id, ordinal)
		)
		select
			candidate.item_id::text,
			summary.story_id::text,
			summary.headline,
			summary.summary,
			summary.signal,
			summary.recommended_action,
			item.package_name,
			coalesce(extraction.validated_output->>'lifecycle_state', 'unknown'),
			coalesce(state.starred_at is not null or state.location = 'later', false)
		from candidate_ids candidate
		join app.items item on item.id = candidate.item_id
		join app.cluster_members member on member.item_id = item.id
		join app.v_story_summaries summary on summary.story_id = member.cluster_id
		left join app.user_item_states state
			on state.user_id = $2::uuid and state.item_id = item.id
		left join lateral (
			select run.validated_output
			from app.ai_runs run
			where run.item_id = item.id
				and run.purpose = 'structured_extraction'
				and run.state in ('completed', 'needs_review')
			order by run.completed_at desc nulls last, run.id desc
			limit 1
		) extraction on true
		where ($3 = '' or extraction.validated_output->'topic_ids' ? $3)
			and ($4 = '' or extraction.validated_output->>'lifecycle_state' = $4)
			and ($5 = '' or lower(summary.signal) = lower($5)
				or lower(summary.recommended_action) like '%' || lower($5) || '%')
			and ($6::boolean is null or
				coalesce(state.starred_at is not null or state.location = 'later', false) = $6)
			and $7 = ''
		order by candidate.ordinal
		limit $8`, ids, request.UserID, request.Topic, request.LifecycleState,
		request.Action, request.Saved, request.RadarState, limit)
	if err != nil {
		return discovery.SearchResponse{}, fmt.Errorf("enrich hybrid search results: %w", err)
	}
	defer rows.Close()
	results := make([]discovery.SearchResult, 0, limit)
	for rows.Next() {
		var result discovery.SearchResult
		if err := rows.Scan(
			&result.ItemID, &result.StoryID, &result.Title, &result.Summary,
			&result.Signal, &result.RecommendedAction, &result.PackageName,
			&result.LifecycleState, &result.Saved,
		); err != nil {
			return discovery.SearchResponse{}, fmt.Errorf("scan hybrid search result: %w", err)
		}
		candidate := ranking[result.ItemID]
		result.SourceTier = candidate.SourceTier
		result.FirstSeenAt = candidate.FirstSeenAt.UTC()
		result.Score = candidate.Score
		result.Explanation = discovery.SearchExplanation{
			KeywordRank: candidate.KeywordRank, SemanticRank: candidate.SemanticRank,
			SemanticSimilarity: candidate.SemanticSimilarity,
			Summary:            searchExplanation(candidate),
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return discovery.SearchResponse{}, fmt.Errorf("iterate hybrid search results: %w", err)
	}
	return discovery.SearchResponse{
		Query: strings.TrimSpace(request.Query), Results: results, ResultCount: len(results),
		Explanation: "Results combine exact full-text and typo matching with model-versioned semantic retrieval using reciprocal-rank fusion.",
	}, nil
}

func emptySearchResponse(query string) discovery.SearchResponse {
	return discovery.SearchResponse{
		Query: strings.TrimSpace(query), Results: []discovery.SearchResult{}, ResultCount: 0,
		Explanation: "No published story matched the active hybrid index and selected filters.",
	}
}

func searchExplanation(result searchstore.Result) string {
	switch {
	case result.KeywordRank != nil && result.SemanticRank != nil:
		return "Matched both indexed terms and semantic meaning."
	case result.KeywordRank != nil:
		return "Matched indexed title, package, entity, summary, or content terms."
	default:
		return "Matched semantic meaning in the active embedding model."
	}
}

func (store *Store) ListSavedSearches(ctx context.Context, userID string) ([]discovery.SavedSearch, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	rows, err := store.pool.Query(ctx, `
		select id::text, name, query, filters, created_at, updated_at
		from app.saved_searches
		where user_id = $1::uuid
		order by updated_at desc, id desc
		limit 100`, userID)
	if err != nil {
		return nil, fmt.Errorf("list saved searches: %w", err)
	}
	defer rows.Close()
	results := make([]discovery.SavedSearch, 0)
	for rows.Next() {
		value, err := scanSavedSearch(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved searches: %w", err)
	}
	return results, nil
}

func (store *Store) SaveSearch(
	ctx context.Context,
	request discovery.SaveSearchRequest,
	now time.Time,
) (discovery.SavedSearch, error) {
	name := strings.TrimSpace(request.Name)
	query := strings.TrimSpace(request.Query)
	if _, err := uuid.Parse(request.UserID); err != nil || len(name) < 1 || len(name) > 100 || now.IsZero() {
		return discovery.SavedSearch{}, fmt.Errorf("%w: saved search user, name, and time are invalid", discovery.ErrInvalid)
	}
	if err := search.ValidateQuery(query); err != nil || !validSearchFilters(request.Filters) {
		return discovery.SavedSearch{}, fmt.Errorf("%w: saved search query or filters are invalid", discovery.ErrInvalid)
	}
	encodedFilters, err := json.Marshal(request.Filters)
	if err != nil {
		return discovery.SavedSearch{}, fmt.Errorf("encode saved search filters: %w", err)
	}
	row := store.pool.QueryRow(ctx, `
		insert into app.saved_searches (
			user_id, name, query, filters, created_at, updated_at
		) values ($1::uuid, $2, $3, $4::jsonb, $5, $5)
		on conflict (user_id, name) do update set
			query = excluded.query,
			filters = excluded.filters,
			updated_at = excluded.updated_at
		returning id::text, name, query, filters, created_at, updated_at`,
		request.UserID, name, query, encodedFilters, now.UTC())
	return scanSavedSearch(row)
}

func (store *Store) DeleteSavedSearch(ctx context.Context, userID string, savedSearchID string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	if _, err := uuid.Parse(savedSearchID); err != nil {
		return fmt.Errorf("%w: saved search ID must be a UUID", discovery.ErrInvalid)
	}
	result, err := store.pool.Exec(ctx, `
		delete from app.saved_searches where id = $1::uuid and user_id = $2::uuid`,
		savedSearchID, userID)
	if err != nil {
		return fmt.Errorf("delete saved search: %w", err)
	}
	if result.RowsAffected() != 1 {
		return discovery.ErrNotFound
	}
	return nil
}

type savedSearchScanner interface {
	Scan(...any) error
}

func scanSavedSearch(row savedSearchScanner) (discovery.SavedSearch, error) {
	var result discovery.SavedSearch
	var encodedFilters []byte
	if err := row.Scan(
		&result.ID, &result.Name, &result.Query, &encodedFilters, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return discovery.SavedSearch{}, fmt.Errorf("scan saved search: %w", err)
	}
	if err := json.Unmarshal(encodedFilters, &result.Filters); err != nil {
		return discovery.SavedSearch{}, fmt.Errorf("decode saved search filters: %w", err)
	}
	result.CreatedAt = result.CreatedAt.UTC()
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}

func validSearchFilters(filters discovery.SearchFilters) bool {
	after, afterValid := parseSavedSearchTime(filters.After)
	before, beforeValid := parseSavedSearchTime(filters.Before)
	return (filters.SourceTier == "" || filters.SourceTier == "T0" || filters.SourceTier == "T1" || filters.SourceTier == "T2" || filters.SourceTier == "T3") &&
		validSemanticLifecycle(filters.LifecycleState) &&
		validRadarState(filters.RadarState) &&
		(filters.Saved == "" || filters.Saved == "true" || filters.Saved == "false") &&
		len(filters.Topic) <= 100 && len(filters.Action) <= 120 &&
		len(filters.After) <= 35 && len(filters.Before) <= 35 &&
		!strings.ContainsAny(filters.Topic+filters.Action+filters.After+filters.Before, "\x00\r\n") &&
		afterValid && beforeValid &&
		(after.IsZero() || before.IsZero() || !after.After(before))
}

func parseSavedSearchTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, true
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func validSemanticLifecycle(value string) bool {
	switch value {
	case "", "stable", "preview", "release_candidate", "deprecated", "eol", "unknown":
		return true
	default:
		return false
	}
}

func validRadarState(value string) bool {
	switch value {
	case "", "adopt", "trial", "assess", "hold", "reject":
		return true
	default:
		return false
	}
}

func (store *Store) Releases(
	ctx context.Context,
	userID string,
	generatedAt time.Time,
) (discovery.ReleaseCatalog, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return discovery.ReleaseCatalog{}, fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	rows, err := store.pool.Query(ctx, `
		with latest_extraction as (
			select distinct on (run.item_id)
				run.item_id, run.validated_output, run.completed_at
			from app.ai_runs run
			where run.purpose = 'structured_extraction'
				and run.state in ('completed', 'needs_review')
			order by run.item_id, run.completed_at desc nulls last, run.id desc
		), release_dates as (
			select claim.item_id,
				max((claim.normalized_value #>> '{}')::date) filter (
					where claim.claim_type = 'date'
					and (claim.normalized_value #>> '{}') ~ '^\d{4}-\d{2}-\d{2}$'
				) as target_date,
				max(claim.created_at) as verified_at
			from app.claims claim
			group by claim.item_id
		)
		select
			summary.story_id::text,
			coalesce(watched.technology, nullif(item.package_name, ''), summary.headline),
			item.package_name,
			item.version,
			case
				when summary.signal = 'security' then 'security'
				when summary.signal = 'deprecation' then 'deprecation'
				else coalesce(extraction.validated_output->>'lifecycle_state', 'unknown')
			end,
			summary.headline,
			summary.summary,
			summary.source_tier,
			summary.primary_source_url,
			release_dates.target_date,
			coalesce(release_dates.verified_at, extraction.completed_at, summary.brief_created_at),
			coalesce(watched.current_version, '')
		from app.v_story_summaries summary
		join app.items item on item.id = summary.item_id
		left join latest_extraction extraction on extraction.item_id = item.id
		left join release_dates on release_dates.item_id = item.id
		left join app.watched_technologies watched
			on watched.user_id = $1::uuid and watched.package_name = item.package_name
		where (item.event_type = 'release'
			or summary.signal in ('security', 'deprecation')
			or extraction.validated_output->>'lifecycle_state' in ('preview', 'release_candidate', 'deprecated', 'eol'))
			and summary.source_tier in ('T0', 'T1')
		order by coalesce(watched.technology, nullif(item.package_name, ''), summary.headline),
			summary.last_changed_at desc, summary.story_id desc
		limit 500`, userID)
	if err != nil {
		return discovery.ReleaseCatalog{}, fmt.Errorf("select release catalog: %w", err)
	}
	defer rows.Close()
	type groupedTechnology struct {
		current string
		entries []discovery.ReleaseEntry
	}
	groups := make(map[string]groupedTechnology)
	comingSoon := make([]discovery.ReleaseEntry, 0)
	for rows.Next() {
		var entry discovery.ReleaseEntry
		var targetDate pgtype.Date
		var currentVersion string
		if err := rows.Scan(
			&entry.StoryID, &entry.Technology, &entry.PackageName, &entry.Version,
			&entry.State, &entry.Headline, &entry.Summary, &entry.SourceTier,
			&entry.SourceURL, &targetDate, &entry.LastVerifiedAt, &currentVersion,
		); err != nil {
			return discovery.ReleaseCatalog{}, fmt.Errorf("scan release catalog: %w", err)
		}
		entry.LastVerifiedAt = entry.LastVerifiedAt.UTC()
		if targetDate.Valid {
			value := targetDate.Time.UTC()
			entry.TargetDate = &value
		}
		group := groups[entry.Technology]
		group.current = currentVersion
		group.entries = append(group.entries, entry)
		groups[entry.Technology] = group
		if entry.State == "preview" || entry.State == "release_candidate" || entry.State == "deprecation" || entry.State == "deprecated" || entry.State == "eol" {
			comingSoon = append(comingSoon, entry)
		}
	}
	if err := rows.Err(); err != nil {
		return discovery.ReleaseCatalog{}, fmt.Errorf("iterate release catalog: %w", err)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	technologies := make([]discovery.TechnologyRelease, 0, len(names))
	for _, name := range names {
		group := groups[name]
		newest := ""
		for _, entry := range group.entries {
			if entry.Version != "" && (entry.State == "stable" || entry.State == "security") {
				newest = entry.Version
				break
			}
		}
		status := "not-configured"
		if group.current != "" && newest == group.current {
			status = "current"
		} else if group.current != "" && newest != "" {
			status = "update-available"
		}
		packageName := ""
		if len(group.entries) > 0 {
			packageName = group.entries[0].PackageName
		}
		technologies = append(technologies, discovery.TechnologyRelease{
			Technology: name, PackageName: packageName, CurrentVersion: group.current,
			NewestVersion: newest, UpgradeStatus: status, Entries: group.entries,
		})
	}
	return discovery.ReleaseCatalog{
		GeneratedAt: generatedAt.UTC(), Technologies: technologies, ComingSoon: comingSoon,
	}, nil
}

func (store *Store) ImportURL(
	ctx context.Context,
	request discovery.ManualCaptureRequest,
) (discovery.ManualCapture, error) {
	if _, err := uuid.Parse(request.UserID); err != nil || len(strings.TrimSpace(request.IdempotencyKey)) < 16 || len(request.IdempotencyKey) > 200 {
		return discovery.ManualCapture{}, fmt.Errorf("%w: user and idempotency key are required", discovery.ErrInvalid)
	}
	canonicalURL, err := discovery.CanonicalCaptureURL(request.URL)
	if err != nil {
		return discovery.ManualCapture{}, err
	}
	parsed, _ := url.Parse(canonicalURL)
	if parsed.Scheme == "http" && !store.allowLocalFixtures {
		return discovery.ManualCapture{}, fmt.Errorf("%w: local fixture URLs are disabled in this environment", discovery.ErrInvalid)
	}
	digest := sha256.Sum256([]byte(canonicalURL))
	identifier := hex.EncodeToString(digest[:8])
	sourceID := "owner-manual-" + identifier
	registryID := "owner-manual-" + identifier

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return discovery.ManualCapture{}, fmt.Errorf("begin manual capture: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if existing, found, selectErr := selectManualCapture(ctx, tx, request.UserID, request.IdempotencyKey); selectErr != nil {
		return discovery.ManualCapture{}, selectErr
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return discovery.ManualCapture{}, fmt.Errorf("commit idempotent manual capture: %w", err)
		}
		return existing, nil
	}
	if _, err := tx.Exec(ctx, `
		insert into app.sources (
			id, name, trust_tier, owner, origin, validation_state,
			homepage_url, content_policy, enabled, topics, reviewed_at
		) values ($1, $2, 'T2', $3, 'owner', 'pending', $4,
			'link-and-excerpt', false, array['manual'], now())
		on conflict (id) do nothing`, sourceID, parsed.Hostname(), "owner:"+request.UserID, canonicalURL); err != nil {
		return discovery.ManualCapture{}, fmt.Errorf("create manual-capture source: %w", err)
	}
	var endpointID string
	if err := tx.QueryRow(ctx, `
		insert into app.source_endpoints (
			registry_id, source_id, connector, url, poll_interval, priority,
			robots_policy, expected_content_types, max_response_bytes,
			fixture_suite, config, health_state
		) values ($1, $2, 'page', $3, interval '7 days', 'normal', 'page',
			array['text/html', 'application/xhtml+xml'], 15728640, '', '{}'::jsonb, 'unverified')
		on conflict (registry_id) do update set updated_at = now()
		returning id::text`, registryID, sourceID, canonicalURL).Scan(&endpointID); err != nil {
		return discovery.ManualCapture{}, fmt.Errorf("create manual-capture endpoint: %w", err)
	}
	var capture discovery.ManualCapture
	if err := tx.QueryRow(ctx, `
		insert into app.manual_captures (
			user_id, idempotency_key, requested_url, canonical_url,
			source_id, endpoint_id
		) values ($1::uuid, $2, $3, $4, $5, $6::uuid)
		returning id::text, canonical_url, state, created_at`, request.UserID,
		request.IdempotencyKey, strings.TrimSpace(request.URL), canonicalURL,
		sourceID, endpointID).Scan(&capture.ID, &capture.URL, &capture.State, &capture.CreatedAt); err != nil {
		return discovery.ManualCapture{}, fmt.Errorf("record manual capture: %w", err)
	}
	if _, _, err := store.jobs.EnqueueManualCapture(ctx, tx, jobqueue.ProcessManualCaptureArgs{CaptureID: capture.ID}); err != nil {
		return discovery.ManualCapture{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return discovery.ManualCapture{}, fmt.Errorf("commit manual capture: %w", err)
	}
	capture.CreatedAt = capture.CreatedAt.UTC()
	return capture, nil
}

func selectManualCapture(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	idempotencyKey string,
) (discovery.ManualCapture, bool, error) {
	var capture discovery.ManualCapture
	var storyID pgtype.Text
	var errorCode pgtype.Text
	var completedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
		select capture.id::text, capture.canonical_url, capture.state,
			cluster.id::text, capture.error_code, capture.created_at, capture.completed_at
		from app.manual_captures capture
		left join app.story_clusters cluster on cluster.id = capture.cluster_id
		where capture.user_id = $1::uuid and capture.idempotency_key = $2
		for update of capture`, userID, idempotencyKey).Scan(
		&capture.ID, &capture.URL, &capture.State, &storyID,
		&errorCode, &capture.CreatedAt, &completedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return discovery.ManualCapture{}, false, nil
	}
	if err != nil {
		return discovery.ManualCapture{}, false, fmt.Errorf("select idempotent manual capture: %w", err)
	}
	if storyID.Valid {
		capture.StoryID = storyID.String
	}
	if errorCode.Valid {
		capture.ErrorCode = errorCode.String
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		capture.CompletedAt = &value
	}
	capture.CreatedAt = capture.CreatedAt.UTC()
	return capture, true, nil
}

type importCandidateRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Connector   string `json:"connector"`
	Duplicate   bool   `json:"duplicate"`
	Valid       bool   `json:"valid"`
	Explanation string `json:"explanation"`
}

func (store *Store) PreviewOPML(
	ctx context.Context,
	userID string,
	payload []byte,
	now time.Time,
) (discovery.ImportPreview, error) {
	if _, err := uuid.Parse(userID); err != nil || now.IsZero() {
		return discovery.ImportPreview{}, fmt.Errorf("%w: valid user and time are required", discovery.ErrInvalid)
	}
	candidates, digest, err := discovery.ParseOPML(payload)
	if err != nil {
		return discovery.ImportPreview{}, err
	}
	urls := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.URL != "" {
			urls = append(urls, candidate.URL)
		}
	}
	rows, err := store.pool.Query(ctx, `
		select url from app.source_endpoints where url = any($1::text[])`, urls)
	if err != nil {
		return discovery.ImportPreview{}, fmt.Errorf("detect imported-source duplicates: %w", err)
	}
	duplicates := make(map[string]struct{})
	for rows.Next() {
		var sourceURL string
		if err := rows.Scan(&sourceURL); err != nil {
			rows.Close()
			return discovery.ImportPreview{}, fmt.Errorf("scan imported-source duplicate: %w", err)
		}
		duplicates[sourceURL] = struct{}{}
	}
	rows.Close()
	records := make([]importCandidateRecord, 0, len(candidates))
	responseCandidates := make([]discovery.ImportCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		_, duplicate := duplicates[candidate.URL]
		candidate.Duplicate = duplicate
		if duplicate {
			candidate.Valid = false
			candidate.Explanation = "This endpoint already exists and will not be imported again."
		}
		responseCandidates = append(responseCandidates, candidate)
		records = append(records, importCandidateRecord(candidate))
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return discovery.ImportPreview{}, fmt.Errorf("encode OPML preview: %w", err)
	}
	expiresAt := now.UTC().Add(importPreviewLifetime)
	var previewID string
	if err := store.pool.QueryRow(ctx, `
		insert into app.source_import_previews (
			user_id, document_sha256, candidates, created_at, expires_at
		) values ($1::uuid, $2, $3::jsonb, $4, $5)
		returning id::text`, userID, digest[:], encoded, now.UTC(), expiresAt).Scan(&previewID); err != nil {
		return discovery.ImportPreview{}, fmt.Errorf("persist OPML preview: %w", err)
	}
	return discovery.ImportPreview{ID: previewID, Candidates: responseCandidates, ExpiresAt: expiresAt}, nil
}

func (store *Store) CommitOPML(
	ctx context.Context,
	request discovery.ImportCommitRequest,
	now time.Time,
) (discovery.ImportCommitResult, error) {
	if _, err := uuid.Parse(request.UserID); err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	if _, err := uuid.Parse(request.PreviewID); err != nil || len(request.ApprovedIDs) == 0 || len(request.ApprovedIDs) > 500 || now.IsZero() {
		return discovery.ImportCommitResult{}, fmt.Errorf("%w: preview and approved source IDs are required", discovery.ErrInvalid)
	}
	approved := make(map[string]struct{}, len(request.ApprovedIDs))
	for _, id := range request.ApprovedIDs {
		if id == "" {
			return discovery.ImportCommitResult{}, fmt.Errorf("%w: approved source ID is empty", discovery.ErrInvalid)
		}
		approved[id] = struct{}{}
	}
	if len(approved) != len(request.ApprovedIDs) {
		return discovery.ImportCommitResult{}, fmt.Errorf("%w: approved source IDs contain duplicates", discovery.ErrInvalid)
	}

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("begin OPML commit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var candidatesJSON []byte
	var expiresAt time.Time
	var committedAt pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
		select candidates, expires_at, committed_at
		from app.source_import_previews
		where id = $1::uuid and user_id = $2::uuid
		for update`, request.PreviewID, request.UserID).Scan(
		&candidatesJSON, &expiresAt, &committedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return discovery.ImportCommitResult{}, discovery.ErrNotFound
	} else if err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("select OPML preview: %w", err)
	}
	if committedAt.Valid || !now.UTC().Before(expiresAt.UTC()) {
		return discovery.ImportCommitResult{}, discovery.ErrConflict
	}
	var candidates []importCandidateRecord
	if err := json.Unmarshal(candidatesJSON, &candidates); err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("decode OPML preview: %w", err)
	}
	byID := make(map[string]importCandidateRecord, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	created := make([]string, 0, len(approved))
	for id := range approved {
		candidate, exists := byID[id]
		if !exists || !candidate.Valid || candidate.Duplicate {
			return discovery.ImportCommitResult{}, fmt.Errorf("%w: source %q is not eligible for approval", discovery.ErrInvalid, id)
		}
		sourceID := "owner-opml-" + id
		if _, err := tx.Exec(ctx, `
			insert into app.sources (
				id, name, trust_tier, owner, origin, validation_state,
				homepage_url, content_policy, enabled, topics, reviewed_at
			) values ($1, $2, 'T2', $3, 'owner', 'pending', $4,
				'link-and-excerpt', false, array['imported'], $5)`,
			sourceID, candidate.Name, "owner:"+request.UserID, candidate.URL, now.UTC()); err != nil {
			return discovery.ImportCommitResult{}, fmt.Errorf("create pending OPML source: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into app.source_endpoints (
				registry_id, source_id, connector, url, poll_interval, priority,
				robots_policy, expected_content_types, max_response_bytes,
				fixture_suite, config, health_state
			) values ($1, $2, $3, $4, interval '6 hours', 'normal', 'feed',
				$5, 5242880, '', '{}'::jsonb, 'unverified')`,
			sourceID, sourceID, candidate.Connector, candidate.URL,
			expectedContentTypes(candidate.Connector)); err != nil {
			return discovery.ImportCommitResult{}, fmt.Errorf("create pending OPML endpoint: %w", err)
		}
		created = append(created, sourceID)
	}
	sort.Strings(created)
	if _, err := tx.Exec(ctx, `
		update app.source_import_previews set committed_at = $3
		where id = $1::uuid and user_id = $2::uuid`, request.PreviewID, request.UserID, now.UTC()); err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("mark OPML preview committed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return discovery.ImportCommitResult{}, fmt.Errorf("commit OPML sources: %w", err)
	}
	return discovery.ImportCommitResult{ImportedCount: len(created), PendingSourceIDs: created}, nil
}

func expectedContentTypes(connector string) []string {
	switch connector {
	case "atom":
		return []string{"application/atom+xml", "application/xml", "text/xml"}
	case "json_feed":
		return []string{"application/feed+json", "application/json"}
	default:
		return []string{"application/rss+xml", "application/xml", "text/xml"}
	}
}

func (store *Store) ExportOPML(ctx context.Context, userID string) (discovery.Export, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return discovery.Export{}, fmt.Errorf("%w: user ID must be a UUID", discovery.ErrInvalid)
	}
	rows, err := store.pool.Query(ctx, `
		select source.name, endpoint.connector, endpoint.url, source.homepage_url
		from app.sources source
		join app.source_endpoints endpoint on endpoint.source_id = source.id
		where source.origin = 'owner' and source.owner = $1
		order by source.name, endpoint.url`, "owner:"+userID)
	if err != nil {
		return discovery.Export{}, fmt.Errorf("select OPML sources: %w", err)
	}
	defer rows.Close()
	type outline struct {
		XMLName xml.Name `xml:"outline"`
		Text    string   `xml:"text,attr"`
		Title   string   `xml:"title,attr"`
		Type    string   `xml:"type,attr"`
		XMLURL  string   `xml:"xmlUrl,attr"`
		HTMLURL string   `xml:"htmlUrl,attr,omitempty"`
	}
	type document struct {
		XMLName xml.Name `xml:"opml"`
		Version string   `xml:"version,attr"`
		Head    struct {
			Title string `xml:"title"`
		} `xml:"head"`
		Body struct {
			Outlines []outline `xml:"outline"`
		} `xml:"body"`
	}
	value := document{Version: "2.0"}
	value.Head.Title = "Relantern owner sources"
	for rows.Next() {
		var entry outline
		var connector string
		if err := rows.Scan(&entry.Title, &connector, &entry.XMLURL, &entry.HTMLURL); err != nil {
			return discovery.Export{}, fmt.Errorf("scan OPML source: %w", err)
		}
		entry.Text = entry.Title
		entry.Type = connectorType(connector)
		value.Body.Outlines = append(value.Body.Outlines, entry)
	}
	if err := rows.Err(); err != nil {
		return discovery.Export{}, fmt.Errorf("iterate OPML sources: %w", err)
	}
	payload, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		return discovery.Export{}, fmt.Errorf("encode OPML export: %w", err)
	}
	payload = append([]byte(xml.Header), payload...)
	payload = append(payload, '\n')
	return discovery.Export{ContentType: "text/x-opml; charset=utf-8", Filename: "relantern-sources.opml", Payload: payload}, nil
}

func connectorType(connector string) string {
	switch connector {
	case "atom":
		return "atom"
	case "json_feed":
		return "jsonfeed"
	default:
		return "rss"
	}
}

func (store *Store) ExportMetadata(
	ctx context.Context,
	request discovery.MetadataExportRequest,
) (discovery.Export, error) {
	if _, err := uuid.Parse(request.UserID); err != nil || (request.Format != "json" && request.Format != "csv") {
		return discovery.Export{}, fmt.Errorf("%w: metadata export requires a user and json or csv format", discovery.ErrInvalid)
	}
	rows, err := store.pool.Query(ctx, `
		select summary.story_id::text, summary.headline, summary.signal,
			summary.source_tier, summary.primary_source_url, summary.first_seen_at,
			coalesce(state.location, 'inbox'), coalesce(state.is_read, false),
			state.starred_at is not null
		from app.v_story_summaries summary
		left join app.user_item_states state
			on state.user_id = $1::uuid and state.item_id = summary.item_id
		order by summary.first_seen_at desc, summary.story_id desc
		limit 10000`, request.UserID)
	if err != nil {
		return discovery.Export{}, fmt.Errorf("select metadata export: %w", err)
	}
	defer rows.Close()
	type record struct {
		StoryID          string    `json:"storyId"`
		Headline         string    `json:"headline"`
		Signal           string    `json:"signal"`
		SourceTier       string    `json:"sourceTier"`
		PrimarySourceURL string    `json:"primarySourceUrl"`
		FirstSeenAt      time.Time `json:"firstSeenAt"`
		Location         string    `json:"location"`
		Read             bool      `json:"read"`
		Starred          bool      `json:"starred"`
	}
	records := make([]record, 0)
	for rows.Next() {
		var value record
		if err := rows.Scan(&value.StoryID, &value.Headline, &value.Signal, &value.SourceTier,
			&value.PrimarySourceURL, &value.FirstSeenAt, &value.Location, &value.Read, &value.Starred); err != nil {
			return discovery.Export{}, fmt.Errorf("scan metadata export: %w", err)
		}
		value.FirstSeenAt = value.FirstSeenAt.UTC()
		records = append(records, value)
	}
	if err := rows.Err(); err != nil {
		return discovery.Export{}, fmt.Errorf("iterate metadata export: %w", err)
	}
	if request.Format == "json" {
		payload, err := json.MarshalIndent(struct {
			Stories []record `json:"stories"`
		}{Stories: records}, "", "  ")
		if err != nil {
			return discovery.Export{}, fmt.Errorf("encode JSON metadata export: %w", err)
		}
		payload = append(payload, '\n')
		return discovery.Export{ContentType: "application/json", Filename: "relantern-metadata.json", Payload: payload}, nil
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"story_id", "headline", "signal", "source_tier", "primary_source_url", "first_seen_at", "location", "read", "starred"}); err != nil {
		return discovery.Export{}, fmt.Errorf("write CSV metadata header: %w", err)
	}
	for _, value := range records {
		if err := writer.Write([]string{
			value.StoryID, value.Headline, value.Signal, value.SourceTier,
			value.PrimarySourceURL, value.FirstSeenAt.Format(time.RFC3339), value.Location,
			fmt.Sprintf("%t", value.Read), fmt.Sprintf("%t", value.Starred),
		}); err != nil {
			return discovery.Export{}, fmt.Errorf("write CSV metadata record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return discovery.Export{}, fmt.Errorf("encode CSV metadata export: %w", err)
	}
	return discovery.Export{ContentType: "text/csv; charset=utf-8", Filename: "relantern-metadata.csv", Payload: buffer.Bytes()}, nil
}

func (store *Store) ExportMarkdown(
	ctx context.Context,
	request discovery.MarkdownExportRequest,
) (discovery.Export, error) {
	if _, err := uuid.Parse(request.UserID); err != nil || len(request.StoryIDs) == 0 || len(request.StoryIDs) > discovery.MaximumExportStoryIDs {
		return discovery.Export{}, fmt.Errorf("%w: Markdown export requires 1 through %d stories", discovery.ErrInvalid, discovery.MaximumExportStoryIDs)
	}
	seen := make(map[string]struct{}, len(request.StoryIDs))
	for _, storyID := range request.StoryIDs {
		if _, err := uuid.Parse(storyID); err != nil {
			return discovery.Export{}, fmt.Errorf("%w: story IDs must be UUIDs", discovery.ErrInvalid)
		}
		if _, duplicate := seen[storyID]; duplicate {
			return discovery.Export{}, fmt.Errorf("%w: story IDs contain duplicates", discovery.ErrInvalid)
		}
		seen[storyID] = struct{}{}
	}
	rows, err := store.pool.Query(ctx, `
		select summary.story_id::text, summary.headline, summary.summary,
			summary.why_it_matters, summary.recommended_action,
			summary.primary_source_url,
			coalesce((select string_agg('- ' || annotation.body, E'\n' order by annotation.created_at)
				from app.annotations annotation
				where annotation.user_id = $1::uuid and annotation.item_id = summary.item_id), '')
		from unnest($2::uuid[]) with ordinality requested(story_id, ordinal)
		join app.v_story_summaries summary on summary.story_id = requested.story_id
		order by requested.ordinal`, request.UserID, request.StoryIDs)
	if err != nil {
		return discovery.Export{}, fmt.Errorf("select Markdown export: %w", err)
	}
	defer rows.Close()
	var buffer strings.Builder
	buffer.WriteString("# Relantern export\n\n")
	count := 0
	for rows.Next() {
		var storyID string
		var headline string
		var summary string
		var why string
		var action string
		var sourceURL string
		var annotations string
		if err := rows.Scan(&storyID, &headline, &summary, &why, &action, &sourceURL, &annotations); err != nil {
			return discovery.Export{}, fmt.Errorf("scan Markdown export: %w", err)
		}
		count++
		fmt.Fprintf(&buffer, "## %s\n\n%s\n\n### Why it matters\n\n%s\n\n### Recommended action\n\n%s\n\n### Citation\n\n- [%s](%s)\n\n", cleanMarkdownHeading(headline), summary, why, action, sourceURL, sourceURL)
		if annotations != "" {
			fmt.Fprintf(&buffer, "### Notes and highlights\n\n%s\n\n", annotations)
		}
		fmt.Fprintf(&buffer, "<!-- story-id: %s -->\n\n", storyID)
	}
	if err := rows.Err(); err != nil {
		return discovery.Export{}, fmt.Errorf("iterate Markdown export: %w", err)
	}
	if count != len(request.StoryIDs) {
		return discovery.Export{}, discovery.ErrNotFound
	}
	return discovery.Export{ContentType: "text/markdown; charset=utf-8", Filename: "relantern-stories.md", Payload: []byte(buffer.String())}, nil
}

func cleanMarkdownHeading(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "#", "\\#").Replace(strings.TrimSpace(value))
}
