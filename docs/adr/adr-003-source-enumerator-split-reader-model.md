# ADR-003: Source Enumerator/Split/Reader Model

## Status

Accepted

## Context

Data sources need to be read in parallel by multiple workers for throughput scaling. The system must support:
- Dynamic split discovery and distribution
- Fault tolerance with split reassignment
- Adaptive splitting based on data distribution
- Exactly-once semantics with offset tracking

## Decision

Adopt the SeaTunnel-style Enumerator/Split/Reader model:

```
SplitEnumerator
    ├── Tracks all splits
    ├── Distributes splits to readers
    ├── Handles split reassignment on failure
    └── Snapshots state for checkpointing

SourceSplit
    ├── SplitId, TableId
    ├── Start/End positions
    ├── IsSnapshot flag
    └── Parallelism hint

SourceReader
    ├── Polls data from assigned splits
    ├── Tracks current position
    └── Reports progress for adaptive splitting
```

### Split Types

| Split Type | Description | Use Case |
|------------|-------------|----------|
| Snapshot Split | Full table data chunk | Initial load, batch read |
| CDC Split | Incremental changes | Change data capture |
| Hybrid Split | Snapshot + CDC transition | Full + incremental sync |

## Consequences

### Positive
- Supports parallel reading and dynamic load balancing
- Enables fault tolerance with split reassignment
- Allows adaptive splitting for skewed data
- Compatible with Debezium-style CDC connectors

### Negative
- More complex connector implementation
- Requires careful offset management
- Split coordination adds overhead

## References

- [SeaTunnel Source Architecture](https://seatunnel.apache.org/docs/architecture/api-design/source-architecture/)
- [Debezium Incremental Snapshot](https://debezium.io/documentation/reference/stable/configuration/signalling.html)
