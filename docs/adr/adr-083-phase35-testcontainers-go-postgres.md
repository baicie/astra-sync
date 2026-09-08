# ADR-083: Phase 35 — Testcontainers-Go for Control-Plane PostgreSQL Integration Tests

## Status

Accepted

## Context

Phase 31 (ADR-076) introduced the cross-module tenant-id
envelope chain fixture at
`tests/cross-module/chain-tenant-id/`. The fixture drives the
**public surface** of `console` + `control-plane/api-server`
against a test fake implementing `bffbackend.Backend` and a
dry-run migration parser. ADR-076 §Follow-ups (re-iterated in
ADR-080 §Follow-ups) deferred a **live PostgreSQL** coverage
gap:

> **Live migration test**: when the project adopts
> testcontainers, the migration test can extend to cover the
> INSERT path against a real PostgreSQL (`TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`).
>
> **Cross-region checkpoint push chain** (ADR-052): a separate
> ADR.

The dry-run fallback in Phase 31's fixture is intentional — it
keeps the cross-module workflow dependency-free so it runs on
every PR lane. But three test surfaces have grown past the
dry-run ceiling:

1. **API Server mutation path against a real `jobs` table.**
   `control-plane/api-server/internal/service/job_mutation_service_test.go`
   and the `interceptor_test.go` Layer-1 tests cover the
   repository contract via in-memory fakes. The
   `INSERT … RETURNING id, version` path against a real
   PostgreSQL is exercised only in the smoke environment
   (`tests/e2e/`), which the project runs out-of-band. A
   regression that swapped `RETURNING id` for `RETURNING
   tenant_id` (or dropped the `version` optimistic-lock
   column) would not be caught until a real e2e run.

2. **Auth membership transaction boundary (ADR-037).**
   `control-plane/auth/internal/service/access_service_test.go`
   covers the `RBAC + audit` rollback contract via fakes.
   The exact shape of the rollback — `BEGIN; UPDATE
   memberships; INSERT INTO audit_events; COMMIT` — is
   PostgreSQL-specific and can drift in a future refactor.

3. **Compiler-validation migration discovery.**
   `control-plane/compiler-validation/internal/discovery_test.go`
   parses the `control-plane/job/postgres/migrations/*.sql`
   file list out of the repository tree. The migration **apply**
   path is not unit-tested today.

The project rule (`testing.mdc` §2 Go 测试) requires:

> 数据库 / etcd / OIDC 集成测试放进 `*_integration_test.go`
> 并加 build tag:
>
> ```go
> //go:build integration
> ```
>
> 或在文件名末尾 `_integration_test.go`。本地默认不跑;CI
> 单独跑。

The build-tag convention exists; the **mechanism** to actually
launch a PostgreSQL inside the integration test does not.
Three options exist today:

