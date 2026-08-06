# Phase 4: Reconciliation Control Plane and HA

Phase 4 turns the Go control-plane skeleton into a durable desired-state system. Slice 11 defines
the versioned lifecycle contract and Slice 12 consumes it through a lease-fenced Scheduler that
admits and dispatches execution-scoped Coordinator/Worker resources.

**Status: Complete**

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 11 | Durable Job CRUD, Start/Stop lifecycle, PostgreSQL repository, API server, and SyncJob reconciliation foundation | Complete |
| 12 | Scheduler admission and Coordinator dispatch from durable desired state | Complete |
| 13 | Controller PostgreSQL convergence, execution heartbeat, orphan cleanup, and operational HA validation | Complete |

## Slice 11 Boundary

The control plane owns a canonical JobSpec compatible with the Java data plane: connector/options
source and sink definitions, ordered transforms, nested delivery guarantee, and bounded runtime
configuration. A Job is addressed by namespace and name, has an immutable UID, carries an
optimistic version, and records desired state, observed state, execution epoch, restart count,
checkpoint summary, and failure details.

PostgreSQL is authoritative for the gRPC and REST Job API. Start and Stop are idempotent desired-
state commands. Every accepted start increments the execution epoch; stale execution updates are
rejected by the domain state machine. The Kubernetes SyncJob schema uses the same JobSpec and state
vocabulary, and its controller applies desired-state changes idempotently under Lease-based leader
election.

Slice 11 does not launch a Java Coordinator, assign workers, or report live execution progress.
Those integrations consume this durable lifecycle in later Phase 4 slices.

## Slice 12 Boundary

The Scheduler reads only PostgreSQL desired state. A transaction-scoped advisory lock admits at
most the configured number of active `(job UID, execution epoch)` records. Each record has a
renewable lease and deterministic Kubernetes names, so a second Scheduler replica can take over an
expired record without creating a second execution identity. The dispatcher creates an immutable
JobSpec Secret, an execution-scoped Worker StatefulSet and headless Service, and a one-shot
Coordinator Job. Coordinator Job conditions are translated back to the epoch-fenced Job status;
Stop deletes the execution group and writes `CANCELED` only after all resources disappear.

The Coordinator accepts the control-plane epoch through `ASTRASYNC_COORDINATOR_EXECUTION_EPOCH`.
Checkpoint retries reuse the same epoch idempotently, while stale and skipped epochs are rejected.
Slice 12 does not add live heartbeats, long-running lease renewal from the data plane, or
multi-region failover; those operational guarantees remain in Slice 13.

## Slice 13 Boundary

The Controller imports SyncJob desired state and spec into the PostgreSQL lifecycle authority,
projects durable status back to the CRD, and uses a finalizer to stop active work before deleting the
row. Active spec changes are implemented as a stop, inactive spec replacement, and new execution
epoch. Optimistic versions and execution epochs remain the final write fences beneath Kubernetes
leader election.

The Coordinator sends authenticated execution heartbeats through the Scheduler Service. PostgreSQL
stores liveness independently from owner leases and atomically fences stale executions before
failure cleanup. Scheduler replicas adopt the same UID/epoch after owner failure, and the orphan
sweep removes only positively labeled resources while retaining known terminal Coordinator Jobs for
their TTL. Phase 4 remains single-region; cross-region active/active is outside this phase.

## Records

- [Slice 11](11-control-plane-job-lifecycle/README.md): [design](11-control-plane-job-lifecycle/design.md),
  [implementation plan](11-control-plane-job-lifecycle/implementation-plan.md), and
  [verification](11-control-plane-job-lifecycle/verification.md)
- [Slice 12](12-scheduler-dispatch/README.md): [design](12-scheduler-dispatch/design.md),
  [implementation plan](12-scheduler-dispatch/implementation-plan.md), and
  [verification](12-scheduler-dispatch/verification.md)
- [ADR-029: Durable Desired-state Job Lifecycle](../adr/adr-029-durable-desired-state-job-lifecycle.md)
- [ADR-030: Lease-fenced Scheduler Dispatch](../adr/adr-030-lease-fenced-scheduler-dispatch.md)
- [Slice 13](13-controller-ha/README.md): [design](13-controller-ha/design.md),
  [implementation plan](13-controller-ha/implementation-plan.md), and
  [verification](13-controller-ha/verification.md)
- [ADR-031: PostgreSQL Lifecycle Convergence and Execution Liveness](../adr/adr-031-controller-convergence-and-ha.md)
