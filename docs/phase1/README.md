# Phase 1: Distributed Batch Sync

Phase 1 extends the verified Phase 0 single-process path toward distributed full-load execution.
The first slice establishes the Coordinator/Worker boundary with a bounded direct exchange. It
is deliberately transport-neutral and does not claim production distributed execution yet.

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 01 | Coordinator task scheduling, in-process Workers, bounded direct exchange, failure propagation | Complete |
| 02 | Connector split enumeration and source-position-aware full-load tasks | Complete |
| 03 | Network Worker protocol and distributed backpressure | Planned |
| 04 | Resumable full-load execution and operational deployment | Planned |

## Slice 01 Boundary

The Coordinator accepts a `BatchSplitEnumerator`, asks a `BatchTaskFactory` to materialize one
resource-owned `BatchTask` per immutable `SourceSplit`, and schedules the tasks across a fixed
Worker set. A Worker runs each task with a Source thread, a Sink thread, and a bounded
`BatchExchange`. The exchange capacity is explicit and every task has its own connector resources.

Slice 02 provides a connector-neutral split contract and a JDBC numeric range implementation. JDBC
enumeration uses `MIN`/`MAX` over a required non-null integer split column, produces left-closed and
right-open ranges, and gives the final split an unbounded upper edge. A split reader is created only
after enumeration and is rejected if its source identity or boundaries do not match the contract.

The slice supports parallel independent tasks and reports aggregate `SyncResult` metrics. A task
failure stops outstanding tasks and preserves the task's partial metrics in the exception.

## Excluded

- gRPC or other network transport
- dynamic split discovery beyond the static JDBC range enumerator
- retries, checkpoints, replay, savepoints, epoch fencing, and exactly-once
- shared transactional sink commits
- autoscaling, durable scheduling state, and Kubernetes reconciliation

## Acceptance

- The engine reactor builds the runtime, Coordinator, and Worker modules.
- A Coordinator schedules multiple split tasks across available Workers and aggregates metrics.
- A Worker never holds more than its configured exchange capacity of unpublished batches.
- Source and Sink run concurrently, observe terminal batches, and close resources on success or
  failure.
- A Source or Sink failure releases the opposite side and returns a structured task failure.
- Tasks assigned to the same Worker are serialized; independent Workers can execute in parallel.
- A connector enumerates stable unique split descriptors; the Coordinator materializes one task per
  split and rejects duplicate split IDs or task factory substitutions.
- The JDBC range connector returns no splits for an empty table, never creates more splits than the
  integer value range, and rejects non-integral split columns.
- Phase 0 behavior and its at-most-once boundary remain unchanged.

## Records

- [ADR-021: Distributed Batch Runtime Boundary](../adr/adr-021-distributed-batch-runtime.md)
- [ADR-022: Connector Split Enumeration and Numeric JDBC Ranges](../adr/adr-022-jdbc-range-splits.md)
- [Slice 02 design](02-connector-split-enumeration/design.md)
- [Slice 02 verification](02-connector-split-enumeration/verification.md)
- [Architecture delivery phases](../architecture.md)
