# ADR-027: Transactional or Idempotent Sink Commit

## Status

Accepted as the design boundary for Phase 2 Slice 07

## Context

Slice 06 introduced durable source checkpoints and execution-epoch fencing, but a sink can commit a
batch just before the Coordinator fails to persist its checkpoint. Replaying from the prior source
position is correct for at-least-once delivery and can duplicate rows for a non-idempotent sink.

## Decision

1. Keep checkpoint records and the ordered progress acknowledgement as the Coordinator durability
   boundary.
2. Add an exactly-once sink SPI with idempotent and transactional specializations. The SPI receives
   a deterministic token and digest for each logical batch; the same token must be a retry-safe
   no-op, while a token/digest mismatch fails closed.
3. Generate the token from job, split, checkpoint sequence, and batch digest. Exclude execution epoch
   so recovery retries the same logical batch after a new epoch is acquired.
4. Extend the task contract and checkpoint protocol with the requested exactly-once mode. The Worker
   validates both the declared mode and the materialized sink implementation before opening
   resources.
5. Accept exactly-once during compilation only when the source has a replayable stable resume
   position and the sink advertises `TRANSACTIONAL_COMMIT` or `IDEMPOTENT_WRITE`.
6. Implement JDBC idempotency with a unique marker table written in the same transaction as the
   target batch. Do not infer target keys or advertise ordinary JDBC INSERT as exactly-once.
7. Leave unary at-most-once execution and checkpointed at-least-once execution behaviorally
   compatible.

## Consequences

Coordinator recovery can replay a committed batch without duplicating its rows when the sink
implements the contract. The sink pays for a marker lookup/write and retains marker state, and
operators must manage the marker table's lifecycle. Exactly-once still depends on the source
resume position being stable and on the target database honoring its transaction semantics. A
connector that only exposes a static capability but does not implement the SPI fails at Worker
materialization instead of silently weakening the requested guarantee.
