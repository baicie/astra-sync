# Arrow Batch Foundation

Phase 5 Slice 14 establishes the columnar batch and measurement contracts used by later runtime
optimizations.

## Data Flow

```text
RowBatch -- schema inference/validation --> bounded ArrowBatch
   ^                                           |
   |                                           v
   +---------------- row decode <-- framed Arrow IPC
```

`ArrowBatch` exclusively owns a child allocator and Arrow root under a caller-supplied memory
limit. `close()` releases both while leaving the parent allocator open. The Arrow root returned to
callers is a borrowed, read-only view valid only until the batch closes.

`ArrowBatchCodec` infers schemas for non-empty scalar rows or accepts an explicit Arrow schema for
all-null and terminal-empty batches. It preserves column order, nullability, Decimal128 scale, and
nanosecond temporal precision. `ArrowIpcCodec` wraps one Arrow stream in a small versioned frame so
end-of-input state survives serialization and malformed or oversized payloads fail deterministically.

The benchmark module compares row and Arrow scans and measures conversion and IPC costs. Benchmark
numbers are evidence only when hardware, JVM, parameters, forks, and raw output are retained; CI
only proves that the benchmark remains runnable.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-032: Bounded Arrow Batch Foundation](../../adr/adr-032-bounded-arrow-batch-foundation.md)
