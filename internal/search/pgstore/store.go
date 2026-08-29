package pgstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/search"
)

type Store struct {
	pool       *pgxpool.Pool
	dimensions int
	rrfK       int
}

type IndexRequest struct {
	ItemID            string
	RevisionID        string
	Summary           string
	EntityNames       []string
	NormalizedContent string
}

type Request struct {
	Query          string
	QueryEmbedding embedding.Vector
	LifecycleState string
	SourceTier     string
	After          *time.Time
	Before         *time.Time
	Limit          int
}

type Result struct {
	ItemID             string
	Title              string
	PackageName        string
	SourceTier         string
	LifecycleState     string
	FirstSeenAt        time.Time
	Score              float64
	KeywordRank        *int64
	SemanticRank       *int64
	SemanticSimilarity *float64
}

func New(pool *pgxpool.Pool, dimensions int, rrfK int) (*Store, error) {
	if pool == nil {
		return nil, errors.New("search store requires a database pool")
	}
	if dimensions != embedding.DefaultDimensions {
		return nil, fmt.Errorf("search index currently requires %d dimensions", embedding.DefaultDimensions)
	}
	if rrfK < 1 || rrfK > 1000 {
		return nil, errors.New("search RRF k must be between 1 and 1000")
	}
	return &Store{pool: pool, dimensions: dimensions, rrfK: rrfK}, nil
}

