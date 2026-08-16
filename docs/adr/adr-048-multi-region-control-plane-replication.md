# ADR-048: Multi-Region Control-Plane Replication Model

## Status

Proposed (Phase 7 Slice 25 design cluster)

## Context

ADR-029 records that the durable Job record, the connection catalog,
and the audit table live in PostgreSQL as the authoritative store.
ADR-007 records that coordination metadata, large state, and bulk
data use dedicated storages (PostgreSQL / etcd / object storage).
ADR-010 records that a Job has one active region at a time and that
checkpoint manifests replicate asynchronously to a standby region
for disaster recovery.

Phase 7 Slice 25 lifts the single-region constraint for the
control plane while preserving the single-active-region constraint
for the data plane. The lift requires an explicit replication
model: how the secondary region's PostgreSQL stays in sync with the
primary region's PostgreSQL, how the durable Job record is readable
from the secondary region without a synchronous cross-region call,
and how the checkpoint store replicates so the secondary region can
resume from the latest completed checkpoint.

Without an explicit model, the secondary region either drifts
silently (which violates ADR-029's optimistic-version invariant) or
requires a synchronous cross-region call in the steady state (which
violates ADR-010's "no synchronous cross-region dependency"
boundary).

## Decision

The multi-region control plane uses an **asynchronous PostgreSQL
replication topology** with three replication targets:

1. **Coordinating metadata replication.** The Job lifecycle
   (ADR-029), connection catalog (ADR-040), and audit table
   (ADR-042) are replicated to the secondary region through the
   same replication channel. The replication is asynchronous and
   per-table; the secondary region reads a snapshot that may lag
   the primary region by `replicationLagThreshold` (default 5
   seconds, configurable via the Helm chart value).
2. **Coordination metadata replication.** The etcd cluster used for
   epoch assignment (ADR-006) and Controller / Scheduler lease
   election (ADR-030, ADR-031) is **not** replicated across regions.
   Each region owns its own etcd cluster; the epoch sequence is
   monotonic across regions because every region reads from the
   primary region's last-known epoch via the replicated Job record.
   The cross-region epoch ordering is enforced by the optimistic
   version on the Job record, not by a shared etcd.
3. **Checkpoint replication.** The state backend's checkpoint
   manifests (ADR-026, ADR-034) are replicated through the
   object-storage-backed replication channel. The transport is
   asynchronous; the secondary region reads the latest completed
   checkpoint from the object store.

### Replication Channels

The replication is **deployment-side, not repository-side.** The
repository records the contract; the deployment chooses the
implementation. The contract is:

- **Consistency model.** Asynchronous. The secondary region reads
  the last-replicated snapshot. The replication lag is bounded by
  `replicationLagThreshold`; a failover that exceeds the bound is
  recorded as an audit event of type `region-failover-rpo-exceeded`.
- **Conflict resolution.** Last-writer-wins on the Job record's
  optimistic version column. The secondary region's writes during
  the failover window are recorded as audit events of type
  `region-write-conflict`; the operator resolves the conflict
  manually.
- **Failure mode.** The secondary region continues to serve
  read-only requests while the replication channel is degraded.
  The secondary region refuses to enter "failover-pending" mode if
  the replication lag exceeds `replicationLagThreshold` and the
  primary region is reachable; the operator must explicitly
  override.
- **Recovery.** The secondary region catches up by replaying the
  replication log. The replay is bounded by the log retention
  window (default 7 days, configurable via the Helm chart value
  `multiRegion.replicationLogRetention`).

### Cross-Region gRPC Channel

The cross-region gRPC channel between the primary region's API
Server and the secondary region's API Server uses the Phase 7
Slice 23 mutual-TLS boundary (ADR-045). The channel carries
replication events, not user requests. User requests stay
region-local.

### Why Asynchronous, Not Synchronous

A synchronous cross-region PostgreSQL replication would violate
ADR-010's "no synchronous cross-region dependency" boundary and
would couple every API Server call to a network round-trip. The
async model is consistent with ADR-029's optimistic-version
contract: the secondary region reads the last-replicated snapshot
and accepts that the snapshot may be stale.

The async model is also consistent with ADR-006's epoch ordering:
the secondary region's epoch sequence starts from the
last-replicated epoch in the Job record and increments locally.
Two regions never assign the same epoch to the same Job because
the optimistic-version check rejects a stale epoch.

## Consequences

### Positive

- The secondary region can serve read-only requests without a
  synchronous cross-region call. The replication lag is bounded.
- The durable Job record, the connection catalog, and the audit
  table are all replicated through the same channel, so the
  secondary region's read snapshot is internally consistent.
- The replication transport is deployment-owned. The repository
  records the contract, not the implementation.
- The optimistic-version conflict resolution is consistent with
  ADR-029 and does not require a new schema field.

### Negative

- The replication lag bounds the failover's RPO. A failure that
  happens before the latest checkpoint replicates recovers from
  the last replicated checkpoint and re-executes the uncommitted
  batch. The re-execution is bounded by ADR-027's "transactional
  or idempotent" rule.
- The cross-region gRPC channel is a new network surface. The
  Phase 7 Slice 23 mTLS boundary applies, but the channel's
  failure mode must be tested.
- The "last-writer-wins" conflict resolution requires an
  operator-driven reconciliation step. The implementation slice
  must record the reconciliation procedure in a runbook template.

## References

- [ADR-007: Storage Separation (PostgreSQL/etcd/Object Storage)](adr-007-storage-separation.md)
- [ADR-010: Single Active Region per Job](adr-010-single-active-region.md)
- [ADR-026: Durable Checkpoint and Epoch Fencing Foundation](adr-026-checkpoint-fencing-foundation.md)
- [ADR-029: Durable Desired-state Job Lifecycle](adr-029-durable-desired-state-job-lifecycle.md)
- [ADR-030: Lease-fenced Scheduler Dispatch](adr-030-lease-fenced-scheduler-dispatch.md)
- [ADR-031: PostgreSQL Lifecycle Convergence and Execution Liveness](adr-031-controller-convergence-and-ha.md)
- [ADR-034: Spillable Exchange and Checkpoint Persistence Optimization](adr-034-spillable-exchange-and-checkpoint-persistence.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](adr-042-tenant-scoped-audited-security-event-queries.md)
- [ADR-045: Control-Plane Mutual TLS and Network Boundary](adr-045-control-plane-mtls-and-network-boundary.md)
