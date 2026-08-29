package reembedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/embedding"
	embeddingstore "github.com/traweezy/relantern/internal/embedding/pgstore"
	"github.com/traweezy/relantern/internal/search/pgstore"
	"github.com/traweezy/relantern/internal/storage"
)

const maximumNormalizedContentBytes = int64(1_000_000)

var (
	ErrInvalidTarget     = errors.New("re-embedding target is invalid")
	ErrModelMismatch     = errors.New("re-embedding model does not match this worker")
	ErrRevisionIntegrity = errors.New("normalized revision content failed integrity validation")
)

type Embedder interface {
	Embed(context.Context, string) (embedding.Vector, error)
}

type EmbeddingWriter interface {
	Put(context.Context, embeddingstore.PutRequest) (string, bool, error)
}

type SearchIndexer interface {
	IndexDocument(context.Context, pgstore.IndexRequest) error
}

type Request struct {
	EntityType string
	EntityID   string
	RevisionID string
	ModelID    string
}

type Result struct {
	EmbeddingID string
	Inserted    bool
	Obsolete    bool
}

type Processor struct {
	pool       *pgxpool.Pool
	reader     storage.ObjectReader
	embedder   Embedder
	embeddings EmbeddingWriter
	search     SearchIndexer
	modelID    string
	dimensions int
}

func New(
	pool *pgxpool.Pool,
	reader storage.ObjectReader,
	embedder Embedder,
	embeddings EmbeddingWriter,
	search SearchIndexer,
	modelID string,
	dimensions int,
) (*Processor, error) {
	if pool == nil || reader == nil || embedder == nil || embeddings == nil || search == nil {
		return nil, errors.New("re-embedding processor requires database, object, provider, embedding, and search dependencies")
	}
	if strings.TrimSpace(modelID) == "" || dimensions != embedding.DefaultDimensions {
		return nil, errors.New("re-embedding processor requires the pinned model and dimensions")
	}
	return &Processor{
		pool:       pool,
		reader:     reader,
		embedder:   embedder,
		embeddings: embeddings,
		search:     search,
		modelID:    modelID,
		dimensions: dimensions,
	}, nil
}

func (processor *Processor) Process(ctx context.Context, request Request) (Result, error) {
	if request.EntityType != "item" {
		return Result{}, fmt.Errorf("%w: entity type must be item", ErrInvalidTarget)
	}
	if _, err := uuid.Parse(request.EntityID); err != nil {
		return Result{}, fmt.Errorf("%w: entity ID must be a UUID", ErrInvalidTarget)
	}
	if _, err := uuid.Parse(request.RevisionID); err != nil {
		return Result{}, fmt.Errorf("%w: revision ID must be a UUID", ErrInvalidTarget)
	}
	if request.ModelID != processor.modelID {
		return Result{}, ErrModelMismatch
	}

	var currentRevisionID string
	var objectKey string
	var expectedDigest []byte
	err := processor.pool.QueryRow(ctx, `
		select
			item.current_revision_id::text,
			revision.normalized_text_object_key,
			revision.normalized_sha256
		from app.items item
		join app.content_revisions revision on revision.id = item.current_revision_id
		where item.id = $1::uuid`, request.EntityID).Scan(
		&currentRevisionID,
		&objectKey,
		&expectedDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, fmt.Errorf("%w: item does not exist", ErrInvalidTarget)
	}
	if err != nil {
		return Result{}, fmt.Errorf("select re-embedding target: %w", err)
	}
	if currentRevisionID != request.RevisionID {
		return Result{Obsolete: true}, nil
	}
	if len(expectedDigest) != sha256.Size {
		return Result{}, fmt.Errorf("%w: stored digest has %d bytes", ErrRevisionIntegrity, len(expectedDigest))
	}
	payload, err := processor.reader.Read(ctx, objectKey, maximumNormalizedContentBytes)
	if err != nil {
		return Result{}, fmt.Errorf("read normalized revision: %w", err)
	}
	digest := sha256.Sum256(payload)
	if !bytes.Equal(digest[:], expectedDigest) || !utf8.Valid(payload) {
		return Result{}, ErrRevisionIntegrity
	}
	embeddingInput, err := boundedEmbeddingInput(string(payload))
	if err != nil {
		return Result{}, err
	}
	vector, err := processor.embedder.Embed(ctx, embeddingInput)
	if err != nil {
		return Result{}, fmt.Errorf("create embedding: %w", err)
	}
	if err := embedding.ValidateVector(vector, processor.dimensions); err != nil {
		return Result{}, fmt.Errorf("validate provider embedding: %w", err)
	}
	embeddingID, inserted, err := processor.embeddings.Put(ctx, embeddingstore.PutRequest{
		EntityType: request.EntityType,
		EntityID:   request.EntityID,
		RevisionID: request.RevisionID,
		ModelID:    request.ModelID,
		Input:      embeddingInput,
		Vector:     vector,
	})
	if err != nil {
		return Result{}, fmt.Errorf("store embedding: %w", err)
	}
	if err := processor.search.IndexDocument(ctx, pgstore.IndexRequest{
		ItemID:            request.EntityID,
		RevisionID:        request.RevisionID,
		NormalizedContent: string(payload),
	}); err != nil {
		return Result{}, fmt.Errorf("index search document: %w", err)
	}
	return Result{EmbeddingID: embeddingID, Inserted: inserted}, nil
}

func boundedEmbeddingInput(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if err := embedding.ValidateInput(trimmed); err == nil {
		return trimmed, nil
	}
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return "", errors.New("normalized revision does not contain valid embedding input")
	}
	payload := []byte(trimmed)
	if len(payload) <= embedding.MaximumInputBytes {
		return "", errors.New("normalized revision embedding input is invalid")
	}
	payload = payload[:embedding.MaximumInputBytes]
	for !utf8.Valid(payload) {
		payload = payload[:len(payload)-1]
	}
	return string(payload), nil
}
