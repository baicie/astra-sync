# Phase 39 - Controller Manager Metrics Integration

## Status

**Complete.**

Phase 39 closes the Phase 36 follow-up for controller metrics by exercising the
real controller-runtime manager lifecycle instead of calling the reconciler or
Recorder directly.

ADR: [ADR-089](../adr/adr-089-controller-manager-metrics-integration.md)

---

## Goal

Verify the deployed controller path:

- controller registration through `SetupWithManager`
- envtest API-server discovery and cache synchronization
- `SyncJob` event delivery into `Reconcile`
- durable Job repository convergence
- Kubernetes finalizer and status projection
- tenant-aware reconcile duration metric emission

---

## Delivered Files

```text
control-plane/controller/internal/controller/
├── envtest_helper_test.go
└── manager_metrics_integration_test.go

docs/adr/
└── adr-089-controller-manager-metrics-integration.md

docs/phase39/
└── README.md
```

---

## Test Contract

`TestControllerManagerReconcileEmitsTenantMetricsIntegration`:

1. Starts envtest with the generated SyncJob CRD and tenant-label admission
   policy.
2. Starts a controller-runtime manager with network metrics and probes
   disabled.
3. Registers `SyncJobReconciler` through `SetupWithManager`.
4. Waits for the manager cache to synchronize.
5. Creates a canonical tenant-labelled `SyncJob`.
6. Waits until
   `controller_job_controller_reconcile_duration_seconds{tenant_id=<tenant>,outcome="success"}`
   has at least one sample.
7. Asserts that the memory Job repository contains `created/stopped` state and
   that the CR has the reconciled status and control-plane finalizer.

---

## CI Integration

The existing `control-plane-integration` workflow already installs envtest
binaries and runs all integration tests under
`control-plane/controller/internal/controller`. This phase adds the new file to
the build-tag guard so the integration gate cannot silently lose it.

---

## Acceptance Criteria

- [x] The envtest harness exposes its REST config and scheme.
- [x] The test starts a real controller-runtime manager.
- [x] The reconciler is registered through `SetupWithManager`.
- [x] Reconcile metrics are asserted from an isolated Prometheus registry.
- [x] Durable Job state and projected CR state are asserted in the same test.
- [x] The test has `//go:build integration`.
- [x] CI verifies the integration build tag.
- [x] ADR-089 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 39 entry.

---

## Non-Goals

- No PostgreSQL plus envtest combined test.
- No stale-writer epoch simulation.
- No production reconciler or metrics behavior change.
- No new Go module dependency.
- No change to controller-runtime versions.

---

## Rollback

Remove `manager_metrics_integration_test.go`, restore the envtest helper return
shape, and remove the Phase 39 documentation/index entries. Production behavior
is unaffected.
