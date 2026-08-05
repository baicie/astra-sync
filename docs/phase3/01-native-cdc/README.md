# Native CDC Usage

This slice is the operational entry point for Phase 3 CDC. The full design and verification
records are linked below.

## Job Shape

```yaml
apiVersion: sync.astrasync.io/v1
kind: SyncJob
metadata:
  name: orders-cdc
spec:
  source:
    connector: mysql-cdc
    options:
      hostname: mysql
      port: "3306"
      username: sync_reader
      password: change-me
      database: shop
      tables: shop.orders
      topicPrefix: shop-source
      serverId: "5401"
      schemaHistoryFile: ./state/mysql-history.dat
  sink:
    connector: jdbc
    options:
      url: jdbc:postgresql://target:5432/shop
      user: sync_writer
      password: change-me
      table: orders
      keyColumns: id
  delivery:
    guarantee: exactly-once
  runtime:
    maxBatchRecords: 2048
```

Use `postgres-cdc` instead of `mysql-cdc` for PostgreSQL and provide `schemas`, `slotName`, and
`publicationName`. The JDBC destination table must already have compatible columns and the listed
key columns must identify one row uniquely.

## Runtime Rules

- The source snapshot is written through the same sink transaction as streaming changes.
- The source offset is acknowledged only after the sink commit returns successfully.
- The checkpoint is durable only after the source acknowledgement and coordinator progress write.
- A retry with the same commit token and digest is a no-op; a token reused with a different digest is
  rejected.
- `snapshotMode: never` starts from the supplied CDC position and requires a compatible durable
  checkpoint when recovering.

This phase provides a local, single CDC task. The current remote Worker protocol remains batch-only.

See [design.md](design.md), [implementation-plan.md](implementation-plan.md), and
[verification.md](verification.md).
