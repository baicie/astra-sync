# ADR-026: Durable Checkpoint and Epoch Fencing Foundation

## Status

Accepted as the design boundary for Phase 2 Slice 06

## Context

Phase 1 persists only completed split results. That is sufficient for split-level restart, but a
Worker failure after a sink batch commit causes the whole split to replay. Phase 1 also has no
execution identity that lets a Worker reject an old Coordinator after failover. The architecture
requires checkpoints as the consistency boundary and epochs as the stale-writer fence, but neither
contract exists in the running data plane.

## Decision

1. Add an opt-in checkpoint contract alongside the Phase 1 source and sink interfaces. A source must
   expose a canonical resume position; a sink must expose its committed batch token and validate the
   active epoch.
2. Persist one ordered checkpoint stream per job and split with atomic replacement and exclusive
   per-job locking. Records include the job, epoch, split fingerprint, sequence, source position,
   sink token, and batch digest.
3. Allocate a monotonically increasing execution epoch for every Coordinator invocation. Workers
   reject lower epochs before task materialization and before sink commit.
4. Add a bounded progress stream to the versioned Worker protocol. The Coordinator durably records a
   `BatchCommitted` event and sends `CheckpointAck` before the Worker advances the source.
5. Keep Phase 1 unary execution and at-most-once jobs unchanged. Exactly-once remains rejected until
   a later slice supplies a transactional or idempotent sink commit protocol.
6. For the first JDBC implementation, require an explicit stable unique resume column for
   checkpointed recovery. Generic JDBC range jobs continue to use the Phase 1 boundary.

## Consequences

Checkpointed jobs can restart from the last acknowledged batch and stale Coordinators can no longer
advance a Worker after a newer epoch is active. The progress stream adds protocol and storage
overhead and initially serializes one batch barrier per task. A crash between the sink commit and
checkpoint durability still replays that batch, so the result is at-least-once rather than
exactly-once. Transactional or idempotent commit support remains a separate Phase 2 slice.
