# Phase 1: Distributed Batch Sync

Phase 1 extends the verified Phase 0 single-process path toward distributed full-load execution.
The first slice establishes the Coordinator/Worker boundary with a bounded direct exchange. It
is deliberately transport-neutral and does not claim production distributed execution yet.

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 01 | Coordinator task scheduling, in-process Workers, bounded direct exchange, failure propagation | Complete |
| 02 | Connector split enumeration and source-position-aware full-load tasks | Complete |
| 03 | Network Worker protocol and distributed task admission backpressure | Complete |
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

Slice 03 adds a versioned protobuf Worker protocol over length-prefixed TCP frames. A remote Worker
materializes each split through a server-side `BatchTaskFactory`, executes it with the existing Worker
contract, and returns task metrics or structured failure. The server has a bounded task executor and
the client has a bounded in-flight window; a full remote Worker rejects work instead of growing an
unbounded queue. Cancellation is an explicit interrupt request.

The slice supports parallel independent tasks and reports aggregate `SyncResult` metrics. A task
failure stops outstanding tasks and preserves the task's partial metrics in the exception.

## Excluded

- gRPC, TLS/mTLS, authentication, service discovery, and durable Worker registration
- network RowBatch streaming between separate Source and Sink processes
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
- A remote Worker validates protocol version and task identity, materializes resources on the Worker,
  returns metrics/failures, rejects work at bounded capacity, and responds to cancellation.
- Phase 0 behavior and its at-most-once boundary remain unchanged.

## Records

- [ADR-021: Distributed Batch Runtime Boundary](../adr/adr-021-distributed-batch-runtime.md)
- [ADR-022: Connector Split Enumeration and Numeric JDBC Ranges](../adr/adr-022-jdbc-range-splits.md)
- [ADR-023: Versioned Worker Protocol and Bounded Remote Admission](../adr/adr-023-worker-network-protocol.md)
- [Slice 02 design](02-connector-split-enumeration/design.md)
- [Slice 02 verification](02-connector-split-enumeration/verification.md)
- [Slice 03 design](03-network-worker-protocol/design.md)
- [Slice 03 verification](03-network-worker-protocol/verification.md)
- [Architecture delivery phases](../architecture.md)
