# Phase 30 — Tenant-Id Envelope End-to-End Regression Chain

## Status

Active (chain tests shipped; ADR-075 Accepted)

## Goal

Pin the **join** between the two halves of the tenant-id envelope
contract that Phases 28 and 29 each test independently. The phase
ships only test code; no production code, no new metric, no new
dependency.

The chain under test is the Console-side path:

```
client → BFF mutation handler → scope().tenantID → │
                                              ├─ backend egress (x-astra-tenant-id)
                                              └─ CR dual-write (astrasync.io/tenant-id label)
```

The controller-side half of the chain (the
`controller_job_state_total{tenant_id="..."}` emission from the
label) is **already** pinned by
`control-plane/controller/internal/controller/syncjob_emission_test.go`
(ADR-066 §3 + ADR-069 §Slice 51.1). Phase 30 deliberately does
**not** import `control-plane/controller` from the console module —
the controller-runtime dependency is intentionally excluded
(ADR-073 §Implementation deltas.1).

## What ships in Phase 30

A single new black-box test file:

```
console/internal/server/chain_e2e_test.go
```

Three cases, all in `package server_test`:

| Test | Boundary crossed | Asserts |
| --- | --- | --- |
| `TestConsoleTenantIDEnvelopeChain_WritesSurface` | BFF egress **and** CR dual-write | same verified tenant-id on (a) backend egress metadata and (b) `WriteInput.Scope.TenantID` |
| `TestConsoleTenantIDEnvelopeChain_NoUnknownLeak` | scope-check atomicity | cross-tenant header denied with 403 before touching either backend or CR writer |
| `TestConsoleTenantIDEnvelopeChain_LabelPropagatesFromBFFToCR` | direct BFF→CR propagation | CR writer's `Scope.TenantID` is pinned independently against future `createJob` refactors |

Failure-mode coverage matrix (the chain test adds the rows marked
✦; the existing rows come from `bff_slice28b_test.go`):

| Failure mode | Caught by |
| --- | --- |
| `createJob` swaps `scope.tenantID` for `session.Principal.ID` | ✦ `chain[cr-write]` mismatch |
| `createJob` stops calling `WriteCR` entirely | ✦ `crCalls != 1` |
| `tenantScopeCheck` is bypassed on a future mutation path | ✦ `chain[egress] != 0` (backend `createCalls`) |
| PG-first / CR-second ordering broken | `TestConsoleCRPostgreSQLFirstOrdering` (existing) |
| PG failure aborts CR write | `TestConsoleCRWriteIsCalledOnlyAfterPGSuccess` (existing) |

## Non-Goals (this phase)

- No new production code.
- No new metric. `controller_job_state_total{tenant_id}` already
  exists (ADR-066 / ADR-069).
- No new helm resource. RBAC unchanged from Phase 28-B.
- No cross-module chain test (Console → API Server → Controller).
  That belongs to a future Phase (likely Phase 31 or later) when
  the project settles on a cross-module test fixture pattern.
- No live K8s cluster in CI. The chain test uses
  `captureCRWriter` (in-memory recorder).

## Acceptance criteria

- [x] Three tests in `chain_e2e_test.go` pass on
      `go test ./console/internal/server/ -run TestConsoleTenantIDEnvelopeChain -count=1`.
- [x] `go vet ./console/...` clean.
- [x] No new dependency in `console/go.mod`.
- [x] No new helm chart change.
- [x] `CHANGELOG.md` "Unreleased / Added" entry:
      "Console tenant-id envelope chain regression test (`chain_e2e_test.go`,
      ADR-075 / Phase 30) pins the BFF egress + CR dual-write join."
- [x] `docs/adr/adr-075-phase30-chain-tenant-id-regression.md`
      Accepted.
- [x] `docs/adr/README.md` index updated to mark ADR-073 Accepted
      and add ADR-075.

## Follow-up (deferred to a later phase)

- **Cross-module chain test (Console → API Server → Controller).**
  Would exercise the full `job.Job.tenant_id` column write through
  the API Server interceptor and the controller's metric emission
  end-to-end. Requires a cross-module test fixture; out of scope
  for Phase 30.
- **Nightly CI job** that runs `go test ./console/... -run
  TestConsoleTenantIDEnvelopeChain` independently of the main
  `make test-go` target, so a chain regression surfaces in PR
  review rather than in nightly monitoring.
- **Backfill `tenant_id` for pre-Phase-29 rows** (already tracked
  in `docs/phase29/README.md` follow-up).

## Rollback

The phase is test-only. Removing `chain_e2e_test.go` reverts the
slice with no production code impact.
