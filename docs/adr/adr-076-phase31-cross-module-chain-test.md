# ADR-076: Phase 31 — Tenant-Id Envelope Cross-Module Chain Test

## Status

Proposed

## Context

Phase 30 (ADR-075) closed the **console-side** half of the tenant-id
envelope chain regression surface:

- `console/internal/server/chain_e2e_test.go` (3 cases) pins the join
  between the BFF egress boundary (Phase 28-A) and the Console
  `SyncJob` CR dual-write boundary (Phase 28-B), at unit-test speed.

But the **full chain** is one step longer:

```
client → BFF → API Server → PostgreSQL → API Server → Controller → metric
                                              ↑                  ↑
                                  ADR-074 server-side         ADR-066 / 069
                                  consumption                 emission
```

The two **controller-side** halves are independently pinned:

- `control-plane/controller/internal/controller/syncjob_emission_test.go`
  covers the controller's emission of
  `controller_job_state_total{tenant_id="..."}` and
  `controller_epoch_fence_total{tenant_id="..."}` from the CR label.
- `control-plane/api-server/internal/authn/interceptor_test.go` covers
  the API Server's reconciliation of `x-astra-tenant-id` metadata
  against the principal membership.

What is **not** pinned today is the **join** between (a) what the
Console sends as metadata and (b) what the API Server's
`Mutation.TenantID` ends up being when the durable `job.Job` row is
written. A future regression that drops the tenant-id between the
interceptor's `withJobTenantID` and `JobService.newJobMutation`
(for example, a refactor of `resolvedTenantIDForMutation` that
prefers the membership value over the metadata value when both are
present — the opposite of the ADR-074 §3 ordering) would:

- Pass the interceptor test (which only checks the membership ↔
  metadata match).
