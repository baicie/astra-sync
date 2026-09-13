//go:build integration

// Package postgres_test hosts the integration tests that drive a real
// PostgreSQL instance via testcontainers-go. The helpers in this file
// are shared by every *_integration_test.go file in the same
// package; the inline-helper decision lives in ADR-084.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// postgresImage is the PostgreSQL image tag the integration tests boot.
// The image tag is pinned (testing.mdc section 7) so a future PostgreSQL
// major-version bump is the explicit ADR-required step.
const postgresImage = "postgres:16-alpine"

// startPostgresContainer spins up a PostgreSQL container via
// testcontainers-go and returns the connection string. Repository
// migrations remain owned by the tests because cross-module schemas
// must be applied in dependency order. The container is terminated via
// t.Cleanup when the test ends.
//
// The function is the single entry point for PostgreSQL in the
// control-plane integration tests; callers MUST NOT construct a
// container directly. A failure to start the container is fatal -
// the integration test cannot run without a live PostgreSQL
// (testing.mdc section 8 forbids t.Skip on invariant tests).
func startPostgresContainer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	pgC, err := postgres.RunContainer(ctx,
		testcontainers.WithImage(postgresImage),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		terminateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := pgC.Terminate(terminateCtx); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	return dsn
}
