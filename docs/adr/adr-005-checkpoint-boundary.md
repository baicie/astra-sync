# ADR-005: Checkpoint as Consistency Boundary

## Status

Accepted

## Context

Ensuring exactly-once or at-least-once semantics requires a consistent snapshot of:
- Source offsets (where to resume reading)
- Operator state (join buffers, aggregations, etc.)
- Pending sink commits (in-flight writes)

These three components must be saved atomically to guarantee consistency.

## Decision

Checkpoint is the sole consistency boundary:

```
1. Coordinator initiates checkpoint N
2. Source injects CheckpointBarrier
3. All operators save state upon receiving all barriers
4. Source saves offset for checkpoint N
5. Sink prepares commit for checkpoint N
6. Coordinator writes CompletedCheckpoint manifest
7. Coordinator triggers sink commit
8. Old checkpoints cleaned up
```

### Checkpoint Types

| Type | Description | Use Case |
|------|-------------|----------|
| Aligned | Block on barrier alignment | Normal operation |
| Unaligned | Save in-flight data | Backpressure |
| Incremental | Only changed state | Large state |

## Consequences

### Positive
- Single source of truth for recovery
- Enables both exactly-once and at-least-once
- Automatic cleanup of old checkpoints
- Supports savepoints for planned maintenance

### Negative
- Checkpoint coordination adds overhead
- Large checkpoints slow recovery
- Aligned checkpoints can block under backpressure

## References

- [Flink Checkpointing](https://nightlies.apache.org/flink/flink-docs-stable/docs/concepts/stateful-stream-processing/)
- [SeaTunnel Checkpoint Mechanism](https://seatunnel.apache.org/docs/architecture/fault-tolerance/checkpoint-mechanism/)
