# ADR-080: Phase 31 Cross-Module Fixture — Public-Surface Constraint

## Status

Proposed

## Context

The Phase 31 cross-module chain test fixture
(`tests/cross-module/chain-tenant-id/chain_test.go`) was first
drafted assuming it could import the real
`control-plane/api-server/internal/authn.Interceptor` and
`control-plane/api-server/internal/service.JobService`. A
pre-implementation Go-module audit revealed this assumption is
**incorrect** because of Go's `internal/` package rule.

### The `internal/` rule

Go's specification states:

> An import of a path containing the element "internal" is
> disallowed if the importing code is outside the tree rooted
> at the parent of the "internal" directory.

The api-server module's `authn` and `service` packages live under
`control-plane/api-server/internal/authn/` and
`control-plane/api-server/internal/service/`. Any test in a
sibling module — including a new `tests/cross-module/...`
module — is **outside** the api-server's source tree and cannot
import those packages. The Go toolchain rejects such imports at
build time.

### Why ADR-076 §2 assumed direct imports

ADR-076 §2.1 / §2.2 / §2.4 specify:

> 1. Boots an in-process gRPC server running
>    control-plane/api-server/cmd/server style *grpc.Server with
>    the real JobService registered.
> 2. Boots a real authn.Interceptor (no test stub) inside that
>    gRPC server, so withJobTenantID actually attaches the
>    verified tenant-id to the request context.

These clauses are unreachable from a sibling module. ADR-079 §1
inherited the assumption without auditing the Go module rule.

### Layered test surface (revisited)

The full chain has four testing layers (ADR-075 §5):

1. **Boundary-level (per module)**: `interceptor_test.go`
   (api-server), `bff_slice28_test.go` / `bff_slice28b_test.go`
   (console), `syncjob_emission_test.go` (controller).
2. **Single-module chain**: `chain_e2e_test.go` (console, Phase 30).
3. **Cross-module chain**: `tests/cross-module/chain-tenant-id/`
   (Phase 31).
4. **Live migration**: would need testcontainers (deferred).

Layer 1 already exercises the api-server interceptor and the
console BFF egress independently. Layer 3's job is therefore
**the join** between the two halves, not the halves themselves.

A cross-module test that exercises only the join can use the
**public** surface of each module:

- `console/internal/server.Config` and `server.NewWithConfig`
  expose the BFF egress path (which is the existing test surface
  in `chain_e2e_test.go`).
- `api-server/gen/go/v1.{JobServiceServer,JobServiceClient}` are
  public; the cross-module fixture can register its own
  `JobServiceServer` implementation that captures the metadata
  and asserts on the contract.

The interceptor itself does not need to be re-driven from a
sibling module; it is already covered by
`control-plane/api-server/internal/authn/interceptor_test.go` —
which is the appropriate location for that test, since the
interceptor IS api-server internals.

## Decision

### 1. Phase 31 cross-module fixture uses the public surface only

The fixture `tests/cross-module/chain-tenant-id/chain_test.go`:

- Imports `io.astrasync/console` (the BFF module) and
  `io.astrasync/control-plane/api-server/gen/go/v1` (the public
  gRPC service descriptors).
- Uses `console/internal/server.NewWithConfig` to drive the BFF
  side (same harness as `chain_e2e_test.go`).
- Registers a `JobServiceServer` implementation that captures
  the incoming metadata key `"x-astra-tenant-id"` (the constant
  is documented in ADR-079 §2 and ADR-074 §3; the test does not
  need to import `authn.TenantMetadataKey` because the constant
  is internal).

The captured metadata is asserted against the BFF egress
metadata, exercising the **join** between the two layers.

### 2. ADR-076 §2 is amended by reference

ADR-076 §2.1 / §2.2 are replaced with:

