# Phase 2: Checkpoint and Exactly-Once Foundations

Phase 2 adds consistency state to the distributed batch runtime. Slice 06 delivers the durable
checkpoint and stale-execution fencing foundation. Exactly-once remains a later capability.

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 06 | Durable batch checkpoints, remote progress acknowledgement, and epoch fencing | Complete |
| 07 | Transactional or idempotent sink commit and exactly-once capability negotiation | Planned |

## Boundary

Slice 06 establishes a durable recovery position for each successfully committed batch and fences
stale Coordinator executions. It provides at-least-once recovery when the source exposes a stable
resume position. It does not claim exactly-once because a process can still fail after a sink commit
and before the checkpoint record is durable.

Slice 07 will add a sink commit protocol that closes that replay window for connectors whose
capabilities support it. A requested exactly-once guarantee remains rejected until that slice is
complete.

## Records

- [ADR-026: Durable Checkpoint and Epoch Fencing Foundation](../adr/adr-026-checkpoint-fencing-foundation.md)
- [Slice 06 usage and delivery](06-checkpoint-fencing-foundation/README.md)
- [Slice 06 design](06-checkpoint-fencing-foundation/design.md)
- [Slice 06 implementation plan](06-checkpoint-fencing-foundation/implementation-plan.md)
- [Slice 06 verification](06-checkpoint-fencing-foundation/verification.md)
