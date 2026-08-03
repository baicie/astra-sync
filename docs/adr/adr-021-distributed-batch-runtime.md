# ADR-021: Distributed Batch Runtime Boundary

## Status

Accepted

## Context

Phase 0 proves a bounded Source-to-Sink path inside one Java process. Phase 1 needs to move
that path behind Coordinator and Worker boundaries without making network transport, checkpoint
storage, or stronger delivery guarantees prerequisites for the first distributed slice.

The existing connector SPI exposes bounded `BatchSource` and `BatchSink` resources. It does not
yet expose split enumeration or a remote protocol, so the first Phase 1 slice must keep those
concerns explicit and testable.

## Decision

1. A `BatchSplitEnumerator` produces immutable `SourceSplit` descriptors. A `BatchTaskFactory`
   materializes one `BatchTask` for each descriptor; a task owns the descriptor, one source, one
   sink, a batch limit, and an exchange capacity.
2. A Coordinator assigns tasks to registered Workers. A Worker is single-task-at-a-time from
   the Coordinator's perspective; tasks assigned to the same Worker are serialized.
3. Each Worker runs Source and Sink execution concurrently and connects them through a bounded
   in-process `BatchExchange`. `RowBatch` is the transport unit and `endOfInput` is the terminal
   marker.
4. Exchange publication and consumption are interruptible and observe failure state, so a failed
   side releases the other side without an unbounded queue or executor backlog.
5. The first slice uses in-process implementations behind interfaces. A network transport can
   replace the exchange and worker implementation later without changing the Coordinator task
   contract.
6. Delivery remains at-most-once. There is no retry, checkpoint, replay, transaction coordinator,
   epoch fencing, or exactly-once claim in this slice.

## Consequences

The execution boundary is now ready for real split-aware connectors and remote Worker transport,
while tests can prove scheduling, bounded exchange, failure propagation, and resource ownership
without external services. Source and sink instances must be created per task; sharing a connector
resource across concurrent tasks is outside this contract. Resumability and exactly-once remain
later phases because they require a durable consistency boundary.
