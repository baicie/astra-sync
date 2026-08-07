# Spillable Exchange and Checkpoint Persistence Optimization

Phase 5 Slice 16 adds an opt-in disk-backed exchange for slow-sink bursts and reduces repeated
checkpoint manifest work without changing fixed-mode behavior, checkpoint ordering, or epoch
fencing.

## Data Flow

```text
source -> bounded slots -> memory RowBatch or versioned spill frame -> sink
                                      |
                              delete after receive

checkpoint event -> bounded state cache -> forced temporary file -> atomic manifest replacement
```

Spill is local to the Worker process. The Coordinator sends a validated policy signature, while
the Worker supplies its own writable spill root. Every queue slot and every spill byte remains
bounded; failure deletes owned temporary files and propagates to the producer and consumer.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-034: Spillable Exchange and Checkpoint Persistence Optimization](../../adr/adr-034-spillable-exchange-and-checkpoint-persistence.md)
