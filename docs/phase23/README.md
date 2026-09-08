# Phase 23: Emission Sub-Slice 43.3.5 — controller_job_state_total via reconcile boundary

## Status

**In Progress.** — see Slices below.

## Theme

Complete the `controller_job_state_total` emission (Phase 17 activation
half → Phase 23+ emission half). Phase 23 closes slice 43.3.5:
wires the controller reconcile-loop's durable commit point to the
controller metrics Recorder and establishes the K8s-label-based
tenant derivation contract for SyncJob resources.

## Scope

### Slices

| Slice | Owner | Description | Status |
|---|---|---|---|
| 49.0 | agent | ADR-066 + ADR index | **Done** |
| 49.1 | agent | SyncJob CRD godoc: document `astrasync.io/tenant-id` label requirement | **Done** |
| 49.2 | agent | Controller reconcile: `observeTransition` helper + wiring in `converge` (3 sites) and `reconcileDeletion` (1 site) | **Done** |
| 49.3 | agent | Tests: 6 cases covering label funnel + nil safety + no-op for unchanged state | **Done** |
| 49.4 | agent | Update `metrics-catalog.md` controller rows | **Done** |
| 49.5 | agent | Gate: `go vet`, `go test`, `check-changelog`, `release-dry-run` | pending |

## Acceptance Criteria

- [x] ADR-066 written and indexed in `docs/adr/README.md`
- [x] SyncJob CRD godoc documents `astrasync.io/tenant-id` label
- [x] `SyncJobReconciler` struct has `Recorder *metrics.Recorder` field
- [x] `SetupWithManager(manager, recorder)` accepts recorder parameter
- [x] `cmd/controller/main.go` passes `controllerMetrics` to SetupWithManager
- [x] `observeTransition` helper emits `ObserveStateTransition` after durable commit
- [x] 4 converge/reconcileDeletion call sites wired (2 in converge, 1 in reconcileDeletion, 1 covered by the deletion RequestStop path)
- [x] Nil-safety: `observeTransition` works with nil resource and nil recorder
- [x] No-op when `stored.Status.State == next.Status.State`
- [x] Tests cover happy + missing label + non-canonical tenant + `_platform` self-scope
- [x] `go vet ./control-plane/controller/...` exits 0
- [x] `go test ./control-plane/controller/...` exits 0
- [ ] `python scripts/check-changelog.py` exits 0
- [ ] `python scripts/release-dry-run.py` exits 0
- [ ] CHANGELOG `[Unreleased]` contains Phase 23 entry

## Non-Goals (Phase 23+)

- Slice 49.3.5: `controller_epoch_fence_total` reconcile-path emission (needs ADR-053 §3 durable commit decision)
- Slice 49.1.5: API server setting `astrasync.io/tenant-id` label on SyncJob creation + kubebuilder validation
- Slice 43.1.5: API Server sign-in handler emission (Phase 24+)
- Java data-plane emission (ADR-051 §7 `26.F9`) — Phase 25+ candidate

## ADR Cross-References

- [ADR-029](adr-029-durable-control-plane-job-lifecycle.md) — durable state machine
- [ADR-053](adr-053-production-hardening.md) — production hardening + durable reconcile safety
- [ADR-058](adr-058-observability-catalog-backlog.md) — Phase 17 activation matrix
- [ADR-066](adr-066-phase23-controller-reconcile-emission.md) — Phase 23: this phase's umbrella decision

## Key Design Decisions

### Durable commit point as the emission boundary

The reconcile-loop's durable commit point is the line where
`r.Jobs.Update(ctx, next, stored.Version)` returns `nil` (no error). At
that moment the job repository has accepted the new state; this is the
PostgreSQL / etcd-backed durable store that `docs/architecture.md`
§1.6 identifies as the coordination-metadata store. Emitting before
the commit would produce a "transient" metric sample that is rolled
back if the commit fails (e.g., on a version conflict). The emission
boundary is therefore post-commit.

### Tenant ID from K8s resource label, not job domain model

The `astrasync.io/tenant-id` label is the standard multi-tenancy
pattern for Kubernetes controllers. The alternative — adding
`TenantID` to the `job.Job` domain model — would touch the job
package, the memory and PostgreSQL repository implementations, the
API server job service, and every test that constructs a `job.Job`.
The K8s label approach achieves the same observability goal with a
one-line controller change. If the label is absent, `_unknown` is
emitted (the catalog row documents this limitation until slice
49.1.5 lands the API server wiring).

### Nil-safe Recorder

The Recorder field is separate from the `ReconcileMetrics` interface
because `ObserveStateTransition` is opt-in for tests. The Recorder
method itself is nil-safe (the metrics package returns early when the
receiver is nil), so callers do not need to nil-guard. This matches
the pattern documented in `authmetrics.ObserveSessionRevoke` (Phase 22
slice 48.2).