> 1. The cross-module fixture uses `console.NewWithConfig` to
>    drive the BFF side. The console handler routes the
>    `JobService.CreateJob` gRPC call through the BFF's
>    `Backend` interface.
> 2. The cross-module fixture registers a `JobServiceServer`
>    test implementation under `control-plane/api-server/gen/go/v1`
>    that satisfies `JobServiceServer` (embedding
>    `UnimplementedJobServiceServer`). The test server captures
>    incoming metadata and asserts on the tenant-id value.
> 3. The fixture does not drive the api-server interceptor
>    directly; the interceptor's behaviour is covered by
>    `interceptor_test.go` (Layer 1).

### 3. ADR-079 §1 / §4 are superseded by this ADR

The `recordingMutationRepository` pattern described in ADR-079
§1 is no longer needed: the cross-module fixture does not run
the api-server mutation service. The replacement test fake is
a `JobServiceServer` test stub whose `CreateJob` records the
incoming metadata.

### 4. Implications for the five test cases

- `TestCrossModule_ChainMatchesBFFScopeAndAPIServerMutation`:
  BFF egress `x-astra-tenant-id` == captured server metadata
  `x-astra-tenant-id`. The test asserts on the test stub's
  captured map, not on a real `Mutation.TenantID`.
- `TestCrossModule_APIServerRejectsBFFMismatch`,
  `TestCrossModule_APIServerRejectsMalformedMetadata`, and
  `TestCrossModule_NoTenantIDInMetadataFallsBackToMembership`:
  re-cast as **BFF ingress contract tests** — the BFF
  must reject malformed/missing tenant-id BEFORE reaching the
  api-server. The test asserts on the BFF's HTTP 4xx response,
  not on the interceptor.
- `TestCrossModule_APIServerMigration003PersistsVerifiedTenantID`:
  re-cast as a SQL parse + BFF egress contract test. The
  fixture no longer drives the api-server mutation path, so
  the "insert-path TenantID" assertion is replaced with a
  documentation test that the migration declares the column.

These recasts preserve the cross-module property (BFF + api-
server public surface) while keeping each case within Go's
`internal/` boundary.

### 5. New ADR row

`docs/adr/README.md` adds an ADR-080 row marked Proposed. The
CHANGELOG entry summarises the constraint and the recast.

## Consequences

### Positive

- The cross-module fixture compiles under `go mod tidy` and
  exercises the public surface of both modules.
- Each test case still pins a single chain boundary; failures
  attribute to a single module's responsibility.
- The interceptor's ordering rule continues to be covered by
  `interceptor_test.go` (Layer 1).
- The migration test continues to be parse-based; live
  PostgreSQL is a future slice (ADR-076 Follow-ups).

### Negative

- The cross-module fixture is **less ambitious** than ADR-076
  originally planned: it does not exercise the api-server
  mutation path end-to-end. A future slice could move the
  mutation-path coverage into the api-server module itself
  (e.g. `control-plane/api-server/internal/authn/cross_module_test.go`)
  and rely on Layer 1 + the cross-module public-surface test.
- The recast from "interceptor rejects" to "BFF ingress
  contract" is semantically weaker: it pins that the BFF does
  not forward a bad value, but not that the api-server would
  reject a forwarded bad value. The latter is covered by
  `interceptor_test.go`.
- This ADR supersedes two earlier ADRs (ADR-076 §2 and
  ADR-079 §1 / §4). The supersession is by reference; those
  ADRs are not edited in place.

## Follow-ups (out of scope for this ADR)

- **Live migration test**: when the project adopts
  testcontainers, the migration test can extend to cover the
  INSERT path against a real PostgreSQL (ADR-076 Follow-ups).
- **Layer 1.5 (api-server internal cross-module test)**: a
  future ADR may move the mutation-path coverage into the
  api-server module, complementing the public-surface test in
  this phase. The cross-module fixture remains the canonical
  Layer 3 entry; Layer 1.5 would be a Layer 2 expansion.
- **Phase 32**: extend the cross-module fixture to cover the
  Controller emission chain. The fixture pattern (public
  surface + test stub) generalises to that case.
