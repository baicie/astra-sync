# Phase 4 Slice 12 Design

## Goals

1. Admit PostgreSQL desired-state Jobs under one global, durable capacity limit.
2. Fence dispatch ownership by immutable Job UID, execution epoch, and renewable lease.
3. Materialize an arbitrary persisted JobSpec into an executable Kubernetes Coordinator/Worker
   group without storing connector credentials in a ConfigMap.
4. Translate execution start, completion, failure, and cancellation into the existing epoch-fenced
   lifecycle.
5. Preserve deterministic retries across Scheduler replicas and Kubernetes Job attempts.

## Non-goals

- Data-plane heartbeat ingestion or automatic failure based on missed heartbeats.
- Controller/SyncJob synchronization into the PostgreSQL authority.
- Shared multi-tenant Workers; the current Worker process owns one immutable JobSpec.
- Catalog lookup for `connectionRef`, credential rotation, or authorization policy.
- Multi-region active/active scheduling or a complete HA validation campaign.

## Durable Admission

`astrasync_scheduler_dispatches` stores one row per `(job_uid, execution_epoch)`. Active phases are
`CLAIMED`, `STARTING`, `RUNNING`, and `STOPPING`; terminal phases are `SUCCEEDED`, `FAILED`, and
`CANCELED`. The row also stores logical namespace/name, owner, lease expiry, takeover attempt,
diagnostic error, and timestamps. Foreign keys remove historical dispatches when the inactive Job
is eventually deleted.

Claim uses a transaction-scoped PostgreSQL advisory lock. Within that transaction it renews the
caller's rows, assigns expired active rows to the caller, counts every non-terminal row, and inserts
new rows up to the remaining capacity from Jobs whose status is `RUNNING/INITIALIZING`. Expired rows
remain in the active count. This is important: lease expiry transfers control of the deterministic
execution; it never creates spare capacity for an overlapping replacement.

Every phase mutation requires the current owner and an unexpired lease. A stale owner receives
`ErrLeaseLost` and cannot publish a terminal result. PostgreSQL optimistic Job versions remain the
second concurrency boundary for lifecycle writeback.

## Reconciliation

Each Scheduler tick claims work and reconciles owned rows concurrently under per-operation
deadlines shorter than the lease. Transient PostgreSQL or Kubernetes errors retain the active row,
renew the lease, and store the diagnostic. Permanent materialization errors advance the current
epoch to `FAILED` and terminate the dispatch.

The status writer retries optimistic conflicts and rereads UID/epoch before every transition. A
Coordinator can complete before its first Active observation; in that case the writer applies
`INITIALIZING -> RUNNING -> FINISHED` in order. A concurrent Stop wins by moving the Job to
`CANCELING`, after which success cannot bypass cancellation.

## Kubernetes Execution Group

The resource base is `job-<compact UUID>-e<base36 epoch>`, which remains a DNS label at the maximum
signed 64-bit epoch. Resources carry full UID and decimal epoch labels plus the JobSpec SHA-256
annotation. Existing resources with another identity or document are rejected as collisions.

The group contains:

- an immutable Secret with strict JobSpec JSON (also valid YAML);
- a headless Service and dedicated Worker StatefulSet mounting that Secret;
- a one-shot Coordinator Job created only after all Worker replicas report ready;
- one configured progress PVC mounted at a UID-specific directory.

Dedicated Workers preserve ADR-025: Coordinator and Workers compile the exact same immutable
JobSpec, while JDBC Sources and Sinks remain Worker-owned. The logical API namespace/name stay in
PostgreSQL and Kubernetes labels; the data-plane JobSpec name is a stable UID-derived runtime ID,
which prevents checkpoint collisions across namespaces and survives restarts.

Complete/Failed Job conditions are authoritative terminal observations. The Scheduler deletes
Workers, Service, and Secret, then records the terminal lifecycle. The Coordinator Job remains for
logs until TTL. Stop deletes the complete resource group and confirms absence before recording
`CANCELED`.

## Epoch Alignment

`ASTRASYNC_COORDINATOR_EXECUTION_EPOCH` is optional for legacy/local runs and required in dynamic
dispatch. `CheckpointBatchCoordinator` passes it to `CheckpointStore.acquireEpoch(job, plan,
epoch)`. `FileCheckpointStore` accepts the next epoch once, returns the active epoch for an
idempotent retry, and rejects older or skipped values. The existing checkpoint Worker protocol then
propagates that authoritative epoch to every task and acknowledgment.

## Deployment

The Helm chart runs three active Scheduler replicas by default. PostgreSQL leases, not Kubernetes
leader election, divide work. RBAC grants Jobs and the existing core/StatefulSet operations. The
chart creates a default progress PVC, supplies execution images and resources, and rejects a values
combination that also enables the legacy static Worker/Coordinator path.

CI tests all Scheduler packages, runs a real PostgreSQL capacity/takeover test, lints both dynamic
and manual Helm modes, and builds API, Controller, Scheduler, Worker, and Coordinator images.
