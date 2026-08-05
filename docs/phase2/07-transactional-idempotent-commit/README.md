# Slice 07: Transactional or Idempotent Sink Commit

Slice 07 closes the replay window between a sink commit and durable checkpoint acknowledgement for
connectors that implement the exactly-once sink SPI. The JDBC connector uses a companion marker
table and commits the marker with the target rows in one JDBC transaction.

## Enable Exactly-Once

The source must retain the stable unique resume column required by Slice 06. Set the delivery
guarantee to `exactly-once`:

```yaml
spec:
  source:
    connector: jdbc
    options:
      url: jdbc:postgresql://source.example/orders
      table: orders
      splitColumn: id
      resumeColumn: id
      splitCount: "4"
  sink:
    connector: jdbc
    options:
      url: jdbc:postgresql://target.example/orders
      table: orders_copy
      # Optional; defaults to orders_copy__astrasync_commit_tokens.
      commitTokenTable: orders_copy_commit_tokens
  delivery:
    guarantee: exactly-once
```

The marker table has a unique `commit_token` and a `batch_digest`. Operators should grant the Worker
database user permission to create and write this table, or provision a compatible table before the
first run. The marker table is sink-side state and must remain available for the lifetime of the
job's replay horizon.

Exactly-once is rejected before connector resources open when the source is not replayable, the
resume column is missing, or the sink descriptor lacks transactional/idempotent capability. The
Worker also rejects a task whose materialized sink does not implement the runtime SPI.

At-most-once and at-least-once jobs do not create marker tables and retain their existing execution
paths.

See [design.md](design.md), [implementation-plan.md](implementation-plan.md),
[verification.md](verification.md), and [ADR-027](../../adr/adr-027-transactional-idempotent-sink-commit.md).
