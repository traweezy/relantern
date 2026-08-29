package pgstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/embedding"
	"github.com/traweezy/relantern/internal/embedding/pgstore"
)

func TestStorePersistsImmutableModelVersionedEmbeddings(t *testing.T) {
	pool := openEmbeddingDatabase(t)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	active, err := store.ActiveModel(context.Background())
	if err != nil {
		t.Fatalf("ActiveModel() error = %v", err)
	}
	if active.ModelID != embedding.DefaultModelID || active.Dimensions != embedding.DefaultDimensions || active.LifecycleState != "active" {
		t.Fatalf("active model = %+v", active)
	}

	entityID := uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "delete from app.embeddings where entity_id = $1::uuid", entityID)
	})
	vector := basisVector(0)
	firstID, inserted, err := store.Put(context.Background(), pgstore.PutRequest{
		EntityType: "item",
		EntityID:   entityID,
		ModelID:    active.ModelID,
		Input:      "immutable database availability embedding",
		Vector:     vector,
	})
	if err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	secondID, secondInserted, err := store.Put(context.Background(), pgstore.PutRequest{
		EntityType: "item",
		EntityID:   entityID,
		ModelID:    active.ModelID,
		Input:      "immutable database availability embedding",
		Vector:     vector,
	})
	if err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	if !inserted || secondInserted || firstID != secondID {
		t.Fatalf("idempotent Put() = first (%q, %t), second (%q, %t)", firstID, inserted, secondID, secondInserted)
	}
	thirdID, thirdInserted, err := store.Put(context.Background(), pgstore.PutRequest{
		EntityType: "item",
		EntityID:   entityID,
		ModelID:    active.ModelID,
		Input:      "materially changed database availability embedding",
		Vector:     basisVector(1),
	})
	if err != nil {
		t.Fatalf("changed Put() error = %v", err)
	}
	if !thirdInserted || thirdID == firstID {
		t.Fatalf("changed Put() = (%q, %t), first = %q", thirdID, thirdInserted, firstID)
	}
	var count int
	if err := pool.QueryRow(context.Background(), "select count(*) from app.embeddings where entity_id = $1::uuid", entityID).Scan(&count); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if count != 2 {
		t.Fatalf("immutable embedding count = %d, want 2", count)
	}
}

func TestStoreRegistersBuildingModelsAndRejectsInvalidBoundaries(t *testing.T) {
	pool := openEmbeddingDatabase(t)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelID := "embedding-integration-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "delete from app.embeddings where model_id = $1", modelID)
		_, _ = pool.Exec(context.Background(), "delete from app.embedding_models where model_id = $1", modelID)
	})
	if err := store.RegisterModel(context.Background(), modelID, "openai", embedding.DefaultDimensions); err != nil {
		t.Fatalf("RegisterModel() error = %v", err)
	}
	if err := store.RegisterModel(context.Background(), modelID, "openai", embedding.DefaultDimensions); err != nil {
		t.Fatalf("idempotent RegisterModel() error = %v", err)
	}
	activeBefore, err := store.ActiveModel(context.Background())
	if err != nil {
		t.Fatalf("ActiveModel() before activation error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			update app.embedding_models
			set evaluated_at = $2, activated_at = $3, updated_at = $3
			where model_id = $1`, activeBefore.ModelID, activeBefore.EvaluatedAt, activeBefore.ActivatedAt)
	})
	evaluatedAt := time.Date(2050, time.January, 1, 0, 0, 0, 0, time.UTC)
	activatedAt := evaluatedAt.Add(time.Minute)
	if err := store.ActivateModel(context.Background(), activeBefore.ModelID, evaluatedAt, activatedAt); err != nil {
		t.Fatalf("ActivateModel(active) error = %v", err)
	}
	entityID := uuid.NewString()
	if _, inserted, err := store.Put(context.Background(), pgstore.PutRequest{
		EntityType: "story_cluster",
		EntityID:   entityID,
		ModelID:    modelID,
		Input:      "parallel model build",
		Vector:     basisVector(2),
	}); err != nil || !inserted {
		t.Fatalf("building-model Put() = inserted %t, error %v", inserted, err)
	}
	if _, err := pool.Exec(context.Background(), "update app.embedding_models set lifecycle_state = 'inactive' where model_id = $1", modelID); err != nil {
		t.Fatalf("deactivate test model: %v", err)
	}
	if _, inserted, err := store.Put(context.Background(), pgstore.PutRequest{
		EntityType: "story_cluster",
		EntityID:   uuid.NewString(),
		ModelID:    modelID,
		Input:      "inactive rollback-ready model",
		Vector:     basisVector(3),
	}); err != nil || !inserted {
		t.Fatalf("inactive-model Put() = inserted %t, error %v", inserted, err)
	}
	if err := store.ActivateModel(context.Background(), modelID, time.Time{}, time.Now()); err == nil {
		t.Fatal("ActivateModel() accepted missing evaluation time")
	}
	if err := store.ActivateModel(context.Background(), "missing-model", time.Now(), time.Now()); err == nil {
		t.Fatal("ActivateModel() accepted a missing model")
	}
	if _, err := pool.Exec(context.Background(), "update app.embedding_models set lifecycle_state = 'retired' where model_id = $1", modelID); err != nil {
		t.Fatalf("retire test model: %v", err)
	}
	if err := store.ActivateModel(context.Background(), modelID, evaluatedAt, activatedAt); err == nil {
		t.Fatal("ActivateModel() accepted a retired model")
	}
	for _, request := range []pgstore.PutRequest{
		{},
		{EntityType: "unknown", EntityID: entityID, ModelID: modelID, Input: "input", Vector: basisVector(0)},
		{EntityType: "item", EntityID: entityID, ModelID: modelID, Input: "", Vector: basisVector(0)},
		{EntityType: "item", EntityID: entityID, ModelID: modelID, Input: "input", Vector: embedding.Vector{1}},
	} {
		if _, _, err := store.Put(context.Background(), request); err == nil {
			t.Errorf("Put() accepted %+v", request)
		}
	}
	for _, registration := range []struct {
		modelID    string
		provider   string
		dimensions int
	}{
		{provider: "openai", dimensions: embedding.DefaultDimensions},
		{modelID: "model", provider: "other", dimensions: embedding.DefaultDimensions},
		{modelID: "model", provider: "openai", dimensions: 0},
	} {
		if err := store.RegisterModel(context.Background(), registration.modelID, registration.provider, registration.dimensions); err == nil {
			t.Errorf("RegisterModel() accepted %+v", registration)
		}
	}
	if err := store.RegisterModel(context.Background(), modelID, "openai", 32); err == nil {
		t.Fatal("RegisterModel() accepted metadata drift for an existing model ID")
	}
	if _, err := pgstore.New(nil); err == nil {
		t.Fatal("New() accepted a nil pool")
	}
}

func basisVector(index int) embedding.Vector {
	vector := make(embedding.Vector, embedding.DefaultDimensions)
	vector[index] = 1
	return vector
}

func openEmbeddingDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required for embedding integration tests")
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
	return pool
}