| Option | Cost | Coverage |
| --- | --- | --- |
| **Local PostgreSQL fixture** | `docker compose up postgres` per developer machine | Real, but manual and not CI-portable |
| **Stub the repository** (today's approach) | Zero infra cost | Misses SQL-level regressions |
| **testcontainers-go** | One new module dep, ~30 MB Docker image per CI run | Real SQL coverage, hermetic, CI-portable |

`testcontainers-go` (`github.com/testcontainers/testcontainers-go`)
is the de-facto Go standard for hermetic integration-test
containers; the project already references the equivalent
`org.testcontainers:testcontainers` (Java) in `pom.xml`
transitively through `quarkus-test-harness` (verified via
`mvn -pl cli dependency:tree`). Adopting `testcontainers-go`
on the Go side keeps the Java / Go test-stack symmetric — a
deliberate goal of `testing.mdc` §3.

## Decision

Phase 35 introduces a **single shared integration-test helper
module** at
`control-plane/integration-test/postgres/` that wraps
`testcontainers-go`'s PostgreSQL module
(`github.com/testcontainers/testcontainers-go/modules/postgres`),
and three **consumer refactors** that route the integration
boundary through the helper:

```
control-plane/integration-test/
└── postgres/
    ├── postgres.go              # NewContainer / Close / Migrate helpers
    ├── postgres_test.go         # Black-box smoke: container + Migrate
    └── README.md                # CI lane + local invocation notes

control-plane/api-server/
├── go.mod                       # + testcontainers-go (replace-directive,
│                                  indirect dep — see Pinning)
└── internal/service/
    └── job_mutation_service_integration_test.go
                                 # build tag //go:build integration

control-plane/auth/
├── go.mod                       # + testcontainers-go
└── internal/service/
    └── access_service_integration_test.go
                                 # build tag //go:build integration

control-plane/compiler-validation/
├── go.mod                       # + testcontainers-go
└── internal/discovery_integration_test.go
                                 # build tag //go:build integration
```

The helper exposes:

```go
// Container is a PostgreSQL 16 instance booted via
// testcontainers-go, with the migrations under
// control-plane/job/postgres/migrations applied.
type Container struct { … }

// NewContainer spins up a Postgres container, applies the
// schema migrations, and returns a *Container whose Pool is
// ready for use. The test MUST defer c.Close() (or
// t.Cleanup(c.Close)).
//
// The function is the only entry point; callers must NOT
// construct Container directly. The Docker image tag is
// pinned in postgresImage (Constant below) so a future
// PostgreSQL major-version bump is an explicit ADR.
func NewContainer(ctx context.Context, t TestingT) (*Container, error)

// Apply runs the SQL files under migrationsDir against the
// container. The migrations are loaded from disk to keep the
// helper ignorant of the migrations' content; the api-server
// module passes its own migrationsDir at call site.
func (c *Container) Apply(ctx context.Context, migrationsDir string) error

// Pool returns a *pgxpool.Pool connected to the container.
// Caller owns the pool and MUST Close() it before c.Close().
func (c *Container) Pool() *pgxpool.Pool
```

### Pinning (testing.mdc §7)

`testcontainers-go` is added to the **three consumer
modules**' `go.mod` (each module is independent per
`go-control-plane.mdc` §1 — no cross-module `replace`). The
version is pinned to the same minor version the project
references transitively for the Java side
(`org.testcontainers:postgresql:1.19.7`), so the Docker
image tag (`postgres:16-alpine`) and the Ryuk reaper
version stay in lock-step. A future minor-version bump is
the explicit ADR-required step
(`testing.mdc` §7).

### Build-tag convention (testing.mdc §2)

Each integration test file ends in
`_integration_test.go` AND carries
`//go:build integration` at the top — both, not either-or.
The `_integration_test.go` suffix is the convention the
go tooling picks up; the `//go:build integration` tag is the
**enforcement** that prevents `go test ./...` from running
the integration suite locally.

A new CI lane
`.github/workflows/control-plane-integration.yml` runs the
integration suite on PRs that touch the helper module, any
consumer module's `*_integration_test.go`, or the
`control-plane/job/postgres/migrations/` directory. The lane
reuses the existing `actions/checkout@v4` pattern; the
Docker daemon is provided by
`docker/setup-buildx-action@v3` (already pinned in the
catalog-check workflow). The integration lane does **not**
gate the unit-test lane — unit tests continue to run
without Docker.

### Why a shared module, not three independent helpers

Three modules independently pulling
`testcontainers-go/modules/postgres` would each pin the
PostgreSQL image tag in their own constants and risk drift
on the next image bump. The shared module centralises:

- the image tag (`postgresImage = "postgres:16-alpine"`),
- the wait strategy (`postgres.NewContainer` defaults are
  sufficient; no custom `WaitFor` is layered on top),
- the migration apply helper (single Apply implementation,
  shared by all three consumers),
- the cleanup contract (a single `Close` order).

The shared module also exposes a stable API surface so the
consumer tests are **portable across PostgreSQL
implementations** (the project rule
`connector-spi.mdc` §1 carries the same principle for
connector descriptors).

### What is explicitly NOT in Phase 35

- **Envtest for the controller.** The controller uses
  controller-runtime envtest (`envtest.Environment` +
  etcd binaries), which is a separate concern from
  PostgreSQL testcontainers. ADR-082 §Follow-ups defers
  envtest to its own ADR; this ADR does not pre-empt it.
- **OIDC testcontainers.** The OIDC integration tests today
  are mocked at the token-verification boundary
  (`control-plane/auth/internal/oidc/*.go`); bringing up a
  real Keycloak via testcontainers is a separate ADR.
- **Cross-module fixture upgrade.** Phase 31's cross-module
  fixture remains a dry-run fixture. Phase 35 does NOT wire
  the cross-module fixture to a live PostgreSQL — that
  upgrade is `tests/cross-module/chain-tenant-id/`'s own
  follow-up and would change the PR lane (the dry-run lane
  is dependency-free on purpose). The consumer refactors
  Phase 35 introduces are scoped to **single-package
  integration tests** inside the three control-plane
  modules.
- **Production code paths.** Phase 35 ships a helper module +
  three integration test files; it touches no production
  code. The repository contracts (job.Repository, audit
  Writer, compiler-validation discovery) are unchanged.