- Pass the controller emission test (which doesn't care about the
  mutation row's `tenant_id`).
- **Silently** produce a row where `job.Job.tenant_id` is the
  membership-derived value, which may differ from the metadata-
  declared value, which in turn may differ from the BFF's
  `scope.tenantID`.

This is the same class of bug Phase 30 (ADR-075 §Context) was written
to prevent — but for the server-side half of the chain.

The chain test cannot live in the console module today (no
controller-runtime dependency per ADR-073 §Implementation deltas.1)
and cannot live in the api-server module today (no console BFF
dependency). The console module's `chain_e2e_test.go` covers the
Console-side chain by capturing the **api-server-bound** metadata in
an in-process `jobMutationBackend`; that backend never sees a real
api-server interceptor.

A future regression that drops `Mutation.TenantID` between the
interceptor and `mutation_repository.createJobMutation` would not be
caught by any existing test surface.

## Decision

### 1. Phase 31 scope: cross-module chain test fixture

Phase 31 introduces a **cross-module test fixture** that exercises
the chain

```
BFF egress (Phase 28-A)
        ↓
API Server interceptor (Phase 29 / ADR-074)
        ↓
JobService mutation (this phase)
        ↓
PostgreSQL astrasync_control_jobs.tenant_id column (Phase 29 migration 003)
```

across the **console and api-server module boundary**, using a
single test binary. The fixture is purpose-built for the tenant-id
envelope contract; it does **not** attempt a generic cross-module
test framework.

### 2. Design principle: in-process, deterministic, no network

The cross-module test runs as a **single Go test binary** that
imports both modules. The fixture:

1. Boots an **in-process gRPC server** running
   `control-plane/api-server/cmd/server` style `*grpc.Server` with
   the real `JobService` registered.
2. Boots a **real `authn.Interceptor`** (no test stub) inside that
   gRPC server, so `withJobTenantID` actually attaches the verified
   tenant-id to the request context.
3. Uses an **in-memory `Memory.Repository`** (existing project
   pattern) for the api-server's PostgreSQL dependency, so the
   mutation repository writes into `Memory` rather than a real
   PostgreSQL. The test asserts on `Memory` directly: after the
   mutation, `repo.GetMutation(uid).TenantID` must equal the BFF
   metadata's `tenantID`.
4. Builds a **BFF client adapter** that wraps the existing
   `console/internal/server` handler stack: the test invokes the
   same code path the HTTP server invokes
   (`s.mutations.CreateJob(ctx, key, spec, idemKey)`) using
   `metadata.AppendToOutgoingContext(ctx, "x-astra-tenant-id",
   tenantID)` to set the metadata. The adapter does **not**
   instantiate an HTTP server; it calls the handler directly. This
   preserves the existing `chain_e2e_test.go` capture pattern.

The fixture is **in-process** — no Docker, no live K8s, no live
PostgreSQL. It runs under `go test -count=1` in the same CI lane as
the console module's `chain_e2e_test.go`.

### 3. Where the cross-module test binary lives

The cross-module test lives in a **new top-level directory**
`tests/cross-module/chain-tenant-id/` with its own `go.mod`. This
avoids forcing the `console` or `control-plane/api-server` module
to take on a dependency on the other. The pattern matches the
existing `tests/e2e/` directory (`tests/e2e/` is a separate module
per the project's multi-module layout).

The new module's `go.mod`:

```go
module io.astrasync/tests/cross-module/chain-tenant-id

go 1.26

require (
    io.astrasync/console v0.0.0
    io.astrasync/control-plane/api-server v0.0.0
    // transitive deps resolved by go mod tidy
)

replace (
    io.astrasync/console => ../../console
    io.astrasync/control-plane/api-server => ../../control-plane/api-server
)
```

This is the same `replace`-directive pattern already used by the
`control-plane/api-server` module to reference its sub-modules (see
`control-plane/api-server/go.mod`).

### 4. Test cases (5 total)

The fixture ships with five cases, each pinning a different join:

| Test | Boundary crossed | Asserts |
| --- | --- | --- |
| `TestCrossModule_ChainMatchesBFFScopeAndAPIServerMutation` | BFF egress **and** API Server interceptor **and** mutation repository | a single mutation call: BFF scope's `tenantID` == `x-astra-tenant-id` metadata == `Mutation.TenantID` == `repo.GetMutation(uid).TenantID` |
| `TestCrossModule_APIServerRejectsBFFMismatch` | BFF egress **and** API Server interceptor rejection | BFF sends `x-astra-tenant-id = T1` against principal membership for `T2`; api-server rejects with `codes.PermissionDenied` and emits `TENANT_DENIED` audit |
| `TestCrossModule_APIServerRejectsMalformedMetadata` | BFF egress **and** API Server envelope validation | BFF sends non-UUID `x-astra-tenant-id`; api-server rejects with `codes.PermissionDenied` and emits `TENANT_ENVELOPE_INVALID` |
| `TestCrossModule_NoTenantIDInMetadataFallsBackToMembership` | BFF egress (omitted) **and** API Server fallback | BFF sends no metadata; api-server derives `tenantID` from principal membership and writes that value to `repo.GetMutation(uid).TenantID` |
| `TestCrossModule_APIServerMigration003PersistsVerifiedTenantID` | API Server interceptor **and** mutation repository **and** migration `003_jobs_tenant_id.sql` | after migration applies, the `job.Job.tenant_id` column on the `astrasync_control_jobs` row matches the metadata-declared tenant-id |

Each test gets its own subtest with a fresh fixture so failures
attribute to a single chain step.

### 5. CI integration

The cross-module test runs:

- On `pull_request` and `push` to `main` / `develop` — under the
  existing `cross-module-tests` job in `ci.yml` (a new job; the
  existing console + api-server jobs are unchanged).
- On a separate `tests/cross-module/chain-tenant-id` lane with its
  own runner matrix (single ubuntu-latest, Go 1.26.x, no network
  access).

The cross-module test does **not** replace the per-module
`interceptor_test.go` and `chain_e2e_test.go`. The three test
surfaces are layered:

1. **Boundary-level** (existing, per module):
   `interceptor_test.go` (api-server) +
   `bff_slice28_test.go` (console) +
   `bff_slice28b_test.go` (console) +
   `syncjob_emission_test.go` (controller).
2. **Single-module chain** (Phase 30, ADR-075):
   `chain_e2e_test.go` (console, BFF egress ↔ Console CR dual-write).
3. **Cross-module chain** (Phase 31, this ADR):
   `tests/cross-module/chain-tenant-id/` (BFF ↔ api-server ↔
   mutation repository).

### 6. Why a separate `go.mod` and not a build tag

Two alternatives were considered and rejected:

- **A build-tag-gated `_test.go` file inside `console`**: rejected
  because it would require `console/go.mod` to import the
  api-server module, which is the dependency direction that ADR-073
  §Implementation deltas.1 explicitly avoided for the
  controller-runtime chain.
- **A build-tag-gated `_test.go` file inside `api-server`**: rejected
  for the same reason.

A standalone `tests/cross-module/...` module is the only option
that preserves both modules' independent dependency surfaces.

### 7. Migration `003_jobs_tenant_id.sql` coverage

Test case `TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`
exercises the **live migration** by:

1. Initialising the in-memory `Memory.Repository` against an empty
   schema.
2. Running the migration loader
   (`control-plane/job/postgres/migrations.Load(003)`) against a
   `pgx`-backed test database (`pgx` is already a transitive
   dependency of `control-plane/job/postgres`).
3. Issuing a single mutation through the fixture.
4. Asserting `repo.GetJob(uid).TenantID` matches the metadata.

The PostgreSQL test instance is supplied by
`github.com/testcontainers/testcontainers-go` (Postgres module) or
the existing `dockertest` pattern if already in use. If neither
library is acceptable (binary-size, CVE surface), the test falls
back to a **migration SQL parse + dry-run** assertion that
verifies the migration is **applied** at test setup time using the
project's existing `migrations.Manager.Apply` interface against the
in-memory adapter. The dry-run fallback is acceptable because the
column-existence assertion is the contract that matters; the data
shape is pinned by `Memory.Repository`'s separate test surface.

The fallback decision is recorded in
`docs/phase31/README.md` §Acceptance criteria and is not deferred
to a follow-up ADR.

### 8. Documentation

- This ADR.
- `docs/phase31/README.md` — describes the phase goal, the five
  cases, the cross-module module layout, and the migration test
  fallback decision.
- `docs/adr/README.md` index gains an ADR-076 row marked Proposed.
- `docs/phase30/README.md` "Follow-up" entry for "cross-module chain
  test" is **not** modified; it remains a follow-up until Phase 31
  ships.

### 9. Backwards compatibility

The phase is test-only. No production code, no schema migration,
no metric, no RBAC change. The existing module boundaries are
preserved: console does not import api-server, and api-server does
not import console.

## Consequences

### Positive

- The full tenant-id envelope chain (BFF egress → API Server
  interceptor → mutation repository → PostgreSQL column) is pinned
  end-to-end. A regression that drops the tenant-id at any one
  step surfaces in a single failing test.
- The cross-module fixture is **deterministic** and runs at unit
  test speed. No Docker, no network, no live K8s, no live
  PostgreSQL (when the dry-run fallback is used).
- The fixture preserves both modules' dependency surface; it does
  not introduce circular imports or force either module to depend
  on the other.
- The pattern (standalone `tests/cross-module/<name>/go.mod`) is
  reusable for future cross-module contract tests (e.g.
  cross-module checkpoint push, cross-region replication
  acceptance). This is a foundation, not a one-off.
- The five test cases each pin a **single** contract, so failures
  attribute cleanly: a test labelled
  `TestCrossModule_APIServerRejectsBFFMismatch` failing can only
  mean the api-server's reconciliation step regressed.

### Negative

- The fixture introduces a new top-level module
  (`tests/cross-module/chain-tenant-id/`). The project's module
  count grows from N to N+1. Module hygiene becomes a new
  responsibility (a future ADR may consolidate under a shared
  `tests/` workspace pattern).
- The cross-module test suite is **CI-only** unless explicitly
  invoked locally. Local developers running
  `go test ./console/...` will not see the cross-module tests.
  This is mitigated by the layered test surface (boundary tests
  still run per module).
- The migration test fallback (dry-run parse vs testcontainers)
  reduces the test's coverage of the actual SQL DDL. A future slice
  that wants to exercise the live migration against a real
  PostgreSQL must explicitly opt in.
- The fixture currently covers the **server-side** chain only.
  The full chain — Console → API Server → PostgreSQL → Controller
  → metric emission — would require a fourth module
  (`tests/cross-module/chain-tenant-id/controller/`); that is a
  Phase 32 follow-up, out of scope here.

## Alternatives Considered

### Single shared workspace mode with `go.work`

Use `go.work` to compose the console, api-server, and the new
cross-module test into a single workspace.

**Reject.** `go.work` is intended for local development only; the
project's CI does not currently use it, and adding it would change
the test orchestration model for every CI lane. A standalone module
is more invasive locally but matches the existing
`tests/e2e/` pattern.

### Inline the test inside the api-server module

Add a build-tag-gated test file under
`control-plane/api-server/tests/cross_module_chain_test.go` that
imports the console module.

**Reject.** This reverses the dependency direction: today api-server
has no dependency on console, and the dependency reversal would
couple api-server releases to console releases. The cross-module
test should not be a reason to invert that boundary.

### Defer to a future "test framework" ADR

Stand up a generic cross-module fixture framework before writing
the chain test.

**Reject.** A generic framework is a much larger investment. The
tenant-id envelope contract is a known, narrow contract; a
purpose-built fixture is faster, smaller, and clearer than waiting
for a generic framework.

### Skip Phase 31 entirely; rely on the boundary tests

The boundary tests already cover the four halves of the contract.

**Reject.** The boundary tests share the regression surface
described in ADR-075 §Context: a future refactor that swaps an
identity source between two halves can pass each boundary's
standalone test while silently breaking the chain. The cross-
module test pins the join at the only level that catches it.

## Follow-ups (out of scope for Phase 31)

- **Phase 32**: extend the cross-module fixture to cover the full
  chain Console → API Server → PostgreSQL → Controller → metric
  emission. This requires importing the controller module into the
  cross-module fixture, which is a separate dependency decision.
- **Live migration test**: replace the dry-run fallback with a
  real `testcontainers-go` PostgreSQL instance for the migration
  test case (`TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`).
- **Cross-region checkpoint push chain**: the same fixture pattern
  can be reused for the cross-region checkpoint push contract
  (ADR-052). That is a separate ADR.
