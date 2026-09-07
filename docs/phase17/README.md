# Phase 17: Observability Catalog Backlog Activation

## Status

**In Progress.** Phase 17 is the **umbrella** scope for closing the
descriptor-only / pending rows in `docs/observability/metrics-catalog.md`.
The umbrella decision is recorded in ADR-058. Each slice below is
landed as a separate PR.

## Goals

1. Activate every descriptor-only or unregistered business metric in
   the control-plane Prometheus inventory with a documented call site,
   label normalization contract, and test contract.
2. Introduce a shared `control-plane/observability/normalize` helper
   package so auth, api-server, controller, and scheduler metric
   packages enforce identical label-allowlist rules without
   duplicating helpers.
3. Keep the descriptor-vs-emitted table in `metrics-catalog.md`
   honest: every row points at the slice that owns it.

## Non-goals

- Re-touching ADR-047 or ADR-051.
- Unblocking the OpenMetrics content negotiation deferral recorded in
  ADR-051 §130.
- Touching the Java data-plane metrics (Phase 9 / ADR-051 already
  activated all seven families).
- Adding new metric names that are not in the catalog.
- Adding `request_id` exemplars (the existing deferred contract
  requires OpenMetrics negotiation first).

## Roadmap

| Slice | Description | Status | Owner metric(s) |
|-------|-------------|--------|-----------------|
| 43.0 | Umbrella: `control-plane/observability/normalize` package + Phase 17 README | Done (this slice) | — |
| 43.1 | API Server sign-in / session-revoke call sites | Pending | `apiserver_sign_in_total`, `apiserver_session_revoke_total` |
| 43.2 | Auth library observability (admin CLI + helpers) | Pending | `auth_sign_in_total`, `auth_session_revoke_total` |
| 43.3 | Controller state transition + epoch fence recorder | Pending | `controller_job_state_total`, `controller_epoch_fence_total` |

The slice numbering follows the Phase 17 directory layout
(`docs/phase17/43-observability-backlog/`).

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| ADR-058 accepted and indexed | Done (2026-09-07) |
| `control-plane/observability/normalize` package exists with `normalizeTenant`, `normalizeOutcome`, `normalizeWorkerID` | Pending (slice 0) |
| `apiserver_sign_in_total` has a real call site emitting non-zero samples | Pending (slice 1) |
| `apiserver_session_revoke_total` has a real call site emitting non-zero samples | Pending (slice 1) |
| `auth_sign_in_total` records admin CLI bootstrap flows (and any new auth helper) | Pending (slice 2) |
| `auth_session_revoke_total` records admin CLI revoke-session success boundary | Pending (slice 2) |
| `controller_job_state_total` descriptor + call site at Controller reconcile boundary | Pending (slice 3) |
| `controller_epoch_fence_total` descriptor + call site at Controller reconcile boundary | Pending (slice 3) |
| Every slice's test contract covers happy / rejected / failure paths and non-canonical UUID labels | Pending |
| `metrics-catalog.md` row status updates from "pending" to "emitted" as each slice lands | Pending |

## Backlog Snapshot (as of 2026-09-07)

| Metric | Owner | Descriptor | Call site | Reference |
|---|---|---|---|---|
| `apiserver_sign_in_total` | api-server | F4 | none | slice 43.1 |
| `apiserver_session_revoke_total` | api-server | F4 | none | slice 43.1 |
| `auth_sign_in_total` | auth library | F4 | none | slice 43.2 |
| `auth_session_revoke_total` | auth library | F4 | none | slice 43.2 |
| `controller_job_state_total` | controller | not registered | not registered | slice 43.3 |
| `controller_epoch_fence_total` | controller | not registered | not registered | slice 43.3 |

## Records

- [Design](43-observability-backlog/README.md) (slice 0 entry point)
- [ADR-058](../adr/adr-058-observability-catalog-backlog.md) — umbrella
  decision
- [ADR-047](../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
  — observability handbook context
- [ADR-051](../adr/adr-051-java-data-plane-metrics-activation.md)
  — Java data-plane metrics activation context
- [ADR-029](../adr/adr-029-durable-control-plane-job-lifecycle.md)
- [ADR-030](../adr/adr-030-lease-fenced-scheduler-dispatch.md)
- [ADR-031](../adr/adr-031-controller-convergence-and-ha.md)
- [metrics-catalog.md](../observability/metrics-catalog.md) — pending rows
- [v0.3.0 release cut](ADR-057) — Phase 17 starts from the v0.3.0 base