## Consequences

### Positive

- The three untested SQL-level paths get real coverage:
  `jobs` table INSERT/RETURNING, RBAC transaction
  rollback, migration apply.
- The CI lane runs hermetically — no developer machine
  setup, no `docker compose up postgres`, no shared
  PostgreSQL instance.
- The test-stack stays symmetric between Java and Go
  (both use `org.testcontainers:*` / `testcontainers-go`),
  so future phases can share CI lane structure.
- The build-tag convention is finally enforced by both the
  filename suffix AND the `//go:build integration` tag.

### Negative

- The integration lane adds ~30 MB Docker image pulls per CI
  run (PostgreSQL + Ryuk reaper). The lane is opt-in via
  PR path filter, so PRs that don't touch
  `control-plane/integration-test/` or any
  `*_integration_test.go` pay nothing.
- Three new indirect dependencies
  (`testcontainers-go`, `testcontainers-go/modules/postgres`,
  and `tc-otel` if metrics are enabled). Phase 35 does NOT
  enable metrics in testcontainers — the cost outweighs the
  benefit for a 30-second integration test.
- The shared helper module adds a fourth
  `control-plane/*` Go module. Project rule
  `go-control-plane.mdc` §1 already supports independent
  Go modules; this is not a new pattern.
- Tests that previously ran in < 1 second on the unit
  suite now take ~25 seconds (container boot + migration
  apply) when run under `//go:build integration`. The
  integration lane is opt-in; unit tests stay fast.

### Neutral

- The shared helper depends on the
  `control-plane/job/postgres/migrations/` directory at
  runtime via the call-site `migrationsDir` parameter — it
  does not import the migrations package directly. This
  keeps the helper module independent of any single
  consumer's migration set.

## Alternatives Considered

### Local PostgreSQL via `docker compose`

Document a `docker compose up postgres` step in
`CONTRIBUTING.md`; integration tests connect to
`localhost:5432`.

**Reject.** The integration lane then depends on every
developer machine having Docker, and every CI runner
having a long-lived PostgreSQL service container. The
testcontainers-go approach is hermetic and reproducible.

### In-memory PostgreSQL (`embedded-postgres`)

`github.com/fergusstrange/embedded-postgres` boots a real
PostgreSQL inside the test process, no Docker required.

**Reject.** Embedded PostgreSQL uses the host's glibc and
network stack; CI runners on Alpine-based images (the
project uses
`ubuntu-22.04` per
`.github/workflows/cross-module-chain-tenant-id.yml`)
require extra build-time dependencies. Docker is already a
CI prerequisite (catalog-check workflow uses it), so
testcontainers-go adds zero new CI infra.

### Three independent helpers (no shared module)

Each of the three consumer modules owns its own
testcontainers-go setup.

**Reject.** Each helper would pin its own PostgreSQL image
tag, its own wait strategy, and its own migration apply
logic. A future PostgreSQL 17 bump would require three
separate updates and three separate PRs. The shared module
collapses that to one constant change in one place.

### Drop integration coverage; rely on e2e

Drop the three integration tests; rely on
`tests/e2e/` to catch the SQL-level regressions.

**Reject.** `tests/e2e/` runs out-of-band (per
`testing.mdc` §5); the bug-discovery latency is hours, not
seconds. The integration lane is the right granularity for
the unit-test-speed feedback these regressions need.

## Follow-ups (out of scope for this ADR)

- **Envtest for the controller** (ADR-082 §Follow-ups):
  separate ADR. Phase 35 deliberately does not pre-empt
  it; the testcontainers-go dependency added in Phase 35
  does not conflict with controller-runtime envtest
  (different binary, different wait strategy).
- **Cross-module fixture live PostgreSQL upgrade**: when
  Phase 35's helper module is stable, the cross-module
  fixture can be re-pointed from the dry-run migration
  parser to `integration-test/postgres.NewContainer`. This
  is a separate ADR because it changes the cross-module
  PR lane from dependency-free to Docker-requiring.
- **OIDC testcontainers** (Keycloak / Authelia): separate
  ADR.
- **Reuse for Java tests**: the project already uses
  `org.testcontainers:postgresql` transitively on the
  Java side. A future slice can standardise a Java test
  helper at `engine/integration-test/postgres/` mirroring
  the Go helper.

## Rollback

Removing the helper module and the three
`*_integration_test.go` files reverts the slice. The unit
test suite (which never built the integration files) is
unaffected. The CI integration lane can be removed by
deleting
`.github/workflows/control-plane-integration.yml`. No
production code changed.
