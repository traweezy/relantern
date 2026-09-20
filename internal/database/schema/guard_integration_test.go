package schema_test

import (
	"context"
	"os"
	"testing"

	"github.com/traweezy/relantern/internal/config"
	"github.com/traweezy/relantern/internal/database"
	"github.com/traweezy/relantern/internal/database/schema"
)

func TestGuardAcceptsFullyMigratedPostgreSQL(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("DATABASE_PASSWORD_FILE") == "" {
		t.Skip("database configuration is required")
	}
	settings, err := config.LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	guard, err := schema.New(pool, config.EnvironmentTest, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(context.Background()); err != nil {
		t.Fatalf("fully migrated database rejected: %v", err)
	}
}
