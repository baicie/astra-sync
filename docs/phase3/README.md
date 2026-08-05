# Phase 3: Native CDC

Phase 3 adds a connector-neutral CDC contract and native MySQL and PostgreSQL sources backed by
Debezium. It connects snapshot capture, streaming changes, checkpoint recovery, and an idempotent
JDBC sink into one local exactly-once execution path.

**Status: Complete**

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 08 | CDC SPI, Debezium offset/runtime adapter, and native MySQL/PostgreSQL connectors | Complete |
| 09 | Checkpoint-coupled CDC worker/coordinator and idempotent JDBC CDC sink | Complete |
| 10 | ServiceLoader discovery, CLI `cdc` command, tests, and operational documentation | Complete |

## Delivered Boundary

`CdcSource` exposes ordered `CdcBatch` values and acknowledges a batch only after the sink has
committed it. `DataEvent` carries immutable before/after rows, a structured key, an opaque source
position, operation, transaction metadata, and trace context. Debezium's native snapshot and
streaming phases are preserved, including the snapshot-to-streaming handoff.

The MySQL connector uses row-based binlog capture and requires an explicit schema-history file.
The PostgreSQL connector uses `pgoutput`, an explicit logical replication slot, and an explicit
publication. Both connectors validate their options and protect Debezium properties that would
break AstraSync offset ownership.

The checkpointed worker commits the CDC sink first, acknowledges the source second, and persists
the checkpoint third. The JDBC CDC sink applies insert, update, and delete events and records a
stable commit token and batch digest in the same target transaction, making retries idempotent.

The CLI discovers connectors with Java `ServiceLoader` and runs the local checkpointed CDC runner.
Remote Worker CDC transport, multi-task CDC partitioning, schema migration policy, and non-JDBC CDC
sinks remain outside this phase.

## Usage

Use `java -jar astrasync-cli-*-all.jar cdc <job-spec>` with a CDC JobSpec. The command accepts
`--checkpoint-dir`, `--poll-timeout-ms`, and `--max-checkpoints`. A MySQL source must include
`hostname`, `username`, `password`, `database`, `tables`, `topicPrefix`, `serverId`, and
`schemaHistoryFile`. A PostgreSQL source must include `hostname`, `username`, `password`,
`database`, `tables`, `topicPrefix`, `slotName`, and `publicationName`.

The CDC sink uses the `jdbc` connector with `url`, `table`, and comma-separated `keyColumns`; its
optional `commitTokenTable` stores idempotency markers.

## Records

- [Design](01-native-cdc/design.md)
- [Implementation plan](01-native-cdc/implementation-plan.md)
- [Verification](01-native-cdc/verification.md)
- [ADR-028: Native CDC and Checkpoint-coupled Offsets](../adr/adr-028-native-cdc-and-checkpoint-coupled-offsets.md)
