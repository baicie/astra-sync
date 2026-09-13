# Phase 18: Scheduler Metrics Normalize (Slice 44)

## Status

**Complete.** Phase 18 closes the last remaining control-plane
Recorder-owner gap. The umbrella decision is recorded in ADR-060;
Phase 17's three Recorder owners (api-server / auth / controller)
all route label values through `control-plane/observability/normalize`
per ADR-058 §3. The Scheduler metric package
(`control-plane/scheduler/internal/metrics`) now joins them: slice
44.0 + 44.1 + 44.2 land in this Phase. Slice 44.3 (connection-test
Recorder migrate from internal `Observe` collapse to
`observability/normalize`) is a future Phase 19 optional scope and
is recorded here for traceability.

## Goals

1. Add a Recorder to
   `control-plane/scheduler/internal/metrics` that owns dedicated
   `scheduler_job_assignment_total`,
   `scheduler_lease_takeover_total`, and
   `scheduler_job_reconcile_duration_seconds` CounterVec /
   HistogramVec, registered against an injected
   `prometheus.Registerer`.
2. Route every label value that derives from caller input through
   `io.astrasync/control-plane/observability/normalize` (slice 43.0):
   - `tenant_id` through `NormalizeTenant`;
   - `worker_id` through `NormalizeWorkerID`;
   - `outcome` through `NormalizeOutcome` with the documented
     allowlist per family (`success|rejected|failure` for
     `scheduler_job_assignment_total`; `success` for
     `scheduler_lease_takeover_total`).
3. Keep the package-level `JobAssignmentTotal` /
   `LeaseTakeoverTotal` / `JobReconcileDuration` `promauto` Vecs
   untouched so any pre-slice-44 import path continues to work.
4. Ship a test contract identical to Phase 17:
   happy / rejected / failure table-driven per call site +
   scrape-level assertion + cross-call cardinality bound +
   backward-compatibility assertion.

## Non-goals

- Re-touching api-server, auth, or controller metric packages —
  they are already wired through `normalize` (Phase 17).
- Touching replication metrics
  (`control-plane/replication/metrics`); its labels
  (`target_region` / `peer_region` / `event_type`) are not
  canonical-lowercase-UUID tenant or bounded worker id, and
  routing them through `normalize` would be a misuse. A future
  ADR can decide whether replication needs a `FreeText` helper
  (Phase 19 candidate).
- Java data-plane emission follow-up (ADR-051 §7 `26.F9`).
  Worker-protocol trust binding is out of scope for Phase 18.
- Touching the Scheduler long-running consumer's reconcile path
  to add a 43.3.5-style "Recorder wired" emission call site.
  Phase 18 closes the **Recorder** side; the **emission**
  call-site side is a separate Phase 19 candidate.

## Roadmap

| Slice | Description | Status | Owner metric(s) |
|-------|-------------|--------|-----------------|
| 44.0 | Umbrella: scheduler `go.mod` `observability` require + replace | Done | — |
| 44.1 | Scheduler Recorder + ObserveAssignment / ObserveLeaseTakeover / ObserveReconcile | Done | `scheduler_job_assignment_total`, `scheduler_lease_takeover_total`, `scheduler_job_reconcile_duration_seconds` |
| 44.2 | Table-driven tests + scrape-level + cardinality bound + backward-compat | Done | — |
| 44.3 | (Optional) connection-test Recorder migrate from internal `Observe` collapse to `observability/normalize` | Pending (depends on Phase 18 scope room after 44.2 lands) | `connection_test_total` |

The slice numbering follows the Phase 18 directory layout
(`docs/phase18/44-scheduler-metrics/`).

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| ADR-060 accepted and indexed | Done (2026-09-08) |
| `control-plane/scheduler/go.mod` has `require + replace io.astrasync/control-plane/observability` | Done (slice 44.0) |
| `scheduler_job_assignment_total` has a Recorder method routing every label value through `normalize` | Done (slice 44.1) |
| `scheduler_lease_takeover_total` has a Recorder method routing every label value through `normalize` (outcome allowlist `success`) | Done (slice 44.1) |
| `scheduler_job_reconcile_duration_seconds` Recorder funnels `tenant_id` through `normalize` | Done (slice 44.1) |
| Every Recorder method has table-driven tests for happy / rejected / failure paths and non-canonical UUID labels | Done (slice 44.2) |
| Scrape-level tests assert the metric appears with non-zero sample after the call site fires | Done (slice 44.2) |
| Cross-call cardinality bound test asserts N distinct inputs produce M bounded series | Done (slice 44.2) |
| Backward-compatibility test confirms the package-level `JobAssignmentTotal` / `LeaseTakeoverTotal` / `JobReconcileDuration` Vecs remain registered | Done (slice 44.2) |
| `scripts/run-go-modules.py vet` and `test` exit 0 across all 8 control-plane modules | Done |
| `python scripts/check-changelog.py` exits 0 | Done |
| `python scripts/release-dry-run.py` exits 0 | Done |

## Records

- [Design](44-scheduler-metrics/README.md) (slice 0 entry point)
- [ADR-060](../adr/adr-060-phase18-scheduler-metrics-normalize.md) —
  umbrella decision
- [ADR-058](../adr/adr-058-observability-catalog-backlog.md) —
  Phase 17 umbrella; Phase 18 follows the same pattern
- [ADR-051](../adr/adr-051-java-data-plane-metrics-activation.md) —
  emission follow-up `26.F9` lives here, out of Phase 18 scope
- [v0.4.0 release cut](../adr/adr-059-v0.4.0-release-cut.md) —
  Phase 17 ships as v0.4.0; Phase 18 is the next release cut
  (v0.5.0) candidate
