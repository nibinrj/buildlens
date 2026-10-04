// Package dbtest starts a throwaway Postgres for tests. Only _test.go files import it, so testcontainers is never
// linked into a binary.
package dbtest

import (
	"context"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the same pinned image docker-compose.yml runs.
// BUILDLENS_TEST_POSTGRES_IMAGE overrides it, for example to try a new version before pinning it.
const Image = "postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"

// URL starts a Postgres container and returns its connection URL. The container is removed when the test ends.
// Tests using it are skipped with -short. The schema is empty: callers run db.Migrate themselves (importing db
// here would create an import cycle with db's own tests).
func URL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()

	image := Image
	if v := os.Getenv("BUILDLENS_TEST_POSTGRES_IMAGE"); v != "" {
		image = v
	}

	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("buildlens"),
		tcpostgres.WithUsername("buildlens"),
		tcpostgres.WithPassword("test-only-password"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return url
}
