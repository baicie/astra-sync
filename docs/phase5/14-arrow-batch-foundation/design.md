# Phase 5 Slice 14 Design: Bounded Arrow Batches

## Goals

1. Represent bulk scalar rows in Arrow vectors without unbounded off-heap allocation.
2. Define deterministic Java-to-Arrow types, nullability, column ordering, and temporal semantics.
3. Preserve row values and terminal-batch state through a versioned Arrow IPC payload.
4. Make ownership misuse and malformed input fail close to the boundary.
5. Establish repeatable JMH workloads before changing production runtime selection.

## Ownership

Each encode or decode operation creates a named child allocator under a caller-owned parent. The
child limit is the maximum off-heap budget for that batch. `ArrowBatch` owns the child plus either a
created `VectorSchemaRoot` or the `ArrowStreamReader` that owns a decoded root. Its close operation
is idempotent and closes the resource before the child allocator. Any close failure retains later
failures as suppressed exceptions.

The parent allocator is never closed by the batch. Public accessors reject use after close. The
returned root is borrowed for vectorized reads only; ownership cannot be transferred through that
accessor.

## Scalar Mapping

| Java value | Arrow type | Decode value |
|---|---|---|
| `Boolean` | Bool | `Boolean` |
| `Byte`, `Short`, `Integer`, `Long` | signed Int8/16/32/64 | same boxed type |
| `Float`, `Double` | FloatingPoint single/double | same boxed type |
| `BigDecimal` | Decimal128 | common exact scale |
| `String` | Utf8 | `String` |
| `byte[]` | Binary | copied `byte[]` |
| `LocalDate` | Date day | `LocalDate` |
| `LocalTime` | Time nanosecond/64 | `LocalTime` |
| `LocalDateTime` | Timestamp nanosecond/no zone | `LocalDateTime` |
| `OffsetDateTime` | Timestamp nanosecond/UTC | same instant in UTC |

Inference uses the first row's insertion order and requires every row to have exactly the same
column names. Nulls make a field nullable. Every non-null value in a column must use the same Java
type. Decimal inference computes one exact common scale and enough precision for all values, up to
Decimal128 precision 38. An all-null column and an empty batch require an explicit schema.

Explicit schemas support only the table above, signed integers, Decimal128, day dates,
nanosecond/64 times, and nanosecond timestamps. Unsupported widths, units, unsigned integers,
dictionaries, nested children, or duplicate/blank field names are rejected.

## IPC Frame

```text
4 bytes magic "ASTR"
1 byte version (1)
1 byte flags (bit 0 = end of input)
2 bytes reserved (zero)
remaining bytes = one Arrow IPC stream
```

Unknown versions, flags, non-zero reserved bytes, missing record batches, truncated streams, and
payloads larger than the supplied limit fail. Decode keeps the stream reader as the batch-owned
resource so buffers are not copied a second time.

## Benchmark Contract

JMH owns one long-lived parent allocator per trial and closes every temporary batch. Parameters
cover small and bulk row counts. Workloads measure row scan, Arrow vector scan, Row-to-Arrow encode,
and framed IPC decode. Normal runs use warmup and multiple forks. The CI smoke invocation uses one
short measurement only to catch build, reflection, JVM-option, and allocator regressions; it cannot
establish a throughput SLA.

## Deferred Work

Native columnar connector SPI, Worker data-path selection, adaptive batch sizing/parallelism,
network shuffle, spill, compression negotiation, and checkpoint aggregation are deferred. Existing
Row execution and delivery guarantees remain unchanged.
