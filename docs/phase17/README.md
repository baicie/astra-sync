# Phase 17: Observability Catalog Backlog Activation

## Status

**Complete.** Phase 17 is the **umbrella** scope for closing the
descriptor-only / pending rows in `docs/observability/metrics-catalog.md`.
The umbrella decision is recorded in ADR-058. The release cut is
recorded in ADR-059. Each slice below is landed as a separate PR.

Phase 17 closes the **activation** half of the catalog backlog
(Recorder + label-allowlist reuse + test contract). The
**emission** half (transitioning catalog rows from "Recorder
wired" to "emitted") requires a non-zero production sample and is
owned by Phase 18+ — see ADR-059 §1 for the scope split.

**Updated 2026-09-08**: Slice 43.1.5 landed with Console BFF
`CompleteLogin` wired to `authmetrics.Recorder.ObserveSignIn`. The
`auth_sign_in_total` and `apiserver_sign_in_total` families are now
fully emitted (Console BFF owns the sign-in path; API Server
`AuthService.Login` is a separate entry point handled by a future
slice).

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
| 43.0 | Umbrella: `control-plane/observability/normalize` package + Phase 17 README | Done | — |
| 43.1 | API Server sign-in / session-revoke recorder wiring through `normalize` | Done (recorder + scrape contract; production call site deferred to slice 43.1.5 once the auth flow surface is decided) | `apiserver_sign_in_total`, `apiserver_session_revoke_total`, `apiserver_trusted_proxy_hsts_total` |
| **43.1.5** | **Console BFF `CompleteLogin` sign-in emission via `authmetrics.Recorder`** | **Done (2026-09-08)** | `auth_sign_in_total`, `apiserver_sign_in_total` |
| 43.2 | Auth library observability (admin CLI + helpers) | Done (Recorder + `HandlerFor`; admin CLI integration deferred to slice 43.2.5 because the admin CLI is one-shot and the Recorder only lands a sample if a long-running consumer exposes the Recorder-owned registry) | `auth_sign_in_total`, `auth_session_revoke_total` |
| 43.3 | Controller state transition + epoch fence recorder | Done (recorder + scrape contract; reconcile-path wiring deferred to slice 43.3.5 once the durable state-transition commit and Scheduler fence-response ownership decision is made) | `controller_job_state_total`, `controller_epoch_fence_total` |

The slice numbering follows the Phase 17 directory layout
(`docs/phase17/43-observability-backlog/`).

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| ADR-058 accepted and indexed | Done (2026-09-07) |
| `control-plane/observability/normalize` package exists with `normalizeTenant`, `normalizeOutcome`, `normalizeWorkerID` | Done (slice 43.0) |
| `apiserver_sign_in_total` has a Recorder method routing every label value through `normalize` | Done (slice 43.1); production call site deferred to slice 43.1.5 |
| `auth_sign_in_total` / `apiserver_sign_in_total` emitted by Console BFF `CompleteLogin` | Done (slice 43.1.5, 2026-09-08); `authmetrics.Recorder` wired; DENIED paths emit `rejected`, session-create failure emits `failure`, success emits `success`; `tenant_id` derived from first principal membership or `_platform` |
| `apiserver_session_revoke_total` has a Recorder method routing every label value through `normalize` | Done (slice 43.1); production call site deferred to slice 43.1.5 |
| `apiserver_trusted_proxy_hsts_total` routes the pre-auth tenant through `normalize` | Done (slice 43.1) |
| `auth_sign_in_total` records admin CLI bootstrap flows (and any new auth helper) | Done (slice 43.2); the admin CLI is one-shot and does not bind a /metrics port — the Recorder + `HandlerFor(gatherer)` pair is wired through the slice-43.2 contract, but the admin CLI integration is deferred to slice 43.2.5 once a long-running consumer exposes the Recorder-owned registry |
| `auth_session_revoke_total` records admin CLI revoke-session success boundary | Done (slice 43.2); admin CLI integration deferred to slice 43.2.5 for the same reason |
| `controller_job_state_total` Recorder routing every label value through `normalize` | Done (slice 43.3); reconcile-path wiring deferred to slice 43.3.5 |
| `controller_epoch_fence_total` Recorder routing every label value through `normalize` | Done (slice 43.3); reconcile-path wiring deferred to slice 43.3.5 |
| Every slice's test contract covers happy / rejected / failure paths and non-canonical UUID labels | Done (slices 43.1 + 43.2 + 43.3) |
| `metrics-catalog.md` row status updates from "pending" to "Recorder wired" as each slice lands | Done (slices 43.0 + 43.1 + 43.2 + 43.3); the row flips to "emitted" only after a non-zero sample is observed in production — that emission milestone is owned by Phase 18+, not by Phase 17 (ADR-059 §1) |
| `control-plane/controller` no longer carries duplicated `normalizeTenant` / `normalizeOutcome` helpers — all metric packages route through the shared `observability/normalize` package | Done (slice 43.3) |

## Backlog Snapshot (as of 2026-09-08)

| Metric | Owner | Descriptor | Call site | Reference |
|---|---|---|---|---|
| `apiserver_sign_in_total` | api-server / Console BFF | F4 | **Emitted** (slice 43.1.5); Console BFF `CompleteLogin` wires `authmetrics.Recorder`; outcome: `success`/`rejected`/`failure`; `tenant_id` from principal memberships or `_platform` | slice 43.1.5 (ADR-066) |
| `apiserver_session_revoke_total` | api-server | F4 | Recorder wired (slice 43.1); production call site pending | slice 43.1 |
| `apiserver_trusted_proxy_hsts_total` | api-server | F4 | Recorder wired (slice 43.1); the existing trusted-proxy observer now funnels through `normalize` | slice 43.1 |
| `auth_sign_in_total` | auth library | F4 | **Emitted** (slice 43.1.5); same Console BFF call site as `apiserver_sign_in_total`; `authmetrics.Recorder` routes through `normalize` | slice 43.1.5 (ADR-066) |
| `auth_session_revoke_total` | auth library | F4 | **Emitted** (Phase 22 slice 48, ADR-065); admin CLI `revoke-session` via log-dump emission | Phase 22 slice 48 (ADR-065) |
| `controller_job_state_total` | controller | F4 | **Emitted** (Phase 23 slice 49, ADR-066); controller reconcile boundary after `r.Jobs.Update(...)` returns nil | Phase 23 slice 49 (ADR-066) |
| `controller_epoch_fence_total` | controller | F4 | Recorder wired (slice 43.3); reconcile-path wiring pending | slice 43.3 |

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
