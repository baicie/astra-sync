# ADR-084: Phase 35 — Testcontainers-Go Migration of Existing PostgreSQL Integration Tests

## Status

Accepted — Supersedes ADR-083 (scope reduction)

## Context

`control-plane/job/postgres/` ships two pre-existing
integration tests:

- `repository_integration_test.go` — drives `job.Repository`
  lifecycle (Create → RequestStart → Update → Delete) against
  a real PostgreSQL via `ASTRASYNC_TEST_POSTGRES_URL`.
- `mutation_integration_test.go` — drives the cross-module
  **atomic job mutation** path (auth + connection + job
  repositories inside one transaction) against the same
  PostgreSQL.

The two tests cover the SQL-level regression surface that
ADR-083 §Context calls out:

> 1. **API Server mutation path against a real `jobs` table.**
>    `INSERT … RETURNING id, version` path against a real
>    PostgreSQL.
> 2. **Auth membership transaction boundary (ADR-037).**
>    `RBAC + audit` rollback contract.

ADR-083 §Decision was written against an **imagined** state in
which the SQL-level coverage did not exist. The pre-existing
`mutation_integration_test.go` already covers the cross-module
RBAC transaction boundary against a real PostgreSQL. The
proposed helper module at
`control-plane/integration-test/postgres/` would have
duplicated that coverage with three more integration test files
inside `control-plane/api-server/`, `control-plane/auth/`, and
`control-plane/compiler-validation/` — a net loss in review
surface area (the three new tests would each be smaller than
`mutation_integration_test.go`, with overlapping
`jobs` / `audit` / `connections` setup).

This ADR supersedes ADR-083 by **narrowing the scope** to the
real gaps:

### Gap 1 — Both integration tests lack a build tag

`repository_integration_test.go` and
`mutation_integration_test.go` are discovered by `go test
./...` because they carry neither `//go:build integration`
nor the `_integration_test.go` filename suffix is enforced
in the build-tag sense. The `t.Skip` short-circuit keeps them
from running locally, but the project rule
(`testing.mdc` §2 Go 测试) requires:

> 数据库 / etcd / OIDC 集成测试放进 `*_integration_test.go`
> 并加 build tag

Both conditions must hold; today neither is satisfied.

### Gap 2 — Both integration tests use `t.Skip`

The `ASTRASYNC_TEST_POSTGRES_URL` short-circuit is a
`t.Skip` call. Project rule (`testing.mdc` §8) forbids
`t.Skip` for **invariant** tests. The atomic-mutation
contract (ADR-037, "data row + audit row in the same
transaction") is a security-critical invariant; skipping it
on the developer's machine is a regression-magnet (a future
refactor that drops the transaction boundary is not caught
until the smoke e2e run).

### Gap 3 — No CI integration lane

`.github/workflows/` does not include a workflow that boots
PostgreSQL + runs the integration suite. Today the
integration tests run only when a developer remembers to
set `ASTRASYNC_TEST_POSTGRES_URL` and run
`go test -tags=integration ./control-plane/job/postgres/`.

### Gap 4 — Integration tests depend on external infrastructure

The `ASTRASYNC_TEST_POSTGRES_URL` model assumes a long-lived
PostgreSQL (developer-machine `docker compose up postgres`
or shared dev cluster). The hermetic-testcontainers-go model
introduces the same coverage with zero infra setup; the
project already uses
`org.testcontainers:postgresql` transitively on the Java
side (`pom.xml`), so the Go / Java test-stack stays
symmetric.

## Decision

Phase 35 ships four artefacts:

1. **`//go:build integration` build tag** at the top of
   `control-plane/job/postgres/repository_integration_test.go`
   and
   `control-plane/job/postgres/mutation_integration_test.go`.
   The filename suffix is already correct (`_integration_test.go`);
   the build tag is the missing enforcement.

2. **`testcontainers-go/modules/postgres` migration** of the
   two integration tests. The helper code lives in a shared
   `postgres_testcontainer_helper_test.go` file. The helper only
   owns container lifecycle; repository migrations remain owned
   by each test and run in cross-module dependency order
   (`auth → job core → connection → job mutations`).

3. **CI lane**
   `.github/workflows/control-plane-integration.yml` that
   runs on PRs that touch
   `control-plane/job/postgres/**` or the workflow itself.
   The lane uses
   `docker/setup-buildx-action@v3` (already pinned in
   `.github/workflows/catalog-check.yml`) and runs
   `go test -tags=integration ./control-plane/job/postgres/...`.
   The lane does NOT gate the unit-test lane — unit tests
   continue to run without Docker.

4. **`ASTRASYNC_TEST_POSTGRES_URL` is removed.** The
   environment-variable escape hatch is dropped; the two
   integration tests are now hermetic by construction.

### Shared helper shape

