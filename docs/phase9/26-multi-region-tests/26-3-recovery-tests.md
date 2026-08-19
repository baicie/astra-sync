# Phase 9 Slice 26.3: Recovery Integration Tests

## Status

Proposed. Implements recovery integration tests for multi-region topology.

## Context

Phase 8 implemented checkpoint-coupled recovery. Phase 9 validates
through integration tests.

The recovery integration tests verify:

1. Checkpoint is located in secondary region
2. Checkpoint is validated
3. State is restored from checkpoint
4. Job resumes from restored state

## Decision

### Test Cases

| Test | Description |
|------|-------------|
| `TestRecovery_HappyPath` | Recovery succeeds, checkpoint restored |
| `TestRecovery_CheckpointNotFound` | Failover aborts if checkpoint not found |
| `TestRecovery_EpochMismatch` | Failover aborts on epoch mismatch |
| `TestRecovery_ValidationFailure` | Failover aborts on validation failure |

## Implementation

### New File

```
tests/integration/multi-region/recovery/
  recovery_test.go
```

## References

- [Phase 9 Slice 26.1](26-1-e2e-framework.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)