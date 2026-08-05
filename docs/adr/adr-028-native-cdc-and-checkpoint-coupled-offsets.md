# ADR-028: Native CDC and Checkpoint-coupled Offsets

## Status

Accepted

## Context

AstraSync needs database change capture without creating a second database-log implementation for
each source. Database snapshots, MySQL binlog details, PostgreSQL logical replication, transaction
metadata, and schema history are already handled by Debezium. The runtime still needs a connector-
neutral event contract and must not advance a source position before the sink commit is durable.

## Decision

Use Debezium Embedded as the source implementation for MySQL and PostgreSQL CDC. Convert Debezium
records into immutable `DataEvent` values and group them into ordered `CdcBatch` values. Expose
connector-owned `SplitPosition` values as the resume contract.

The embedded source uses an AstraSync offset backing store. Debezium's Kafka Connect offset map is
encoded into the opaque `SplitPosition`; the source marks records processed only after the CDC sink
has committed the batch. Snapshot events are labeled separately from streaming events, and a batch
containing both phases is labeled as the snapshot/streaming handoff.

The compiler accepts CDC only in the checkpoint runtime. It requires replayable offsets, an
exactly-once-capable source, and a sink with both upsert and delete capabilities. The local CDC
runner uses the JDBC CDC sink, whose data changes and idempotent commit marker are committed in one
database transaction using the Phase 2 logical commit token.

MySQL persists Debezium schema history at an explicit configured file path. PostgreSQL requires
explicit logical replication slot and publication names. Connector-specific properties are
allow-listed and protected Debezium properties cannot be overridden by a job.

## Consequences

- MySQL and PostgreSQL share one source, offset, event, and runtime contract.
- Recovery can resume from a durable source position without inventing database-specific offsets in
  the Coordinator.
- A sink failure cannot acknowledge the source batch, so recovery replays the uncommitted batch.
- Native CDC is currently available through the in-process checkpointed runner; the remote Worker
  protocol is still a batch-only boundary.
- Database integration tests require Docker and are skipped when no Docker daemon is available;
  unit and recovery tests remain self-contained.
