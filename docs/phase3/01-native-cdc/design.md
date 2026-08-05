# Phase 3 Slice 08-10 Design

## Goals

1. Capture MySQL binlog and PostgreSQL logical-replication changes with native Debezium connectors.
2. Provide one immutable event and opaque resume-position contract to the AstraSync runtime.
3. Preserve snapshot-to-streaming handoff and transaction metadata.
4. Couple sink commit, source offset acknowledgement, and durable checkpoint progress.
5. Make CDC retries idempotent for the JDBC sink.
6. Keep the existing Phase 1 batch path and Phase 2 checkpoint contracts compatible.

## Non-goals

- Remote CDC task transport or multi-worker CDC partition scheduling.
- XA transactions across databases.
- Automatic schema migration or a universal CDC sink implementation.
- Oracle, SQL Server, MongoDB, or other database log formats.

## Contract

```text
CdcSource.openAt(SplitPosition)
CdcSource.poll(timeout) -> CdcBatch?
CdcSource.acknowledge(CdcBatch) -> SplitPosition

CdcSink.open(CheckpointContext)
CdcSink.writeBatch(CdcBatch, SinkCommitContext)
CdcSink.lastCommitToken() -> String
```

`CdcBatch` is the source checkpoint unit. A source keeps the batch outstanding until
`acknowledge` is called. `SourcePosition` contains connector identity, database/table identity,
ordered native offsets, event timestamp, transaction id, and transaction order. The runtime treats
the position as opaque.

`DataEvent` contains an operation (`SNAPSHOT`, `INSERT`, `UPDATE`, or `DELETE`), structured key,
before/after rows, schema/table identity, event and ingest timestamps, headers, and trace context.
Rows and maps are defensively copied and immutable.

## Offset Ownership

Debezium's embedded engine writes Kafka Connect offset maps through `AstraOffsetBackingStore`.
The store is registered per connector instance and encodes its state into `SplitPosition` using a
versioned, deterministic Base64 map. An unbounded position means a new source with no saved offset.
The Debezium engine receives a zero flush interval and an always commit policy, but its records are
marked processed only after AstraSync has acknowledged the sink commit. This makes the next
checkpoint position the source's durable recovery boundary.

## Execution Ordering

```text
source.poll
  -> sink.writeBatch (transaction + commit marker)
  -> source.acknowledge (Debezium offset commit)
  -> checkpoint store append
```

The worker verifies that the sink returned the expected Phase 2 commit token and batch digest.
Failure before the sink commit leaves the source batch unacknowledged. Failure after sink commit
but before checkpoint durability can replay the batch; the JDBC marker makes that replay harmless.
The coordinator increments execution epoch and checkpoint sequence using the Phase 2 fencing
contract.

## Connector Configuration

MySQL configures `database.server.id`, row-based binlog capture, table filters, minimal snapshot
locking, transaction metadata, and file-backed schema history. PostgreSQL configures `pgoutput`,
table/schema filters, logical replication slot, filtered publication, transaction metadata, and
non-dropping slot behavior. Both connectors allow namespaced advanced Debezium options while
rejecting protected ownership and connection properties.

## Capability Negotiation

CDC planning requires `CHANGE_DATA_CAPTURE`, `STREAM_READ`, and `REPLAYABLE_OFFSET` on the source;
exactly-once additionally requires `EXACTLY_ONCE_SOURCE`. The sink must declare `BATCH_WRITE`,
`UPSERT`, and `DELETE`; exactly-once also requires `TRANSACTIONAL_COMMIT` or `IDEMPOTENT_WRITE`.
The descriptor validates role/capability consistency before a job can be compiled.

## Lifecycle and Discovery

Connector factories are discovered with `ServiceLoader`. The CLI's `cdc` command compiles the job,
materializes the source and sink, runs one local checkpointed task, and persists progress in the
configured checkpoint directory. A shutdown request stops polling after the current checkpoint
boundary and closes both connector resources.
