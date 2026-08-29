package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/dedupe"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
)

const candidateLimit = 2000

type transactionStarter interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Store struct {
	database transactionStarter
	clock    clock.Clock
	config   dedupe.Config
}

type storedDocument struct {
	dedupe.Document
	FetchedCanonicalURL string
}

func New(database transactionStarter, configuredClock clock.Clock, config dedupe.Config) (*Store, error) {
	if database == nil || configuredClock == nil {
		return nil, errors.New("dedupe PostgreSQL store requires a database and clock")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate dedupe configuration: %w", err)
	}
	return &Store{database: database, clock: configuredClock, config: config}, nil
}

func (store *Store) Process(ctx context.Context, request dedupe.ProcessRequest) (dedupe.ProcessResult, error) {
	if _, err := uuid.Parse(request.RevisionID); err != nil {
		return dedupe.ProcessResult{}, errors.New("dedupe revision id must be a UUID")
	}
	if strings.TrimSpace(request.NormalizedText) == "" {
		return dedupe.ProcessResult{}, errors.New("dedupe normalized text is required")
	}
	hasEmbeddingModel := strings.TrimSpace(request.EmbeddingModelID) != ""
	hasEmbedding := len(request.Embedding) > 0
	if hasEmbeddingModel != hasEmbedding {
		return dedupe.ProcessResult{}, errors.New("dedupe embedding model and vector must be supplied together")
	}
	if hasEmbedding {
		if err := embedding.ValidateVector(request.Embedding, embedding.DefaultDimensions); err != nil {
			return dedupe.ProcessResult{}, fmt.Errorf("validate dedupe embedding: %w", err)
		}
	}

	// The transaction-scoped advisory lock serializes all candidate reads and
	// writes. Read committed is intentional: a waiter must observe the decision
	// committed by the lock holder instead of retaining a pre-wait snapshot.
	transaction, err := store.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return dedupe.ProcessResult{}, fmt.Errorf("begin dedupe transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `
		select pg_advisory_xact_lock(hashtextextended('relantern:dedupe-cluster', 0))`); err != nil {
		return dedupe.ProcessResult{}, fmt.Errorf("lock dedupe decisions: %w", err)
	}
	if hasEmbedding {
		if err := validateEmbeddingModel(ctx, transaction, request.EmbeddingModelID); err != nil {
			return dedupe.ProcessResult{}, err
		}
	}

	if existing, found, err := existingDecision(ctx, transaction, request.RevisionID); err != nil {
		return dedupe.ProcessResult{}, err
	} else if found {
		existing.Idempotent = true
		if err := transaction.Commit(ctx); err != nil {
			return dedupe.ProcessResult{}, fmt.Errorf("commit idempotent dedupe decision: %w", err)
		}
		return existing, nil
	}

	document, canonicalAccepted, err := loadDocument(ctx, transaction, request)
	if err != nil {
		return dedupe.ProcessResult{}, err
	}
	candidates, err := loadCandidates(
		ctx,
		transaction,
		document,
		store.config.ClusterMaxAge,
		request.EmbeddingModelID,
		request.Embedding,
	)
	if err != nil {
		return dedupe.ProcessResult{}, err
	}
	decision := dedupe.Decide(document.Document, candidates, store.config)
	evaluatedAt := store.clock.Now().UTC()

	var result dedupe.ProcessResult
	switch decision.Outcome {
	case dedupe.OutcomeCreate:
		result, err = createItemAndCluster(ctx, transaction, document.Document, decision, evaluatedAt)
	case dedupe.OutcomeCluster:
		result, err = createClusterMember(ctx, transaction, document.Document, decision, evaluatedAt)
	case dedupe.OutcomeDuplicate, dedupe.OutcomeRevision:
		result, err = attachRevision(ctx, transaction, document.Document, decision, evaluatedAt)
	default:
		err = fmt.Errorf("unsupported dedupe outcome %q", decision.Outcome)
	}
	if err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := persistEmbedding(ctx, transaction, document.Document, result.ItemID, request); err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := recordDecision(ctx, transaction, document.Document, result, decision, canonicalAccepted, store.config, evaluatedAt); err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := refreshClusterPrimary(ctx, transaction, result.ClusterID, evaluatedAt); err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return dedupe.ProcessResult{}, fmt.Errorf("commit dedupe decision: %w", err)
	}
	return result, nil
}

func existingDecision(ctx context.Context, transaction pgx.Tx, revisionID string) (dedupe.ProcessResult, bool, error) {
	var result dedupe.ProcessResult
	var outcome string
	var method string
	err := transaction.QueryRow(ctx, `
		select item_id::text, cluster_id::text, outcome, method, similarity::double precision
		from app.dedupe_decisions
		where revision_id = $1::uuid`, revisionID).Scan(
		&result.ItemID,
		&result.ClusterID,
		&outcome,
		&method,
		&result.Similarity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return dedupe.ProcessResult{}, false, nil
	}
	if err != nil {
		return dedupe.ProcessResult{}, false, fmt.Errorf("select existing dedupe decision: %w", err)
	}
	result.Outcome = dedupe.Outcome(outcome)
	result.Method = dedupe.Method(method)
	return result, true, nil
}

func loadDocument(
	ctx context.Context,
	transaction pgx.Tx,
	request dedupe.ProcessRequest,
) (storedDocument, bool, error) {
	var document storedDocument
	var rawDigest []byte
	var normalizedDigest []byte
	var publishedAt pgtype.Timestamptz
	err := transaction.QueryRow(ctx, `
		select
			revision.id::text,
			raw.source_id,
			source.trust_tier,
			raw.canonical_url,
			raw.raw_sha256,
			revision.normalized_sha256,
			revision.title,
			revision.author,
			coalesce(revision.source_published_at, raw.source_published_at),
			raw.first_seen_at
		from app.content_revisions revision
		join app.raw_documents raw on raw.id = revision.raw_document_id
		join app.sources source on source.id = raw.source_id
		where revision.id = $1::uuid`, request.RevisionID).Scan(
		&document.RevisionID,
		&document.SourceID,
		&document.SourceTier,
		&document.FetchedCanonicalURL,
		&rawDigest,
		&normalizedDigest,
		&document.Title,
		&document.Author,
		&publishedAt,
		&document.FirstSeenAt,
	)
	if err != nil {
		return storedDocument{}, false, fmt.Errorf("select dedupe revision %q: %w", request.RevisionID, err)
	}
	if err := copyDigest(&document.RawSHA256, rawDigest); err != nil {
		return storedDocument{}, false, fmt.Errorf("load raw digest: %w", err)
	}
	if err := copyDigest(&document.NormalizedSHA256, normalizedDigest); err != nil {
		return storedDocument{}, false, fmt.Errorf("load normalized digest: %w", err)
	}
	if sha256.Sum256([]byte(request.NormalizedText)) != document.NormalizedSHA256 {
		return storedDocument{}, false, errors.New("dedupe normalized text does not match revision digest")
	}
	canonicalURL, accepted, err := dedupe.ResolveDeclaredCanonical(
		document.FetchedCanonicalURL,
		request.DeclaredCanonicalURL,
		request.AliasPairs,
	)
	if err != nil {
		return storedDocument{}, false, err
	}
	document.CanonicalURL = canonicalURL
	document.PackageName = dedupe.NormalizeMetadata(request.PackageName)
	document.Version = dedupe.NormalizeMetadata(request.Version)
	if document.PackageName == "" || document.Version == "" {
		extractedPackage, extractedVersion := dedupe.ExtractReleaseMetadata(document.Title)
		if document.PackageName == "" {
			document.PackageName = extractedPackage
		}
		if document.Version == "" {
			document.Version = dedupe.NormalizeMetadata(extractedVersion)
		}
	}
	document.SimHash = dedupe.SimHash(request.NormalizedText)
	if publishedAt.Valid {
		document.PublishedAt = publishedAt.Time.UTC()
	}
	return document, accepted, nil
}

func loadCandidates(
	ctx context.Context,
	transaction pgx.Tx,
	document storedDocument,
	maximumAge time.Duration,
	embeddingModelID string,
	queryEmbedding embedding.Vector,
) ([]dedupe.Candidate, error) {
	var vectorLiteral *string
	if len(queryEmbedding) > 0 {
		literal := embeddingstore.VectorLiteral(queryEmbedding)
		vectorLiteral = &literal
	}
	rows, err := transaction.Query(ctx, `
		select
			item.id::text,
			member.cluster_id::text,
			item_source.revision_id::text,
			raw.source_id,
			item_source.source_tier,
			item_source.canonical_url,
			item.normalized_title,
			item.normalized_author,
			item.package_name,
			item.version,
			raw.raw_sha256,
			revision.normalized_sha256,
			item.simhash,
			coalesce(revision.source_published_at, raw.source_published_at),
			item.first_seen_at,
			revision.observed_at,
			case
				when candidate_embedding.embedding is null then null
				else 1.0 - (
					candidate_embedding.embedding::vector(1536)
					<=> $9::vector(1536)
				)
			end as embedding_similarity
		from app.items item
		join app.cluster_members member on member.item_id = item.id
		join app.item_sources item_source on item_source.item_id = item.id
		join app.content_revisions revision on revision.id = item_source.revision_id
		join app.raw_documents raw on raw.id = revision.raw_document_id
		left join lateral (
			select stored_embedding.embedding
			from app.embeddings stored_embedding
			where $8 <> ''
				and stored_embedding.entity_type = 'item'
				and stored_embedding.entity_id = item.id
				and stored_embedding.model_id = $8
			order by stored_embedding.created_at desc, stored_embedding.id desc
			limit 1
		) candidate_embedding on true
		where item_source.revision_id <> $1::uuid
			and (
				item.first_seen_at between $2::timestamptz - $3::interval and $2::timestamptz + $3::interval
				or (raw.source_id = $4 and item_source.canonical_url = $5)
				or item_source.canonical_url = $5
				or raw.raw_sha256 = $6
				or revision.normalized_sha256 = $7
			)
		order by
			case when
				(raw.source_id = $4 and item_source.canonical_url = $5)
				or item_source.canonical_url = $5
				or raw.raw_sha256 = $6
				or revision.normalized_sha256 = $7
			then 0 else 1 end,
			item.first_seen_at desc,
			item.id,
			item_source.sort_order
		limit $10`,
		document.RevisionID,
		document.FirstSeenAt,
		maximumAge.String(),
		document.SourceID,
		document.CanonicalURL,
		document.RawSHA256[:],
		document.NormalizedSHA256[:],
		embeddingModelID,
		vectorLiteral,
		candidateLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("select dedupe candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]dedupe.Candidate, 0)
	for rows.Next() {
		var candidate dedupe.Candidate
		var rawDigest []byte
		var normalizedDigest []byte
		var simhash []byte
		var publishedAt pgtype.Timestamptz
		var embeddingSimilarity pgtype.Float8
		if err := rows.Scan(
			&candidate.ItemID,
			&candidate.ClusterID,
			&candidate.RevisionID,
			&candidate.SourceID,
			&candidate.SourceTier,
			&candidate.CanonicalURL,
			&candidate.NormalizedTitle,
			&candidate.NormalizedAuthor,
			&candidate.PackageName,
			&candidate.Version,
			&rawDigest,
			&normalizedDigest,
			&simhash,
			&publishedAt,
			&candidate.FirstSeenAt,
			&candidate.ObservedAt,
			&embeddingSimilarity,
		); err != nil {
			return nil, fmt.Errorf("scan dedupe candidate: %w", err)
		}
		if err := copyDigest(&candidate.RawSHA256, rawDigest); err != nil {
			return nil, fmt.Errorf("load candidate raw digest: %w", err)
		}
		if err := copyDigest(&candidate.NormalizedSHA256, normalizedDigest); err != nil {
			return nil, fmt.Errorf("load candidate normalized digest: %w", err)
		}
		parsedSimHash, err := decodeSimHash(simhash)
		if err != nil {
			return nil, fmt.Errorf("load candidate SimHash: %w", err)
		}
		candidate.SimHash = parsedSimHash
		if publishedAt.Valid {
			candidate.PublishedAt = publishedAt.Time.UTC()
		}
		if embeddingSimilarity.Valid {
			candidate.EmbeddingSimilarity = embeddingSimilarity.Float64
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dedupe candidates: %w", err)
	}
	return candidates, nil
}

func validateEmbeddingModel(ctx context.Context, transaction pgx.Tx, modelID string) error {
	var dimensions int
	var lifecycleState string
	if err := transaction.QueryRow(ctx, `
		select dimensions, lifecycle_state
		from app.embedding_models
		where model_id = $1`, modelID).Scan(&dimensions, &lifecycleState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("dedupe embedding model is not registered")
		}
		return fmt.Errorf("select dedupe embedding model: %w", err)
	}
	if dimensions != embedding.DefaultDimensions {
		return fmt.Errorf("dedupe embedding model must have %d dimensions", embedding.DefaultDimensions)
	}
	if lifecycleState == "retired" {
		return errors.New("dedupe embedding model may not be retired")
	}
	return nil
}

func persistEmbedding(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	itemID string,
	request dedupe.ProcessRequest,
) error {
	if request.EmbeddingModelID == "" {
		return nil
	}
	_, err := transaction.Exec(ctx, `
		insert into app.embeddings (
			entity_type, entity_id, revision_id, model_id, dimensions,
			embedding, content_sha256
		) values (
			'item', $1::uuid, $2::uuid, $3, $4,
			$5::vector, $6
		)
		on conflict (entity_type, entity_id, model_id, content_sha256) do nothing`,
		itemID,
		document.RevisionID,
		request.EmbeddingModelID,
		embedding.DefaultDimensions,
		embeddingstore.VectorLiteral(request.Embedding),
		document.NormalizedSHA256[:],
	)
	if err != nil {
		return fmt.Errorf("persist dedupe embedding: %w", err)
	}
	return nil
}

func createItemAndCluster(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	decision dedupe.Decision,
	evaluatedAt time.Time,
) (dedupe.ProcessResult, error) {
	itemID, err := insertItem(ctx, transaction, document, evaluatedAt)
	if err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := insertItemSource(ctx, transaction, itemID, document.RevisionID, document.CanonicalURL, "primary", document.SourceTier); err != nil {
		return dedupe.ProcessResult{}, err
	}
	var clusterID string
	err = transaction.QueryRow(ctx, `
		insert into app.story_clusters (
			primary_item_id, cluster_key, title, first_seen_at, last_changed_at
		) values ($1::uuid, $2, $3, $4, $5)
		returning id::text`,
		itemID,
		"story:"+itemID,
		titleFor(document),
		document.FirstSeenAt,
		evaluatedAt,
	).Scan(&clusterID)
	if err != nil {
		return dedupe.ProcessResult{}, fmt.Errorf("insert story cluster: %w", err)
	}
	if err := insertClusterMember(ctx, transaction, clusterID, itemID, 1, dedupe.MethodNew); err != nil {
		return dedupe.ProcessResult{}, err
	}
	return dedupe.ProcessResult{
		ItemID:     itemID,
		ClusterID:  clusterID,
		Outcome:    decision.Outcome,
		Method:     decision.Method,
		Similarity: decision.Similarity,
	}, nil
}

func createClusterMember(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	decision dedupe.Decision,
	evaluatedAt time.Time,
) (dedupe.ProcessResult, error) {
	itemID, err := insertItem(ctx, transaction, document, evaluatedAt)
	if err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := insertItemSource(ctx, transaction, itemID, document.RevisionID, document.CanonicalURL, "primary", document.SourceTier); err != nil {
		return dedupe.ProcessResult{}, err
	}
	if err := insertClusterMember(ctx, transaction, decision.ClusterID, itemID, decision.Similarity, decision.Method); err != nil {
		return dedupe.ProcessResult{}, err
	}
	return dedupe.ProcessResult{
		ItemID:     itemID,
		ClusterID:  decision.ClusterID,
		Outcome:    decision.Outcome,
		Method:     decision.Method,
		Similarity: decision.Similarity,
	}, nil
}

func attachRevision(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	decision dedupe.Decision,
	evaluatedAt time.Time,
) (dedupe.ProcessResult, error) {
	role := "duplicate"
	var previousRole string
	err := transaction.QueryRow(ctx, `
		select source_role
		from app.item_sources item_source
		where item_source.item_id = $1::uuid and item_source.revision_id = $2::uuid`,
		decision.CandidateItemID,
		decision.CandidateRevisionID,
	).Scan(&previousRole)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return dedupe.ProcessResult{}, fmt.Errorf("select prior item source role: %w", err)
	}
	if decision.Outcome == dedupe.OutcomeRevision && err == nil {
		role = previousRole
	}

	var primaryTier string
	err = transaction.QueryRow(ctx, `
		select source_tier
		from app.item_sources
		where item_id = $1::uuid and source_role = 'primary'
		order by sort_order
		limit 1`, decision.CandidateItemID).Scan(&primaryTier)
	if err != nil {
		return dedupe.ProcessResult{}, fmt.Errorf("select current primary source tier: %w", err)
	}
	promote := role == "primary" || tierRank(document.SourceTier) < tierRank(primaryTier)
	if promote {
		if _, err := transaction.Exec(ctx, `
			update app.item_sources
			set source_role = 'supporting'
			where item_id = $1::uuid and source_role = 'primary'`, decision.CandidateItemID); err != nil {
			return dedupe.ProcessResult{}, fmt.Errorf("demote previous primary item source: %w", err)
		}
		role = "primary"
	}
	if err := insertItemSource(ctx, transaction, decision.CandidateItemID, document.RevisionID, document.CanonicalURL, role, document.SourceTier); err != nil {
		return dedupe.ProcessResult{}, err
	}
	if promote {
		if _, err := transaction.Exec(ctx, `
			update app.items
			set current_revision_id = $2::uuid,
				canonical_url = $3,
				title = $4,
				normalized_title = $5,
				normalized_author = $6,
				package_name = $7,
				version = $8,
				published_at = $9,
				status = 'updated',
				simhash = $10,
				lifecycle_state = case when $11 then 'clustered' else lifecycle_state end,
				updated_at = $12
			where id = $1::uuid`,
			decision.CandidateItemID,
			document.RevisionID,
			document.CanonicalURL,
			titleFor(document),
			dedupe.NormalizeMetadata(document.Title),
			dedupe.NormalizeMetadata(document.Author),
			document.PackageName,
			document.Version,
			nullableTime(document.PublishedAt),
			encodeSimHash(document.SimHash),
			document.SourceTier == "T0" || document.SourceTier == "T1",
			evaluatedAt,
		); err != nil {
			return dedupe.ProcessResult{}, fmt.Errorf("promote item revision: %w", err)
		}
	}
	return dedupe.ProcessResult{
		ItemID:     decision.CandidateItemID,
		ClusterID:  decision.ClusterID,
		Outcome:    decision.Outcome,
		Method:     decision.Method,
		Similarity: decision.Similarity,
	}, nil
}

func insertItem(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	evaluatedAt time.Time,
) (string, error) {
	var itemID string
	err := transaction.QueryRow(ctx, `
		insert into app.items (
			current_revision_id, canonical_url, title, normalized_title,
			normalized_author, package_name, version, slug, lifecycle_state,
			published_at, first_seen_at, status, simhash, updated_at
		) values (
			$1::uuid, $2, $3, $4,
			$5, $6, $7, $8, $9,
			$10, $11, 'active', $12, $13
		) returning id::text`,
		document.RevisionID,
		document.CanonicalURL,
		titleFor(document),
		dedupe.NormalizeMetadata(document.Title),
		dedupe.NormalizeMetadata(document.Author),
		document.PackageName,
		document.Version,
		slugFor(document),
		lifecycleFor(document.SourceTier),
		nullableTime(document.PublishedAt),
		document.FirstSeenAt,
		encodeSimHash(document.SimHash),
		evaluatedAt,
	).Scan(&itemID)
	if err != nil {
		return "", fmt.Errorf("insert deduplicated item: %w", err)
	}
	return itemID, nil
}

func insertItemSource(
	ctx context.Context,
	transaction pgx.Tx,
	itemID string,
	revisionID string,
	canonicalURL string,
	role string,
	tier string,
) error {
	_, err := transaction.Exec(ctx, `
		insert into app.item_sources (
			revision_id, item_id, canonical_url, source_role, source_tier, sort_order
		)
		select $1::uuid, $2::uuid, $3, $4, $5, coalesce(max(sort_order) + 1, 0)
		from app.item_sources
		where item_id = $2::uuid`, revisionID, itemID, canonicalURL, role, tier)
	if err != nil {
		return fmt.Errorf("insert item source: %w", err)
	}
	return nil
}

func insertClusterMember(
	ctx context.Context,
	transaction pgx.Tx,
	clusterID string,
	itemID string,
	similarity float64,
	method dedupe.Method,
) error {
	_, err := transaction.Exec(ctx, `
		insert into app.cluster_members (cluster_id, item_id, similarity, method)
		values ($1::uuid, $2::uuid, $3, $4)`, clusterID, itemID, similarity, string(method))
	if err != nil {
		return fmt.Errorf("insert story cluster member: %w", err)
	}
	return nil
}

func recordDecision(
	ctx context.Context,
	transaction pgx.Tx,
	document dedupe.Document,
	result dedupe.ProcessResult,
	decision dedupe.Decision,
	canonicalAccepted bool,
	config dedupe.Config,
	evaluatedAt time.Time,
) error {
	details, err := json.Marshal(map[string]any{
		"canonicalAccepted":  canonicalAccepted,
		"clusterMaxAge":      config.ClusterMaxAge.String(),
		"embeddingThreshold": config.EmbeddingSimilarity,
		"package":            document.PackageName,
		"simhashThreshold":   config.SimHashDistance,
		"version":            document.Version,
	})
	if err != nil {
		return fmt.Errorf("encode dedupe decision details: %w", err)
	}
	var candidateItemID *string
	if decision.CandidateItemID != "" {
		candidateItemID = &decision.CandidateItemID
	}
	var distance *int
	if decision.Method == dedupe.MethodSimHash || decision.Method == dedupe.MethodPackageVersion {
		distance = &decision.SimHashDistance
	}
	_, err = transaction.Exec(ctx, `
		insert into app.dedupe_decisions (
			revision_id, item_id, cluster_id, candidate_item_id, outcome,
			method, similarity, simhash_distance, details, evaluated_at
		) values (
			$1::uuid, $2::uuid, $3::uuid, $4::uuid, $5,
			$6, $7, $8, $9, $10
		)`,
		document.RevisionID,
		result.ItemID,
		result.ClusterID,
		candidateItemID,
		string(result.Outcome),
		string(result.Method),
		result.Similarity,
		distance,
		details,
		evaluatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert dedupe decision: %w", err)
	}
	return nil
}

func refreshClusterPrimary(
	ctx context.Context,
	transaction pgx.Tx,
	clusterID string,
	evaluatedAt time.Time,
) error {
	command, err := transaction.Exec(ctx, `
		with ranked as (
			select
				item.id,
				item.title,
				min(item.first_seen_at) over () as cluster_first_seen_at,
				row_number() over (
					order by
						case primary_source.source_tier
							when 'T0' then 0 when 'T1' then 1
							when 'T2' then 2 when 'T3' then 3 else 4
						end,
						item.first_seen_at,
						item.id
				) as priority
			from app.cluster_members member
			join app.items item on item.id = member.item_id
			join lateral (
				select source_tier
				from app.item_sources
				where item_id = item.id and source_role = 'primary'
				order by sort_order
				limit 1
			) primary_source on true
			where member.cluster_id = $1::uuid
		)
		update app.story_clusters cluster
		set primary_item_id = ranked.id,
			title = ranked.title,
			first_seen_at = ranked.cluster_first_seen_at,
			last_changed_at = greatest(cluster.last_changed_at, $2),
			updated_at = $2
		from ranked
		where cluster.id = $1::uuid and ranked.priority = 1`, clusterID, evaluatedAt)
	if err != nil {
		return fmt.Errorf("refresh story cluster primary: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("story cluster primary refresh did not update exactly one cluster")
	}
	return nil
}

func copyDigest(destination *[sha256.Size]byte, source []byte) error {
	if len(source) != sha256.Size {
		return fmt.Errorf("expected %d digest bytes, got %d", sha256.Size, len(source))
	}
	copy(destination[:], source)
	return nil
}

func encodeSimHash(value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

func decodeSimHash(value []byte) (uint64, error) {
	if len(value) != 8 {
		return 0, fmt.Errorf("expected 8 SimHash bytes, got %d", len(value))
	}
	return binary.BigEndian.Uint64(value), nil
}

func titleFor(document dedupe.Document) string {
	title := strings.TrimSpace(document.Title)
	if title == "" {
		title = document.CanonicalURL
	}
	characters := []rune(title)
	if len(characters) > 1000 {
		return string(characters[:1000])
	}
	return title
}

func slugFor(document dedupe.Document) string {
	base := dedupe.NormalizeMetadata(titleFor(document))
	var slug strings.Builder
	previousHyphen := false
	for _, character := range base {
		isASCIIAlphaNumeric := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if isASCIIAlphaNumeric {
			slug.WriteRune(character)
			previousHyphen = false
			continue
		}
		if (unicode.IsSpace(character) || character == '-') && slug.Len() > 0 && !previousHyphen {
			slug.WriteByte('-')
			previousHyphen = true
		}
	}
	baseSlug := strings.Trim(slug.String(), "-")
	if baseSlug == "" {
		baseSlug = "story"
	}
	if len(baseSlug) > 80 {
		baseSlug = strings.TrimRight(baseSlug[:80], "-")
	}
	suffix := strings.ReplaceAll(document.RevisionID, "-", "")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	return baseSlug + "-" + suffix
}

func tierRank(tier string) int {
	switch tier {
	case "T0":
		return 0
	case "T1":
		return 1
	case "T2":
		return 2
	case "T3":
		return 3
	default:
		return 4
	}
}

func lifecycleFor(tier string) string {
	if tier == "T0" || tier == "T1" {
		return "clustered"
	}
	return "needs_review"
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
