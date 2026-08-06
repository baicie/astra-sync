# Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) for AstraSync.

## ADR Index

| Number | Title | Status | Date |
|--------|-------|--------|------|
| [ADR-001](adr-001-control-data-plane-separation.md) | Control Plane and Data Plane Separation | Accepted | 2026-08-02 |
| [ADR-002](adr-002-direct-pipeline-mode.md) | Direct Pipeline Mode as Default | Accepted | 2026-08-02 |
| [ADR-003](adr-003-source-enumerator-split-reader-model.md) | Source Enumerator/Split/Reader Model | Accepted | 2026-08-02 |
| [ADR-004](adr-004-sink-writer-committer-model.md) | Sink Writer/Committer Model | Accepted | 2026-08-02 |
| [ADR-005](adr-005-checkpoint-boundary.md) | Checkpoint as Consistency Boundary | Accepted | 2026-08-02 |
| [ADR-006](adr-006-epoch-fencing.md) | Epoch Fencing for Coordinator | Accepted | 2026-08-02 |
| [ADR-007](adr-007-storage-separation.md) | Storage Separation (PostgreSQL/etcd/Object Storage) | Accepted | 2026-08-02 |
| [ADR-008](adr-008-row-and-arrow-formats.md) | Dual Format Support (Row and Arrow) | Accepted | 2026-08-02 |
| [ADR-009](adr-009-capability-negotiation.md) | Exactly-Once via Capability Negotiation | Accepted | 2026-08-02 |
| [ADR-010](adr-010-single-active-region.md) | Single Active Region per Job | Accepted | 2026-08-02 |
| [ADR-011](adr-011-bounded-pull-single-node-runtime.md) | Bounded Pull-based Single-node Runtime | Accepted | 2026-08-02 |
| [ADR-012](adr-012-strict-versioned-job-spec.md) | Strict Versioned JobSpec Boundary | Accepted | 2026-08-02 |
| [ADR-013](adr-013-descriptor-first-connector-planning.md) | Descriptor-first Connector Planning | Accepted | 2026-08-02 |
| [ADR-014](adr-014-local-runner-and-cli-boundary.md) | Local Runner and CLI Boundary | Accepted | 2026-08-03 |
| [ADR-015](adr-015-strict-csv-and-create-new-output.md) | Strict CSV and Create-new Output | Accepted | 2026-08-03 |
| [ADR-016](adr-016-jdbc-connector-contract-and-type-mapping.md) | JDBC Connector Contract and Type Mapping | Accepted | 2026-08-03 |
| [ADR-017](adr-017-jdbc-transaction-boundaries.md) | JDBC Transaction Boundaries | Accepted | 2026-08-03 |
| [ADR-018](adr-018-cross-connector-scalar-encoding.md) | Cross-connector Scalar Encoding | Accepted | 2026-08-03 |
| [ADR-019](adr-019-cooperative-cancellation.md) | Cooperative Cancellation Boundary | Accepted | 2026-08-03 |
| [ADR-020](adr-020-cli-metrics-report.md) | CLI Metrics Report Contract | Accepted | 2026-08-03 |
| [ADR-021](adr-021-distributed-batch-runtime.md) | Distributed Batch Runtime Boundary | Accepted | 2026-08-03 |
| [ADR-022](adr-022-jdbc-range-splits.md) | Connector Split Enumeration and Numeric JDBC Ranges | Accepted | 2026-08-03 |
| [ADR-023](adr-023-worker-network-protocol.md) | Versioned Worker Protocol and Bounded Remote Admission | Accepted | 2026-08-03 |
| [ADR-024](adr-024-resumable-full-load.md) | Split-level Resumable Full-load Execution | Accepted | 2026-08-04 |
| [ADR-025](adr-025-distributed-jdbc-operational-slice.md) | Distributed JDBC Operational Slice | Accepted | 2026-08-04 |
| [ADR-026](adr-026-checkpoint-fencing-foundation.md) | Durable Checkpoint and Epoch Fencing Foundation | Accepted | 2026-08-04 |
| [ADR-027](adr-027-transactional-idempotent-sink-commit.md) | Transactional or Idempotent Sink Commit | Accepted | 2026-08-04 |
| [ADR-028](adr-028-native-cdc-and-checkpoint-coupled-offsets.md) | Native CDC and Checkpoint-coupled Offsets | Accepted | 2026-08-05 |
| [ADR-029](adr-029-durable-desired-state-job-lifecycle.md) | Durable Desired-state Job Lifecycle | Accepted | 2026-08-05 |
| [ADR-030](adr-030-lease-fenced-scheduler-dispatch.md) | Lease-fenced Scheduler Dispatch | Accepted | 2026-08-05 |
| [ADR-031](adr-031-controller-convergence-and-ha.md) | PostgreSQL Lifecycle Convergence and Execution Liveness | Accepted | 2026-08-05 |
| [ADR-032](adr-032-bounded-arrow-batch-foundation.md) | Bounded Arrow Batch Foundation | Accepted | 2026-08-06 |
| [ADR-033](adr-033-adaptive-batch-and-parallelism-control.md) | Adaptive Batch and Parallelism Control | Accepted | 2026-08-06 |

## Template

When adding a new ADR, use this template:

```markdown
# ADR-XXX: Title

## Status
Proposed | Accepted | Deprecated | Superseded

## Context
The issue that prompted this decision.

## Decision
The change that is being made and why.

## Consequences
What becomes easier or more difficult as a result of this change.
```

## Process

1. Create a new file named `adr-XXX-title.md`
2. Fill in the template sections
3. Update this index with the new ADR
4. Set status to "Proposed" initially
5. After review, update status to "Accepted"
