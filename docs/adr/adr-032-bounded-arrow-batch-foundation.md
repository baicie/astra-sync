# ADR-032: Bounded Arrow Batch Foundation

## Status

Accepted

## Context

Phase 5 must improve bulk throughput without weakening AstraSync's bounded-memory and explicit
backpressure invariants. The `arrow-format` module currently declares Apache Arrow dependencies but
has no batch, type, ownership, or wire contract. Adaptive batching, spill, and checkpoint tuning
cannot be measured safely until columnar memory and serialization have deterministic boundaries.

## Decision

Introduce an `ArrowBatch` that owns one child `BufferAllocator` and one `VectorSchemaRoot`. Every
factory receives a parent allocator and a positive per-batch allocation limit. Closing the batch
closes the Arrow resource and child allocator exactly once; it never closes the caller's parent.
The exposed root is borrowed and must not be retained, closed, or mutated by consumers.

Map the existing scalar `Row` values to signed Arrow integers, single/double precision floating
point, Decimal128, UTF-8, binary, day dates, nanosecond times, and nanosecond timestamps. Timestamp
values without a zone use UTC only as an arithmetic epoch and round-trip as `LocalDateTime`.
Timestamp values with a zone preserve the instant and decode in the schema timezone. Inferred
schemas require at least one non-null value per column; callers use an explicit Arrow schema for
all-null or empty batches. Mixed Java types, unsupported Arrow types, missing columns, nulls in
non-nullable fields, and Decimal128 overflow fail before a batch is returned.

Frame one Arrow IPC stream with an AstraSync magic, version, flags, and reserved header. The
end-of-input bit is outside the Arrow schema so a batch's terminal semantics survive IPC without
making schemas batch-specific. Decode applies both a payload-size limit and an off-heap allocator
limit.

Add JMH benchmarks for row scanning, Arrow vector scanning, row-to-Arrow conversion, and framed IPC
round trips. CI runs a short smoke profile to prove the harness remains executable, but does not use
shared-runner throughput as a performance threshold. Optimization claims require comparable runs
on pinned hardware with raw JMH output retained.

## Consequences

Phase 5 gains a leak-detectable and measurable columnar primitive without changing existing Row
connector or runtime behavior. Bulk connectors and exchanges can adopt it in later slices without
inventing ownership rules. The first conversion still copies Row values into Arrow vectors, and a
decoded IPC batch keeps its reader open until the batch is closed. Adaptive representation
selection, native columnar connectors, network shuffle, spill, and checkpoint batching remain out
of scope for this slice.
