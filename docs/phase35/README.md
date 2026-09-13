# Phase 35 — Testcontainers-Go Migration of Existing PostgreSQL Integration Tests

## Status

Active. Four artefacts ship: build tag, testcontainers-go helper,
`t.Skip` removal, and CI lane.

## Goal

ADR-076 §Follow-ups identified a live PostgreSQL coverage gap
in the cross-module tenant-id envelope chain:
`TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`
exercised only the migration file **parse**, not the migration
**apply** path. Phase 35 closes this gap by migrating the two
pre-existing integration tests from an external PostgreSQL
(`ASTRASYNC_TEST_POSTGRES_URL`) to a hermetic
testcontainers-go instance, while simultaneously fixing two
pre-existing compliance violations:

1. `t.Skip` on invariant tests (testing.mdc §8).
2. Missing `//go:build integration` build tag (testing.mdc §2).

## What ships in Phase 35

```
control-plane/
├── go.mod                                           # + testcontainers-go v0.35.0
├── go.sum                                           # updated (transitive deps)
└── job/postgres/
    ├── repository_integration_test.go                 # +build tag, -t.Skip, +tc helper
    ├── mutation_integration_test.go                   # +build tag, -t.Skip, +tc helper
    └── postgres_testcontainer_helper_test.go          # NEW: shared helper
.github/workflows/
└── control-plane-integration.yml                     # NEW: CI lane
docs/adr/
├── adr-083-phase35-testcontainers-go-postgres.md    # Superseded by ADR-084
└── adr-084-phase35-testcontainers-go-migration.md    # Accepted
docs/phase35/
└── README.md                                        # This file
```

## Changes

### 1. Build tag — `//go:build integration`

Both `repository_integration_test.go` and
`mutation_integration_test.go` now carry:

```go
//go:build integration
```

The filename suffix (`_integration_test.go`) was already correct;
the build tag was the missing enforcement. `go test ./...` no
longer discovers or compiles these files without `-tags=integration`.

### 2. testcontainers-go helper — `postgres_testcontainer_helper_test.go`

A new shared helper file (`package postgres_test`, same package
as the two consumer integration tests) implements:

```go
func startPostgresContainer(t *testing.T) string
```

The helper:
- Boots `postgres:16-alpine` via `testcontainers-go/modules/postgres`
- Returns the `sslmode=disable` connection string
- Registers `t.Cleanup` to terminate the container

Repository migrations stay owned by the tests and must be applied
in cross-module dependency order (`auth → job core → connection →
job mutations`). This avoids applying `002_job_mutations.sql`
before its auth tenant and connection-binding dependencies exist.

The helper is shared by all integration tests in the `postgres`
package. ADR-084 §Decision discusses the inline-versus-shared
trade-off; the shared helper is used because two integration test
files already exist.

### 3. `t.Skip` removal

Both integration tests previously guarded with:

```go
if os.Getenv("ASTRASYNC_TEST_POSTGRES_URL") == "" {
    t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
}
```

This was a compliance violation (testing.mdc §8 forbids
`t.Skip` on invariant tests) and a maintenance hazard (developers
forgot to set the env var, the test silently passed). The
short-circuit is replaced by `startPostgresContainer(t)` which
fails fast if Docker is unavailable rather than silently
skipping.

### 4. CI lane — `.github/workflows/control-plane-integration.yml`

A new workflow runs `go test -tags=integration
./control-plane/job/postgres/...` on:

- PRs touching `control-plane/job/postgres/**`,
  `control-plane/go.mod`, `control-plane/go.sum`, or the
  workflow itself
- post-merge pushes to `main` and `develop`
- `workflow_dispatch` for on-demand re-runs

The lane runs on `ubuntu-latest` (Docker is pre-installed) with
a 10-minute timeout. The unit-test lane (`ci.yml`) is
**unchanged**; the integration lane does not gate PRs that
don't touch the persistence layer.

### Module dependency

`github.com/testcontainers/testcontainers-go v0.35.0` and
`github.com/testcontainers/testcontainers-go/modules/postgres
v0.35.0` are added to `control-plane/go.mod` (the root Go
module that owns `control-plane/job/`). The version is pinned
per testing.mdc §7; future bumps require an ADR.

The image tag is `postgres:16-alpine`, pinned in the helper as
`postgresImage const`. A future PostgreSQL 17 bump requires an
explicit ADR.

## CI integration

`.github/workflows/control-plane-integration.yml` is the
canonical integration lane. Local invocation:

```bash
go test -tags=integration -v ./control-plane/job/postgres/...
```

The lane requires Docker. On `ubuntu-latest` runners Docker is
pre-installed; on macOS/Windows use Docker Desktop.

## Non-Goals (this phase)

- **Envtest for the controller.** controller-runtime envtest
  (etcd binaries) is a separate concern. ADR-082 §Follow-ups.
- **Cross-module fixture live PostgreSQL upgrade.** The
  `tests/cross-module/chain-tenant-id/` fixture remains
  dependency-free. ADR-084 §Follow-ups.
- **OIDC testcontainers (Keycloak / Authelia).** Separate ADR.
- **New integration tests.** Phase 35 migrates the two
  pre-existing tests; it does not add new test surface.
- **production code change.** No production Go files are modified.
  The repository contracts, migration apply logic, and audit
  boundary are unchanged.

## Acceptance criteria

- [x] `repository_integration_test.go` carries
      `//go:build integration` and calls `startPostgresContainer(t)`.
- [x] `mutation_integration_test.go` carries
      `//go:build integration` and calls `startPostgresContainer(t)`.
- [x] `postgres_testcontainer_helper_test.go` exists and exports
      `startPostgresContainer`.
- [x] Both `t.Skip` calls and the `os.Getenv` import are removed.
- [x] `control-plane/go.mod` contains `testcontainers-go`.
- [x] `control-plane-integration.yml` runs on `ubuntu-latest`
      with Docker, gates PRs touching `control-plane/job/postgres/`.
- [x] `go test ./...` (no `-tags=integration`) passes without
      Docker; `go test -tags=integration ./control-plane/job/postgres/...`
      requires Docker.
- [x] `go vet ./...` and `go test ./...` are clean in the root
      module.
- [x] ADR-084 Accepted, ADR-083 Superseded, index updated.
- [x] `docs/phase35/README.md` exists.

## Follow-ups (out of scope for Phase 35)

- **Shared helper module extraction** (`control-plane/integration-test/postgres/`):
  if a third integration test file needs the helper, extract the
  shared package. ADR-084 §Alternatives Considered.
- **Cross-module fixture live PostgreSQL**: wire the dry-run
  migration fixture to `startPostgresContainer`. ADR-084 §Follow-ups.
- **Envtest for the controller**: separate ADR. ADR-082 §Follow-ups.
- **OIDC testcontainers**: separate ADR.

## Rollback

Removing the `//go:build integration` tags, restoring the
`t.Skip` short-circuits, reverting `control-plane/go.mod`,
and deleting the CI workflow reverts the slice. The unit test
suite is unaffected.