```go
// startPostgresContainer boots a Postgres 16 container via
// testcontainers-go and registers t.Cleanup to terminate the
// container when the test ends. Repository migrations remain
// test-owned so they can be applied in dependency order.
// The image tag is pinned to match the production deployment
// (PostgreSQL 16).
func startPostgresContainer(t *testing.T) string {
    t.Helper()
    ctx := context.Background()
    pgC, err := postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:16-alpine"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2).WithStartupTimeout(60*time.Second)),
    )
    if err != nil { t.Fatalf("start postgres: %v", err) }
    t.Cleanup(func() {
        if err := pgC.Terminate(ctx); err != nil {
            t.Logf("terminate postgres: %v", err)
        }
    })
    return connectionString
}
```

### Pinning (`testing.mdc` §7)

`testcontainers-go` and
`testcontainers-go/modules/postgres` are added to
`control-plane/job/go.mod`. Versions are pinned explicitly;
a future bump is the explicit ADR-required step
(`testing.mdc` §7). The Docker image tag
(`postgres:16-alpine`) is a constant in the helper; a
PostgreSQL 17 bump is a separate ADR.

## Consequences

### Positive

- The two pre-existing integration tests become
  **hermetic**, **CI-portable**, and **build-tag
  enforced** — the three properties the project rule
  required but that had not been satisfied.
- `t.Skip` for invariant tests is removed; the security-
  critical atomic-mutation contract is now caught at
  integration-test speed on every PR.
- The integration lane runs the same suite a developer
  runs locally — no drift between "what CI sees" and "what
  the developer saw".

### Negative

- The integration lane adds ~30 MB Docker image pulls per CI
  run (PostgreSQL + Ryuk reaper). The lane is opt-in via PR
  path filter; PRs that don't touch
  `control-plane/job/postgres/` pay nothing.
- One new module dependency (`testcontainers-go`) in
  `control-plane/job/go.mod`. The same dependency is already
  transitively present on the Java side
  (`org.testcontainers:postgresql`), so the dependency
  footprint is symmetric.
- Tests that previously skipped in 0.0s now take ~25 seconds
  when run under `-tags=integration`. Unit tests (without
  `-tags=integration`) stay fast.

### Neutral

- The shared helper file is used by both integration tests.
  A future third consumer may justify extracting a shared
  helper package.

## Alternatives Considered

### ADR-083 as written (helper module + three new consumer integration tests)

Build `control-plane/integration-test/postgres/` and add
integration tests inside `api-server`, `auth`, and
`compiler-validation` that drive a real PostgreSQL.

**Reject.** The pre-existing
`mutation_integration_test.go` already covers the
cross-module RBAC transaction boundary against a real
PostgreSQL — three new module-internal tests would
duplicate that coverage with smaller tests. The real gaps
are the build-tag, the `t.Skip`, and the missing CI lane,
not new tests.

### Inline helper duplicated across 2+ files (rejected ADR-083)

Extract a shared module the moment a third consumer needs
the helper.

**Accept in spirit, defer.** Two integration test files do
not justify a shared package; the duplication is 30 lines.
A future third consumer would justify the extraction. The
helper is small enough that the extraction is mechanical.

### Drop integration coverage; rely on e2e

Drop the two integration tests; rely on `tests/e2e/` to
catch SQL-level regressions.

**Reject.** `tests/e2e/` runs out-of-band (per
`testing.mdc` §5); the bug-discovery latency is hours, not
seconds. The integration lane is the right granularity for
the unit-test-speed feedback these regressions need.

### Keep `ASTRASYNC_TEST_POSTGRES_URL` as an escape hatch

Retain the env-var path so a developer pointing at a
long-lived dev PostgreSQL can skip the container boot.

**Reject.** The escape hatch re-introduces the
infrastructure dependency Phase 35 is removing. A developer
who wants a long-lived PostgreSQL can run
`docker compose up postgres` outside the test process; the
integration tests themselves must be hermetic.

## Follow-ups (out of scope for this ADR)

- **Envtest for the controller** (ADR-082 §Follow-ups):
  separate ADR. The `testcontainers-go` dependency added
  in Phase 35 does not conflict with controller-runtime
  envtest (different binary, different wait strategy).
- **Cross-module fixture live PostgreSQL upgrade**: when
  Phase 35's helper is stable, the cross-module fixture at
  `tests/cross-module/chain-tenant-id/` can be re-pointed
  from the dry-run migration parser to a real PostgreSQL.
  This is a separate ADR because it changes the
  cross-module PR lane from dependency-free to
  Docker-requiring.
- **OIDC testcontainers** (Keycloak / Authelia): separate
  ADR.
- **Shared helper extraction**: if a third integration test
  file needs the helper, extract
  `control-plane/integration-test/postgres/` as ADR-083
  originally proposed. ADR-083 is superseded today but
  remains a useful reference for the extraction shape.

## Rollback

Removing the `//go:build integration` tags, restoring the
`t.Skip` short-circuits, and deleting the CI workflow
reverts the slice. The unit test suite (which never built
the integration files) is unaffected.
