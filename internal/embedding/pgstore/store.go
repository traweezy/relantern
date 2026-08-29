package pgstore

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/embedding"
)

type Store struct {
	pool *pgxpool.Pool
}

type Model struct {
	ModelID        string
	Provider       string
	Dimensions     int
	LifecycleState string
	EvaluatedAt    *time.Time
	ActivatedAt    *time.Time
}

type PutRequest struct {
	EntityType string
	EntityID   string
	RevisionID string
	ModelID    string
	Input      string
	Vector     embedding.Vector
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("embedding store requires a database pool")
	}
	return &Store{pool: pool}, nil
}

func (store *Store) ActiveModel(ctx context.Context) (Model, error) {
	return selectModel(ctx, store.pool, `
		select model_id, provider, dimensions, lifecycle_state, evaluated_at, activated_at
		from app.embedding_models
		where lifecycle_state = 'active'`)
}

func (store *Store) RegisterModel(ctx context.Context, modelID string, provider string, dimensions int) error {
	if strings.TrimSpace(modelID) == "" || len(modelID) > 255 {
		return errors.New("embedding model ID must contain between 1 and 255 characters")
	}
	if provider != "openai" {
		return errors.New("embedding model provider must be openai")
	}
	if dimensions < 1 || dimensions > 4096 {
		return errors.New("embedding dimensions must be between 1 and 4096")
	}
	result, err := store.pool.Exec(ctx, `
		insert into app.embedding_models (
			model_id, provider, dimensions, lifecycle_state
		) values ($1, $2, $3, 'building')
		on conflict (model_id) do update
		set model_id = excluded.model_id
		where embedding_models.provider = excluded.provider
			and embedding_models.dimensions = excluded.dimensions`, modelID, provider, dimensions)
	if err != nil {
		return fmt.Errorf("register embedding model: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("embedding model ID is already registered with different metadata")
	}
	return nil
}

func (store *Store) ActivateModel(
	ctx context.Context,
	modelID string,
	evaluatedAt time.Time,
	activatedAt time.Time,
) error {
	if evaluatedAt.IsZero() || activatedAt.IsZero() || activatedAt.Before(evaluatedAt) {
		return errors.New("embedding activation requires ordered evaluation and activation times")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin embedding model activation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lifecycleState string
	var dimensions int
	if err := tx.QueryRow(ctx, `
		select lifecycle_state, dimensions
		from app.embedding_models
		where model_id = $1
		for update`, modelID).Scan(&lifecycleState, &dimensions); err != nil {
		return fmt.Errorf("select embedding model for activation: %w", err)
	}
	if lifecycleState == "retired" {
		return errors.New("retired embedding models cannot be reactivated")
	}
	if dimensions != embedding.DefaultDimensions {
		return fmt.Errorf("embedding activation currently requires %d dimensions", embedding.DefaultDimensions)
	}
	if _, err := tx.Exec(ctx, `
		update app.embedding_models
		set lifecycle_state = 'inactive', updated_at = $2
		where lifecycle_state = 'active' and model_id <> $1`, modelID, activatedAt.UTC()); err != nil {
		return fmt.Errorf("deactivate current embedding model: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update app.embedding_models
		set lifecycle_state = 'active',
			evaluated_at = $2,
			activated_at = $3,
			updated_at = $3
		where model_id = $1`, modelID, evaluatedAt.UTC(), activatedAt.UTC()); err != nil {
		return fmt.Errorf("activate embedding model: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit embedding model activation: %w", err)
	}
	return nil
}

func (store *Store) Put(ctx context.Context, request PutRequest) (string, bool, error) {
	if request.EntityType != "item" && request.EntityType != "story_cluster" {
		return "", false, errors.New("embedding entity type must be item or story_cluster")
	}
	if request.EntityID == "" || request.ModelID == "" {
		return "", false, errors.New("embedding entity and model IDs are required")
	}
	if err := embedding.ValidateInput(request.Input); err != nil {
		return "", false, err
	}
	model, err := selectModel(ctx, store.pool, `
		select model_id, provider, dimensions, lifecycle_state, evaluated_at, activated_at
		from app.embedding_models
		where model_id = $1`, request.ModelID)
	if err != nil {
		return "", false, fmt.Errorf("select embedding model: %w", err)
	}
	if model.LifecycleState == "retired" {
		return "", false, errors.New("cannot write embeddings for a retired model")
	}
	if err := embedding.ValidateVector(request.Vector, model.Dimensions); err != nil {
		return "", false, err
	}
	digest := embedding.ContentDigest(request.Input)
	var id string
	var inserted bool
	err = store.pool.QueryRow(ctx, `
		with inserted as (
			insert into app.embeddings (
				entity_type, entity_id, revision_id, model_id, dimensions,
				embedding, content_sha256
			) values (
				$1, $2::uuid, nullif($3, '')::uuid, $4, $5,
				$6::vector, decode($7, 'hex')
			)
			on conflict (entity_type, entity_id, model_id, content_sha256) do nothing
			returning id
		)
		select id::text, true from inserted
		union all
		select id::text, false
		from app.embeddings
		where entity_type = $1
			and entity_id = $2::uuid
			and model_id = $4
			and content_sha256 = decode($7, 'hex')
			and not exists (select 1 from inserted)
		limit 1`,
		request.EntityType,
		request.EntityID,
		request.RevisionID,
		request.ModelID,
		model.Dimensions,
		VectorLiteral(request.Vector),
		hex.EncodeToString(digest[:]),
	).Scan(&id, &inserted)
	if err != nil {
		return "", false, fmt.Errorf("persist immutable embedding: %w", err)
	}
	return id, inserted, nil
}

func VectorLiteral(vector embedding.Vector) string {
	var builder strings.Builder
	builder.Grow(len(vector) * 10)
	builder.WriteByte('[')
	for index, value := range vector {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func selectModel(ctx context.Context, querier queryRower, query string, arguments ...any) (Model, error) {
	var model Model
	err := querier.QueryRow(ctx, query, arguments...).Scan(
		&model.ModelID,
		&model.Provider,
		&model.Dimensions,
		&model.LifecycleState,
		&model.EvaluatedAt,
		&model.ActivatedAt,
	)
	if err != nil {
		return Model{}, err
	}
	return model, nil
}
