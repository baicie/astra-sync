# ADR-004: Sink Writer/Committer Model

## Status

Accepted

## Context

Sink connectors need to support various commit strategies depending on target system capabilities:
- Transactional systems (Kafka, Iceberg) need two-phase commit
- File-based systems need rename or manifest commit
- JDBC systems need batch commit with optional idempotency
- Some systems only support idempotent writes

## Decision

Separate data writing from global commitment:

```
SinkWriter
    ├── Writes records to target
    ├── Buffers data locally
    └── Returns CommitHandle on prepareCommit

GlobalCommitter
    ├── Aggregates CommitHandles from all writers
    ├── Performs global commit coordination
    └── Handles recovery from partial commits
```

### Commit Strategies by Target

| Target | Strategy | Exactly-Once |
|--------|----------|--------------|
| Kafka | Transaction ID | Yes |
| Iceberg | Manifest Commit | Yes |
| JDBC | Batch + Idempotency Key | Best Effort |
| File | Rename/Manifest | Yes |
| HTTP API | Idempotency Key | Best Effort |

## Consequences

### Positive
- Enables true end-to-end exactly-once with transactional targets
- Supports mixed targets with different capabilities
- Clear separation of write and commit concerns
- Better fault tolerance with partial failure recovery

### Negative
- Two-phase commit adds latency
- Committer becomes a bottleneck for parallel sinks
- Requires careful handling of commit failures

## References

- [SeaTunnel Sink Architecture](https://seatunnel.apache.org/docs/architecture/api-design/sink-architecture/)
- [Flink Two-Phase Commit](https://flink.apache.org/features/2018/03/13/end-to-end-exactly-once.html)
