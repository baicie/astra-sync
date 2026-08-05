# Phase 4: Reconciliation Control Plane and HA

Phase 4 turns the Go control-plane skeleton into a durable desired-state system. The first slice
defines one versioned job contract, persists lifecycle state in PostgreSQL, exposes gRPC and JSON
APIs, and establishes epoch-fenced reconciliation semantics for later Coordinator dispatch.

**Status: In Progress**

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 11 | Durable Job CRUD, Start/Stop lifecycle, PostgreSQL repository, API server, and SyncJob reconciliation foundation | Complete |
| 12 | Scheduler admission and Coordinator dispatch from durable desired state | Planned |
| 13 | Controller failover, execution observation, and operational HA validation | Planned |

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

## Records

- [Slice 11 usage and boundary](11-control-plane-job-lifecycle/README.md)
- [Design](11-control-plane-job-lifecycle/design.md)
- [Implementation plan](11-control-plane-job-lifecycle/implementation-plan.md)
- [Verification](11-control-plane-job-lifecycle/verification.md)
- [ADR-029: Durable Desired-state Job Lifecycle](../adr/adr-029-durable-desired-state-job-lifecycle.md)
