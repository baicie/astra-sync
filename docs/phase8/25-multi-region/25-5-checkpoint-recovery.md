# Phase 8 Slice 25.5: Checkpoint-coupled Recovery

## Status

Implementation Complete (Phase 8, 2026-08-19). Implements the
checkpoint-coupled recovery described in
ADR-049 §"Step 3: Checkpoint-Coupled Recovery".

## Context

ADR-049 records that after the secondary region's Coordinator receives
the new epoch, the Coordinator must replay the checkpoint manifest that
was most recently completed and replicated before the failover. The
Coordinator reads the checkpoint manifest from the WAL, downloads the
checkpoint files from object storage, and resumes the job from the
checkpoint.

ADR-048 §"Checkpoint replication" records that the WAL entry contains
the checkpoint manifest URI. The secondary region reads the WAL entries
that were replicated before the failover and uses the checkpoint manifest
to restore the job state.

Slice 25.1 implemented the WAL transport. Slice 25.2 implemented the
cross-region channel. Slice 25.5 implements the recovery logic that
uses the WAL entries to restore the job.

## Decision

### Recovery Flow

1. **Read WAL**: Secondary region's WAL consumer reads entries since the
   last confirmed checkpoint
2. **Find checkpoint**: Locate the most recent replicated checkpoint manifest
3. **Download checkpoint**: Download checkpoint files from object storage
4. **Restore state**: Restore job state from checkpoint files
5. **Resume job**: Resume the job from the restored state

### Recovery Failure Handling

| Failure | Behavior |
|---------|----------|
| Checkpoint manifest not found | Abort failover; operator must resolve manually |
| Checkpoint files not accessible | Abort failover; operator must resolve manually |
| State corruption | Abort failover; operator must resolve manually |

### Checkpoint Validation

Before restoring, the secondary region's Coordinator validates:
- CRC-32C checksum of each checkpoint file
- Checkpoint sequence matches WAL entry sequence
- Checkpoint epoch matches new epoch from promotion

## Implementation

### New Go Packages

```
control-plane/
  replication/
    recovery/
      recovery.go     # Recovery logic
      validator.go    # Checkpoint validation
```

### Verification

- **Functional**: Job restores from replicated checkpoint after failover
- **Validation**: Invalid checkpoint causes abort
- **Audit**: Recovery events recorded in audit log

## Consequences

### Positive

- Job state is preserved across failover
- Checkpoint validation ensures integrity
- Audit trail records recovery outcome

### Negative

- Recovery time depends on checkpoint size
- Network partition during recovery causes abort

## References

- [ADR-026: Durable Checkpoint and Epoch Fencing Foundation](../adr/adr-026-checkpoint-fencing-foundation.md)
- [ADR-034: Spillable Exchange and Checkpoint Persistence Optimization](../adr/adr-034-spillable-exchange-and-checkpoint-persistence.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [Slice 25.1: WAL Replication Topology](25-1-replication-topology.md)