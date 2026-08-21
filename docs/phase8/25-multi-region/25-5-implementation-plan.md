# Phase 8 Slice 25.5: Implementation Plan

## Status

Implementation Complete (Phase 8, 2026-08-19). Matches the design decisions
in [`25-5-checkpoint-recovery.md`](25-5-checkpoint-recovery.md). See
[`docs/phase8/closeout.md`](../closeout.md) and the Phase 9 evidence in
[`docs/phase9/closeout.md`](../../phase9/closeout.md).

## Scope

This document is the implementation plan for Slice 25.5. It records
the decisions, dependencies, and verification path for checkpoint-coupled
recovery.

## Decisions Made

### 1. Recovery Trigger

**Decision**: Triggered by promotion manager after capability revalidation

The recovery is triggered automatically after the sink capability is
confirmed and the failover is complete.

### 2. Checkpoint Selection

**Decision**: Most recent replicated checkpoint

The Coordinator selects the most recent checkpoint that has been fully
replicated to the secondary region via the WAL.

### 3. Validation

**Decision**: Full validation before restoration

The checkpoint is validated for CRC, sequence, and epoch before
restoration proceeds.

## Dependencies

| Dependency | ADR | Status | Notes |
|------------|-----|--------|-------|
| Slice 25.1 | ADR-048 | Complete | WAL transport and topology loader |
| Slice 25.2 | ADR-048 | Complete | Cross-region gRPC channel |
| Slice 25.3 | ADR-049 | Complete | Promotion manager |
| Slice 25.4 | ADR-049 | Complete | Capability revalidation |
| Checkpoint persistence | ADR-034 | Complete | Checkpoint manifest and files |

## Implementation Tasks

### Phase 1: Recovery Logic

- [ ] Implement RecoveryManager with checkpoint selection
- [ ] Implement WAL entry reader for checkpoint discovery
- [ ] Add checkpoint manifest parser
- [ ] Add checkpoint file downloader

### Phase 2: Validation

- [ ] Implement checkpoint validator (CRC, sequence, epoch)
- [ ] Add validation failure handling
- [ ] Add audit events for validation

### Phase 3: Integration

- [ ] Wire recovery to promotion completion
- [ ] Add recovery metrics
- [ ] End-to-end test with failover

## Out-of-Scope

- Checkpoint compaction (handled by object storage lifecycle)
- Incremental checkpoint recovery (future enhancement)

## Verification

### Functional Verification

1. **Recovery succeeds**: Job restores from replicated checkpoint
2. **Checkpoint not found**: Recovery fails with audit event
3. **Validation failure**: Recovery fails with audit event

### Non-Functional Verification

1. **Recovery time**: < 5 minutes for 100GB checkpoint
2. **Metrics**: Recovery duration and outcome recorded

## Open Questions

None. All decisions are made in the design document.