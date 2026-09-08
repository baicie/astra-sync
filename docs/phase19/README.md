# Phase 19: Connection-Test Recorder Migrate (Slice 45)

## Status

**Complete.** Phase 19 closes the last remaining duplicated
`normalizeLabel`-style helper in the control plane. The umbrella
decision is recorded in ADR-061; the connection-test Recorder
(`control-plane/scheduler/internal/connectiontestmetrics`) now
joins api-server, auth, controller, and Scheduler as a consumer
of the slice-43.0 `io.astrasync/control-plane/observability/normalize`
package. The connection-test Recorder's observable contract is
unchanged: same allowlist (`success | rejected | failure`), same
`_unknown` fallback for empty / whitespace tenant, same nil-receiver
no-op semantics. Slice 45.0 + 45.1 + 45.2 land in this Phase.

## Goals

1. Replace the connection-test Recorder's internal
   `strings.TrimSpace` / `if tenantID == ""` collapse with a
   single call to `normalize.NormalizeTenant`.
2. Replace the connection-test Recorder's internal
   `switch outcome { ... }` allowlist collapse with a single
   call to `normalize.NormalizeOutcome` with the documented
   `success | rejected | failure` allowlist and `failure` as
   the default value.
3. Promote the `OutcomeSuccess` / `OutcomeRejected` /
   `OutcomeFailure` constants and the `outcomeAllowlist`
   slice to a single source of truth inside the package so
   future contributors can reuse them.
4. Keep the package-level `ConnectionTestTotal` `promauto`
   CounterVec registered against the default registry so any
   pre-slice-45 import path continues to scrape the same
   series.
5. Ship a test contract identical to Phase 18:
   happy / rejected / failure table-driven + non-canonical
   UUID collapse + outcome allowlist collapse + cross-call
   cardinality bound + nil-receiver safety + sentinel error
   contracts + backward-compatibility assertion.

## Non-goals

- Re-touching api-server, auth, controller, or Scheduler
  metric packages — they are already wired through `normalize`.
- Touching replication metrics
  (`control-plane/replication/metrics`); its labels are not
  canonical-lowercase-UUID tenant or bounded worker id, and
  routing them through `normalize` would be a misuse (recorded
  in ADR-060 §1 and ADR-061 §1).
- Java data-plane emission follow-up (ADR-051 §7 `26.F9`).
  Worker-protocol trust binding is out of scope for Phase 19.
- Emission sub-slices (43.1.5 / 43.2.5 / 43.3.5); those have
  open ownership questions (API Server sign-in handler needs
  new RPC + RBAC role — AGENTS.md §8 decision gate) and are
  not a single coherent Phase.

## Roadmap

| Slice | Description | Status | Owner metric |
|-------|-------------|--------|--------------|
| 45.0 | Umbrella (ADR-061) + Scheduler `go.mod` observability require check | Done | — |
| 45.1 | connection-test Recorder migrates from internal collapse to `observability/normalize` | Done | `connection_test_total` |
| 45.2 | Table-driven tests + scrape-level + cardinality bound + backward-compat + sentinel errors | Done | — |

The slice numbering follows the Phase 19 directory layout
(`docs/phase19/45-connection-test-recorder/`).

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| ADR-061 accepted and indexed | Done (2026-09-08) |
| `connectiontestmetrics.Observe` routes tenant_id through `NormalizeTenant` | Done (slice 45.1) |
| `connectiontestmetrics.Observe` routes outcome through `NormalizeOutcome` with documented allowlist `success\|rejected\|failure` and default `failure` | Done (slice 45.1) |
| `outcomeAllowlist` slice exposed as the source of truth for the connection-test outcome contract | Done (slice 45.1) |
| Every Recorder method has table-driven tests for happy / rejected / failure paths and non-canonical UUID labels | Done (slice 45.2) |
| Scrape-level tests assert the metric appears with non-zero sample after the call site fires | Done (slice 45.2) |
| Cross-call cardinality bound test asserts N distinct inputs produce M bounded series | Done (slice 45.2) |
| Nil-receiver safety test asserts Recorder methods are no-ops | Done (slice 45.2) |
| Sentinel error contracts: `ErrNilRegisterer` + `ErrDuplicateMetric` exposed via `errors.Is` | Done (slice 45.2) |
| Backward-compatibility test confirms the package-level `ConnectionTestTotal` Vec remains registered | Done (slice 45.2) |
| `scripts/run-go-modules.py vet` and `test` exit 0 across all 8 control-plane modules | Done |
| `python scripts/check-changelog.py` exits 0 | Done |
| `python scripts/release-dry-run.py` exits 0 | Done |

## Records

- [Design](45-connection-test-recorder/README.md) (slice 0 entry point)
- [ADR-061](../adr/adr-061-phase19-connection-test-recorder-migrate.md) —
  umbrella decision
- [ADR-060](../adr/adr-060-phase18-scheduler-metrics-normalize.md) —
  Phase 18 (Phase 19 follows the same template)
- [ADR-058](../adr/adr-058-observability-catalog-backlog.md) —
  Phase 17 umbrella; the connection-test Recorder is the fifth
  control-plane Recorder owner
- [v0.4.0 release cut](../adr/adr-059-v0.4.0-release-cut.md) —
  Phase 17 ships as v0.4.0; Phase 18 ships as next release cut
  candidate; Phase 19 is the v0.5.0 candidate
