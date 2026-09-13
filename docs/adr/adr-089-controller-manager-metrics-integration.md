# ADR-089: Controller Manager Metrics Integration Test

## Status

Accepted

## Context

Phase 36 added envtest coverage for CRD validation, status subresources,
optimistic locking, and finalizer behavior. Phase 34 added unit coverage for
tenant-aware reconcile metrics at the `SyncJobReconciler` boundary.

Those tests do not prove that the production controller wiring works through a
real controller-runtime manager. A regression in `SetupWithManager`, cache
subscription, event delivery, metric registration, or reconcile execution could
leave every unit test green while the deployed controller never emits a metric.

## Decision

Add an envtest integration test that:

1. Reuses the Phase 36 envtest harness and exposes its Kubernetes REST config
   and scheme to controller-manager tests.
2. Starts a real controller-runtime manager with metrics and health endpoints
   disabled on the network.
3. Registers `SyncJobReconciler` through `SetupWithManager` with an isolated
   Prometheus registry.
4. Creates a valid tenant-labelled `SyncJob` and waits for the manager cache
   and reconcile loop to process it.
5. Asserts that the durable in-memory Job repository contains the reconciled
   `created/stopped` state, the Kubernetes status/finalizer are projected, and
   `controller_job_controller_reconcile_duration_seconds` has a successful
   sample for the SyncJob tenant.

The test remains behind the `integration` build tag and uses no new production
dependency.

## Consequences

- Controller metrics are now verified through the same manager lifecycle used
  by production, not only through direct Recorder or Reconciler calls.
- Cache synchronization, controller registration, event delivery, and metric
  tenant attribution are covered together.
- The integration workflow remains the sole owner of envtest binary setup.
- The runtime of the controller integration lane increases by one manager
  startup and one reconcile cycle.

## Rollback

Remove the manager metrics integration test and restore the envtest helper to
its single-client return shape. Production controller behavior and metrics
contracts are unchanged.
