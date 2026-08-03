# ADR-011: Bounded Pull-based Single-node Runtime

## Status

Accepted

## Date

2026-08-02

## Context

Phase 0 needs a single-process runtime before distributed exchange, checkpointing, and worker
scheduling exist. A prototype Source returned the complete input as a list. That makes memory
proportional to the data set and lets Source production run independently of Sink capacity,
violating AstraSync's bounded-memory and explicit-backpressure invariant.

Introducing an asynchronous queue in the first slice would require cancellation, executor,
queue ownership, and cross-thread failure semantics before they add product value.

## Decision

The Phase 0 single-node kernel uses synchronous bounded pull execution:

1. The runtime requests at most a configured number of records from the Source.
2. The Source returns one explicit batch and an end-of-input signal.
3. The runtime completely transforms and writes that batch before requesting another.
4. The runtime has no prefetch thread or data queue.
5. A Source batch that exceeds the requested limit is a contract failure.
6. Source and Sink have explicit lifecycle ownership; failures carry stage and partial metrics.

The default direct-pipeline decision in ADR-002 still applies. This ADR only chooses the local
execution behavior used before Phase 1 adds bounded network exchange.

## Consequences

### Positive

- Memory is bounded by connector batch configuration rather than total input size.
- Slow Sink behavior naturally propagates to Source polling.
- Ordering, cancellation points, resource ownership, and failures are deterministic.
- Connector and runtime contract violations are easy to test.
- The model provides a clear reference for future distributed backpressure.

### Negative

- Source, transforms, and Sink cannot overlap work in this slice.
- A blocking Sink occupies the execution thread.
- Throughput is lower than a carefully bounded asynchronous pipeline.
- Distributed credit accounting is still required in Phase 1.

## Alternatives Considered

### Materialize the Complete Source

Rejected because input size directly controls heap usage and there is no backpressure.

### Bounded Producer/Consumer Queue

Deferred. It can overlap I/O but introduces cross-thread cancellation and failure complexity. It
may be added later only with an explicit queue bound and updated decision record.

### Reactive Streams

Deferred. It supplies a formal demand protocol but would add a framework-level contract before
the connector and JobSpec boundaries have stabilized.

## Related Decisions

- ADR-002: Direct Pipeline Mode as Default
- ADR-003: Source Enumerator/Split/Reader Model
- ADR-004: Sink Writer/Committer Model
- ADR-005: Checkpoint as Consistency Boundary
- ADR-009: Exactly-once via Capability Negotiation
