//go:build integration

// Package postgres_test hosts the integration tests that drive a real
// PostgreSQL instance via testcontainers-go. The helpers in this file
// are shared by every *_integration_test.go file in the same
// package; the inline-helper decision lives in ADR-084.
package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// postgresImage is the PostgreSQL image tag the integration tests boot.
// The image tag is pinned (testing.mdc section 7) so a future PostgreSQL
// major-version bump is the explicit ADR-required step.
const postgresImage = "postgres:16-alpine"

// migrationsDir is the directory whose *.sql files are applied to
// the container after boot. The path is relative to the Go module root
// (control-plane/), where go test executes the test binary.
const migrationsDir = "job/postgres/migrations"

// startPostgresContainer spins up a PostgreSQL container via
// testcontainers-go, applies every *.sql file under migrationsDir
// in lexical order, and returns the connection string. The container
// is terminated via t.Cleanup when the test ends.
//
// The function is the single entry point for PostgreSQL in the
// control-plane integration tests; callers MUST NOT construct a
// container directly. A failure to start the container is fatal -
// the integration test cannot run without a live PostgreSQL
// (testing.mdc section 8 forbids t.Skip on invariant tests).
func startPostgresContainer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	// Log the actual working directory so we can diagnose
	// path resolution failures in CI.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Logf("DEBUG working directory: %s", cwd)
	migrationsPath := filepath.Join(cwd, migrationsDir)
	t.Logf("DEBUG migrations path: %s", migrationsPath)

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
	if err := applyMigrations(ctx, dsn, migrationsPath); err != nil {
		t.Fatalf("apply migrations under %q: %v", migrationsPath, err)
	}
	return dsn
}

// applyMigrations opens a connection pool against dsn and applies
// every *.sql file under dir in lexical order. The helper is
// order-sensitive because the migration filenames encode their apply
// order (e.g. 001_jobs.sql precedes 002_job_mutations.sql). The
// migration contents are loaded from disk so this helper stays
// ignorant of the schema it applies.
func applyMigrations(ctx context.Context, dsn, dir string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open pool: %w", err)
	}
	defer pool.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		bytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", path, err)
		}
		if _, err := pool.Exec(ctx, string(bytes)); err != nil {
			return fmt.Errorf("apply migration %q: %w", path, err)
		}
	}
	return nil
}