func (store *Store) IndexDocument(ctx context.Context, request IndexRequest) error {
	if strings.TrimSpace(request.ItemID) == "" || strings.TrimSpace(request.RevisionID) == "" {
		return errors.New("search index item and revision IDs are required")
	}
	if strings.TrimSpace(request.NormalizedContent) == "" || len(request.NormalizedContent) > 1_000_000 {
		return errors.New("search normalized content must contain between 1 and 1000000 characters")
	}
	if len(request.Summary) > 10_000 {
		return errors.New("search summary may not exceed 10000 characters")
	}
	entityNames := append([]string(nil), request.EntityNames...)
	sort.Strings(entityNames)
	entityText := strings.Join(entityNames, " ")
	if len(entityText) > 10_000 {
		return errors.New("search entity text may not exceed 10000 characters")
	}
	result, err := store.pool.Exec(ctx, `
		insert into app.search_documents (
			item_id,
			revision_id,
			title,
			summary,
			entity_text,
			package_name,
			normalized_content,
			source_tier,
			lifecycle_state,
			published_at,
			first_seen_at
		)
		select
			item.id,
			item.current_revision_id,
			item.title,
			$3,
			$4,
			item.package_name,
			$5,
			primary_source.source_tier,
			item.lifecycle_state,
			item.published_at,
			item.first_seen_at
		from app.items item
		join lateral (
			select source_tier
			from app.item_sources
			where item_id = item.id and source_role = 'primary'
			limit 1
		) primary_source on true
		where item.id = $1::uuid and item.current_revision_id = $2::uuid
		on conflict (item_id) do update set
			revision_id = excluded.revision_id,
			title = excluded.title,
			summary = excluded.summary,
			entity_text = excluded.entity_text,
			package_name = excluded.package_name,
			normalized_content = excluded.normalized_content,
			source_tier = excluded.source_tier,
			lifecycle_state = excluded.lifecycle_state,
			published_at = excluded.published_at,
			first_seen_at = excluded.first_seen_at,
			updated_at = now()`,
		request.ItemID,
		request.RevisionID,
		strings.TrimSpace(request.Summary),
		entityText,
		strings.TrimSpace(request.NormalizedContent),
	)
	if err != nil {
		return fmt.Errorf("index search document: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("search document requires the item's current revision and primary source")
	}
	return nil
}

func (store *Store) Search(ctx context.Context, request Request) ([]Result, error) {
	if err := search.ValidateQuery(request.Query); err != nil {
		return nil, err
	}
	if err := embedding.ValidateVector(request.QueryEmbedding, store.dimensions); err != nil {
		return nil, err
	}
	if request.LifecycleState != "" && !validLifecycleState(request.LifecycleState) {
		return nil, errors.New("unsupported search lifecycle filter")
	}
	if request.SourceTier != "" && request.SourceTier != "T0" && request.SourceTier != "T1" && request.SourceTier != "T2" && request.SourceTier != "T3" {
		return nil, errors.New("unsupported search source-tier filter")
	}
	if request.After != nil && request.Before != nil && !request.After.Before(*request.Before) {
		return nil, errors.New("search date range must be increasing")
	}
	limit := request.Limit
	if limit == 0 {
		limit = search.DefaultResultLimit
	}
	if limit < 1 || limit > search.MaximumResultLimit {
		return nil, errors.New("search limit must be between 1 and 100")
	}
	candidateLimit := limit * 10
	if candidateLimit < 50 {
		candidateLimit = 50
	}
	if candidateLimit > 500 {
		candidateLimit = 500
	}

	var activeModelID string
	var activeDimensions int
	if err := store.pool.QueryRow(ctx, `
		select model_id, dimensions
		from app.embedding_models
		where lifecycle_state = 'active'`).Scan(&activeModelID, &activeDimensions); err != nil {
		return nil, fmt.Errorf("select active embedding model: %w", err)
	}
	if activeDimensions != store.dimensions {
		return nil, errors.New("active embedding model dimensions do not match the search index")
	}
	rows, err := store.pool.Query(ctx, hybridSearchSQL,
		strings.TrimSpace(request.Query),
		activeModelID,
		store.dimensions,
		embeddingstore.VectorLiteral(request.QueryEmbedding),
		store.rrfK,
		request.LifecycleState,
		request.SourceTier,
		request.After,
		request.Before,
		candidateLimit,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("run hybrid search: %w", err)
	}
	defer rows.Close()
	results := make([]Result, 0, limit)
	for rows.Next() {
		var result Result
		var keywordRank pgtype.Int8
		var semanticRank pgtype.Int8
		var semanticSimilarity pgtype.Float8
		if err := rows.Scan(
			&result.ItemID,
			&result.Title,
			&result.PackageName,
			&result.SourceTier,
			&result.LifecycleState,
			&result.FirstSeenAt,
			&result.Score,
			&keywordRank,
			&semanticRank,
			&semanticSimilarity,
		); err != nil {
			return nil, fmt.Errorf("scan hybrid search result: %w", err)
		}
		if keywordRank.Valid {
			value := keywordRank.Int64
			result.KeywordRank = &value
		}
		if semanticRank.Valid {
			value := semanticRank.Int64
			result.SemanticRank = &value
		}
		if semanticSimilarity.Valid {
			value := semanticSimilarity.Float64
			result.SemanticSimilarity = &value
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hybrid search results: %w", err)
	}
	return results, nil
}

func validLifecycleState(state string) bool {
	switch state {
	case "discovered", "fetched", "normalized", "duplicate", "clustered",
		"awaiting_ai", "extracting", "needs_review", "ready", "published",
		"suppressed", "failed_retryable", "failed_terminal":
		return true
	default:
		return false
	}
}

const hybridSearchSQL = `
with keyword_query as (
	select websearch_to_tsquery('english', $1) as query
),
keyword_scored as (
	select
		document.item_id,
		(
			case when lower(document.package_name) = lower($1) then 4.0 else 0.0 end
			+ case when lower(document.title) = lower($1) then 3.0 else 0.0 end
			+ ts_rank_cd(document.search_vector, keyword_query.query, 32)
			+ similarity(lower(document.title), lower($1))
			+ similarity(lower(document.package_name), lower($1))
		) as keyword_score,
		document.first_seen_at
	from app.search_documents document
	cross join keyword_query
	where
		($6 = '' or document.lifecycle_state = $6)
		and ($7 = '' or document.source_tier = $7)
		and ($8::timestamptz is null or document.first_seen_at >= $8)
		and ($9::timestamptz is null or document.first_seen_at < $9)
		and (
			document.search_vector @@ keyword_query.query
			or lower(document.title) % lower($1)
			or lower(document.package_name) % lower($1)
		)
),
keyword as (
	select
		item_id,
		row_number() over (
			order by keyword_score desc, first_seen_at desc, item_id
		)::bigint as rank
	from keyword_scored
	order by keyword_score desc, first_seen_at desc, item_id
	limit $10
),
semantic_scored as (
	select
		embedding.entity_id as item_id,
		(embedding.embedding::vector(1536) <=> $4::vector(1536)) as distance,
		document.first_seen_at
	from app.embeddings embedding
	join app.search_documents document on document.item_id = embedding.entity_id
	where
		embedding.entity_type = 'item'
		and embedding.model_id = $2
		and embedding.dimensions = $3
		and ($6 = '' or document.lifecycle_state = $6)
		and ($7 = '' or document.source_tier = $7)
		and ($8::timestamptz is null or document.first_seen_at >= $8)
		and ($9::timestamptz is null or document.first_seen_at < $9)
		and not exists (
			select 1
			from app.embeddings newer
			where newer.entity_type = embedding.entity_type
				and newer.entity_id = embedding.entity_id
				and newer.model_id = embedding.model_id
				and (newer.created_at, newer.id) > (embedding.created_at, embedding.id)
		)
	order by embedding.embedding::vector(1536) <=> $4::vector(1536), embedding.entity_id
	limit $10
),
semantic as (
	select
		item_id,
		row_number() over (
			order by distance, first_seen_at desc, item_id
		)::bigint as rank,
		1.0 - distance as similarity
	from semantic_scored
),
candidate_ids as (
	select item_id from keyword
	union
	select item_id from semantic
),
fused as (
	select
		candidate_ids.item_id,
		coalesce(1.0 / ($5 + keyword.rank), 0.0)
			+ coalesce(1.0 / ($5 + semantic.rank), 0.0) as score,
		keyword.rank as keyword_rank,
		semantic.rank as semantic_rank,
		semantic.similarity as semantic_similarity
	from candidate_ids
	left join keyword using (item_id)
	left join semantic using (item_id)
)
select
	document.item_id::text,
	document.title,
	document.package_name,
	document.source_tier,
	document.lifecycle_state,
	document.first_seen_at,
	fused.score,
	fused.keyword_rank,
	fused.semantic_rank,
	fused.semantic_similarity
from fused
join app.search_documents document using (item_id)
order by fused.score desc,
	least(coalesce(fused.keyword_rank, 9223372036854775807), coalesce(fused.semantic_rank, 9223372036854775807)),
	document.first_seen_at desc,
	document.item_id
limit $11
`
