# Phase 25: Emission Sub-Slice 51.1 — controller_epoch_fence_total via reconcile boundary

## Status

**Complete.** Phase 25 closes the emission sub-slice 51.1:
the `SyncJobReconciler` now calls `observeEpochFence` after every
successful `r.Jobs.Update` (the durable commit boundary, ADR-053 §3)
in three converge sites: the spec-change stop path, the desired-state
transition path, and the deletion path. The `controller_epoch_fence_total`
counter moves from "Recorder wired" to "emitted" in the metrics catalog.
The architecture decision is recorded in ADR-069. All slices (51.0–51.2)
are landed; the Phase 25 acceptance criteria are fully satisfied.

## Theme

Complete the `controller_epoch_fence_total` emission (Phase 17 activation
half → Phase 25 emission half). Phase 25 closes slice 51.1: wires the
controller reconcile durable-commit boundary to the metrics Recorder,
distinguishing clean writer fence-off (`fenced`) from no-epoch-change
updates (`success`) and misconfiguration fallbacks (`failure`).

## Scope

### Slices

| Slice | Owner | Description | Status |
|---|---|---|---|
| 51.0 | agent | ADR-069 + ADR index entry in `docs/adr/README.md` | **Done** |
| 51.1 | agent | `observeEpochFence` helper + wiring into three converge sites (`converge` spec-change stop, `converge` desired-state transition, `reconcileDeletion`); unit tests | **Done** |
| 51.2 | agent | Update `metrics-catalog.md` `controller_epoch_fence_total` row + status table + backlog table | **Done** |

## Acceptance Criteria

- [x] ADR-069 written, indexed in `docs/adr/README.md`, status = Accepted
- [x] `observeEpochFence` helper added to `SyncJobReconciler` in `control-plane/controller/internal/controller/syncjob_controller.go`
- [x] `observeEpochFence` called after every successful `r.Jobs.Update` in all three converge sites
- [x] `outcome` label derives correctly: `fenced` for `next.Status.Epoch > stored.Status.Epoch`, `success` for unchanged epoch, `failure` for lower epoch
- [x] `tenant_id` label read from SyncJob `astrasync.io/tenant-id` label; absent → `_unknown`
- [x] Helper is nil-safe: nil `Recorder` drops the call silently
- [x] `metrics-catalog.md` row updated to reflect the production call site
- [x] `docs/phase25/README.md` written
- [x] `CHANGELOG.md` updated under `[Unreleased]` / `Added`
- [x] All `go vet` and `go test` runs green for `control-plane/controller`

## Out of Scope

- Setting `astrasync.io/tenant-id` on SyncJob resources (Phase 23 slice 49.1.5): without it, all `controller_epoch_fence_total` emissions carry `_unknown` as the tenant label.
- The `controller_epoch_fence_total` metric is emitted at the controller reconcile boundary, not at the worker or scheduler boundary.
- The Phase 17 activation matrix's other pending emission (Java 26.F9 data-plane metrics, ADR-051 §7) is not in scope.

## References

- ADR-029 — Durable Desired-state Job Lifecycle
- ADR-030 — Lease-fenced Scheduler Dispatch
- ADR-053 — Checkpoint WAL and Promotion Recovery Orchestration
- ADR-058 — Observability Catalog Backlog (Phase 17 umbrella)
- ADR-066 — Phase 23 Controller emission (controller_job_state_total)
- ADR-068 — Phase 24 API Server session-revoke emission
- ADR-069 — Phase 25 controller epoch-fence emission (this phase)
