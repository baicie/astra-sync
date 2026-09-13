# ADR-075: Phase 30 — Tenant-Id Envelope End-to-End Regression Chain

## Status

Accepted

## Context

Phases 27–29 closed the **discrete halves** of the tenant-id envelope
contract on three trust boundaries:

- **SyncJob label contract** (ADR-071 / Slice 27-A): the
  `astrasync.io/tenant-id` label is required from trusted writers.
  ADR-086 later corrected the original, non-installable admission
  validation claim, and ADR-087 restored enforcement with a
  ValidatingAdmissionPolicy.
- **Console BFF egress** (ADR-072 / Phase 28-A): `x-astra-tenant-id`
  flows on every job mutation.
- **SyncJob CR creation** (ADR-073 / Phase 28-B): the Console
  dual-writes the CR after the durable PostgreSQL row, with the
  tenant-id label injected.
- **Server-side consumption** (ADR-074 / Phase 29): the API Server
  authn interceptor reconciles the metadata against the principal
  membership and persists the verified tenant-id into
  `job.Job.tenant_id` and the audit row.

Phases 27–29 each carry **unit-level** regression tests:

- `console/internal/server/bff_slice28_test.go` (Phase 28-A) pins the
  BFF egress contract.
- `console/internal/server/bff_slice28b_test.go` (Phase 28-B) pins the
  Console dual-write envelope (label injection, namespace routing,
  PG-first / CR-second ordering, disabled-manager bypass, failure
  modes).
- `control-plane/api-server/internal/authn/interceptor_test.go`
  (Phase 29) pins the server-side reconciliation contract.
- `control-plane/controller/internal/controller/syncjob_emission_test.go`
  pins the controller's `controller_job_state_total` emission under
  the canonical / missing-label / non-canonical label inputs.

But **no test currently crosses more than one boundary at a time**. A
regression in the boundary that joins Phase 28-A egress and Phase 28-B
dual-write (e.g. a future refactor of `createJob` swaps
`scope.tenantID` for `session.Principal.ID`, or a BFF migration
drops `tenantScopeCheck` from one of the six mutation paths) will
not be caught by either boundary's standalone suite. The four
deliverables above each pin their own boundary, but the join —
**the tenant-id UUID reaching the CR writer verbatim from the BFF
scope** — is the most fragile link because both ends read from the
same `scope.tenantID` variable.

The risk is concrete: the project will soon add a new mutation path
(planned for Phase 31 — job-level RBAC grant on membership rotation),
and that path will inherit `s.crWriter.WriteCR(..., scope, ...)`
from `createJob`. If `scope.tenantID` is silently swapped for a
session-bound identity (e.g. `session.Principal.ID`), the new path
will write a CR whose `astrasync.io/tenant-id` label is the
principal's OIDC subject, not the tenant UUID. The unit-level
`bff_slice28b_test.go` will still pass because it asserts on
`Scope.TenantID == scope.tenantID` directly — the regression is
silent because the boundary's standalone test is satisfied.

## Decision

### 1. New chain-level test surface: `console/internal/server/chain_e2e_test.go`

