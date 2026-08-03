# Slice 01: Single-node Kernel

## Status

Verified and complete on implementation commit `e4ed490`.

## Objective

Provide the smallest trustworthy data-plane kernel: one bounded batch is pulled, transformed,
fully written, and released before another batch is requested. The slice also restores a
reproducible build for the modules needed by the kernel.

## Included

- Immutable null-safe `SyncRecord` values.
- `SyncBatch` with explicit end-of-input and maximum-size enforcement.
- Source, transform, and Sink lifecycle contracts.
- A synchronous linear `Source -> Transform* -> Sink` execution loop.
- Configurable positive batch limit and no prefetch queue.
- Success counters and failure-stage/partial-counter reporting.
- Deterministic resource closing and suppressed close failures.
- Unit tests for bounds, ordering, lifecycle, failure, and immutability.
- Minimal repairs needed to compile the initial API sketches; later public SPI design remains
  owned by slice 02.

## Excluded

- JobSpec parsing, connector registry, file/JDBC connectors, CLI, and distributed execution.
- Checkpoint, retries, prefetch, asynchronous transforms, or exactly-once semantics.

## Acceptance

1. More records than the configured batch size are processed across multiple source pulls.
2. A returned batch larger than the requested limit fails at `SOURCE_READ` before any record in
   that batch reaches the Sink.
3. The Source is not polled again until the Sink has completed the current batch.
4. Source, transform, and Sink failures report their exact stage and partial result.
5. Every successfully opened resource closes once; close errors never replace a primary error.
6. Records are immutable and retain database null values.
7. The documented Maven test command passes.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-011](../../adr/adr-011-bounded-pull-single-node-runtime.md)
