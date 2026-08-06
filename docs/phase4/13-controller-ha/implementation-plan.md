# Phase 4 Slice 13 Implementation Plan

## 1. Contract and persistence

- [x] Define PostgreSQL as the lifecycle authority and document CRD import/status projection.
- [x] Add dispatch heartbeat persistence, execution tokens, and list-all-dispatch support.
- [x] Add heartbeat timeout configuration, takeover predicates, and atomic timeout fencing.

## 2. Controller convergence

- [x] Inject the shared Job repository into `SyncJobReconciler`.
- [x] Import CRD spec and desired state with optimistic conflict retries.
- [x] Stop active executions before importing a changed spec and restart when still desired.
- [x] Project PostgreSQL status through the status subresource on a periodic refresh.
- [x] Add deletion finalizer and inactive-row cleanup.

## 3. Execution liveness and cleanup

- [x] Send authenticated initial and periodic heartbeats from the Coordinator.
- [x] Fail executions that remain stale after an atomic heartbeat-timeout fence.
- [x] Resume `STOPPING` heartbeat cleanup after Scheduler owner failure.
- [x] Sweep only positively labeled resources outside the appropriate active/known keep set.
- [x] Preserve terminal Coordinator Jobs and deterministic UID/epoch fencing.

## 4. Verification and operations

- [x] Add Controller fake-client convergence, conflict, refresh, finalizer, and PostgreSQL tests.
- [x] Add heartbeat authentication, takeover, CAS race, and stale-owner fencing tests.
- [x] Add fake Kubernetes orphan-sweep tests.
- [x] Update Helm Controller/Scheduler configuration and internal heartbeat Service.
- [x] Add HA operations and failover verification records.
- [ ] Run Go tests/vet/gofmt, CRD generation, Helm lint/render, PostgreSQL integration, and
      container builds.
- [ ] Record verification evidence and merge the PR with squash.