A new black-box test file (`package server_test`, consistent with
the project's black-box preference for BFF tests) ships three cases
that cross the BFF egress and Console dual-write boundaries in the
same HTTP request:

| Test | Boundary crossed | Asserts |
| --- | --- | --- |
| `TestConsoleTenantIDEnvelopeChain_WritesSurface` | BFF egress **and** CR dual-write | the same verified `tenantID` is observable on both (a) the api-server metadata captured by the fake backend and (b) the captured `WriteInput.Scope.TenantID`. A future refactor that swaps identity sources between (a) and (b) fails with `chain[egress]` or `chain[cr-write]` mismatch |
| `TestConsoleTenantIDEnvelopeChain_NoUnknownLeak` | scope-check atomicity | a cross-tenant `X-Astra-Tenant-ID` is denied with 403 *before* the BFF touches the backend or the CR writer; neither layer records the rejected tenant-id as `_unknown` |
| `TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR` | direct BFF→CR propagation | the CR writer's `WriteInput.Scope.TenantID` is pinned independently so a future refactor of `createJob` cannot silently swap `scope.tenantID` for a session-bound identity |

The tests reuse the existing helpers
(`jobMutationBackend`, `newCaptureCRManager`, `newSlice28bHandler`,
`bffRequest`, `jobMutationHeaders`) and the existing
`testTenantID` fixture; no new helper, no new test framework, no new
dependency. The black-box `package server_test` boundary means the
tests assert on the **exported contract** of the BFF + CR writer,
not on internals.

### 2. Why the chain test stays in the console module

The chain test **does not import `control-plane/controller`**. The
controller-runtime dependency chain is deliberately excluded from
`console/go.mod` (ADR-073 §Implementation deltas.1). Adding it back
would re-introduce the dependency that ADR-073 explicitly avoided.
Instead, the chain test pins the **Console-side chain** (BFF egress
+ CR dual-write), and the controller-side chain
(`controller_job_state_total{tenant_id="..."}` emission from the
label) is **already** pinned by
`control-plane/controller/internal/controller/syncjob_emission_test.go`
(ADR-066 §3 + ADR-069 §Slice 51.1). The two halves together close
the loop; the chain test is named so that a future maintainer
recognises the split.

### 3. What this slice does **not** add

- No new production code.
- No new metric.
- No new helm resource.
- No new RBAC binding.
- No new dependency.

The chain test is **observability** for the existing envelope
contract. The implementation deltas for the four halves (ADR-071
through ADR-074) are unchanged; this ADR pins the join.

### 4. Failure mode matrix

The chain test pins three failure modes the boundary-level tests
do not:

| Failure mode | Caught by |
| --- | --- |
| `createJob` calls `WriteCR` with `session.Principal.ID` instead of `scope.tenantID` | `chain[cr-write]` mismatch on `TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR` |
| `createJob` stops calling `WriteCR` entirely (regression) | `crCalls != 1` on `TestConsoleTenantIDEnvelopeChain_WritesSurface` |
| `tenantScopeCheck` is bypassed on a future mutation path | `chain[egress]` mismatch on `TestConsoleTenantIDEnvelopeChain_NoUnknownLeak` (backend `createCalls != 0`) |
| `WriteCR` runs **before** `mutations.CreateJob` (PG-first ordering broken) | the existing `bff_slice28b_test.go:TestConsoleCRPostgreSQLFirstOrdering` (already in place) |

The chain test deliberately does **not** retest any of the four
discrete halves; that is the boundary tests' job.

### 5. CI behaviour

The chain test runs under the existing `go test ./console/... -count=1`
target (deterministic, no network). It does not require a live K8s
cluster; the `captureCRWriter` is an in-memory recorder. The
`controller-runtime` is not exercised by the chain test.

A follow-up CI step may add `-run TestConsoleTenantIDEnvelopeChain`
as a nightly job to detect silent regressions across the full
chain, but the same effect is achieved by running the existing
`make test-go` (which already exercises the chain test alongside
all other console tests).

### 6. Documentation

- This ADR.
- `docs/phase30/README.md` — describes the phase goal, the three
  chain tests, and the split with the controller-side emission
  test surface.
- `docs/adr/README.md` index gains an ADR-075 row marked Accepted.

### 7. Backwards compatibility

The chain test is purely additive. No existing code or contract is
modified. A test-only addition is by definition wire-compatible.

## Consequences

### Positive

- The BFF egress and CR dual-write envelope is now pinned as a
  single chain. A regression that swaps `scope.tenantID` for a
  session-bound identity is caught by
  `chain[cr-write]` mismatch at the **first** test execution,
  rather than at production scrape time when the
  `controller_job_state_total{tenant_id="..."}` metric drifts.
- The chain test is **self-contained**: it reuses every helper
  that already ships in the console module's test surface. No new
  test framework, no new fixture, no new helper.
- The chain test is the documentation: a future maintainer reading
  `chain_e2e_test.go` learns that the envelope contract is
  "same tenant-id, two layers, single HTTP request". This is the
  cheapest form of documentation in the project.
- The split with `syncjob_emission_test.go` (controller-side) keeps
  `console/go.mod` free of controller-runtime, preserving the
  ADR-073 §Implementation deltas.1 decision.
- The chain test does not increase the test runtime materially; all
  three tests run in well under one second on the existing CI
  runner (verified locally: `go test ./internal/server/ -run
  TestConsoleTenantIDEnvelopeChain` reports < 0.01s per test).

### Negative

- The chain test covers the Console-side chain only. A future
  regression that touches the **server-side** consumption half
  (Phase 29 / ADR-074) is still caught only by
  `control-plane/api-server/internal/authn/interceptor_test.go`,
  not by the chain test. A future slice should add a cross-module
  chain test once the project settles on the cross-module test
  fixture pattern.
- The chain test asserts on the **observed** identity, not on the
  identity's *correctness* (e.g. it does not assert that the
  captured tenant-id is a member of the principal's `Memberships`
  set; that contract is the scope-check's job, tested by
  `bff_slice28_test.go:TestConsoleRejectsMutationWhenScopeMismatch`).
- The chain test's "no unknown leak" assertion depends on the
  existing `captureCRWriter.recorder` binding
  (ADR-073 §Implementation deltas — slice 28b test fixtures). A
  future refactor of the slice 28b helper that drops the recorder
  binding would silently break the chain test's metric observation
  without surfacing a failure. This is a minor test-only coupling.

## Alternatives Considered

### Add the chain test to the controller module instead

Move the chain test to `control-plane/controller` so it can
exercise the BFF → API Server → Controller chain with a single
test fixture.

**Reject.** The controller module is Go-native (controller-runtime,
`client.Client`), and re-creating the BFF and CR writer fixtures
inside the controller module would duplicate `bff_test.go`'s 600+
lines of helpers. The chain test's purpose is to pin the
**console-side** join, which the controller module cannot reach
without crossing the existing module boundary.

### Add a new end-to-end test in `tests/e2e/`

Stand up a `docker-compose` stack (Console, API Server, Postgres,
fake K8s API server) and exercise the chain through the full HTTP
surface.

**Reject for Phase 30.** The `tests/e2e/` stack is heavy
(multi-minute spin-up, requires Docker) and is owned by a separate
team. The chain test's purpose is **regression pinning at unit
test speed**, not end-to-end coverage. A future slice may add a
thin e2e test that runs the chain against the docker-compose stack
in CI, but it would be redundant with the chain test for the
boundary that joins the BFF and the CR writer.

### Add a runtime invariant in the production code

Add a `defer`-style check inside `createJob` that asserts
`scope.tenantID == writeCR.Scope.TenantID` after the dual-write
runs.

**Reject.** Runtime assertions in production code are not project
practice (`AGENTS.md §7` — anti-patterns). The chain test is the
right place for the invariant: it fails loudly in CI without
polluting the production binary.

### Defer Phase 30 to after Phase 31 (job-level RBAC grant)

Add the chain test only when the new mutation path lands.

**Reject.** The regression surface grows with every mutation path
added. Pinning the chain **before** the next slice is the
defensive move: a future slice that breaks the chain will fail
this test, alerting the author before the slice lands.

## Original proposal (kept for archival)

> See "Original proposal" below.
