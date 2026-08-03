# Single-node Kernel Design

## Context

The initial prototype reads `List<SyncRecord>` from a Source in one call. Its memory usage grows
with the complete input and a slow Sink cannot stop the Source from materializing everything.
It also has no resource lifecycle or structured failure evidence. Both properties conflict with
the architecture invariant that every task has bounded memory and explicit backpressure.

The initial repository API sketches also prevent the Maven reactor from reaching the kernel
tests. Slice 01 may make minimal compile repairs, but it must not prematurely freeze the public
connector contract owned by slice 02.

## Runtime Model

```text
                   maxBatchRecords
                         |
                         v
Source.readBatch(limit) -> SyncBatch -> Transform chain -> Sink.write(record)
          ^                                              |
          +------------- next pull after completion -----+
```

Execution is single-threaded and synchronous. There is no background producer and no queue.
At most one Source batch plus one transformed record is owned by the execution loop.

## Contracts

### SyncRecord

- Owns a defensive copy of an insertion-ordered string-to-value map.
- Keys are non-null; values may be null.
- `with` creates a new record.
- `asMap` exposes an unmodifiable view/copy.

### SyncBatch

- Contains an immutable list of non-null records.
- Carries `endOfInput`, allowing the final non-empty batch to terminate the job without an extra
  poll.
- An empty non-terminal batch is invalid because it would allow an infinite busy loop.
- The runtime rejects a batch whose size exceeds the limit supplied to the Source.

### RecordSource

```java
open()
readBatch(int maxRecords) -> SyncBatch
close()
```

The Source must return no more than `maxRecords`. `open` and `close` default to no-op so simple
test Sources remain concise. The runtime owns lifecycle calls.

### RecordSink

```java
open()
write(SyncRecord)
close()
```

Per-record writes keep the slice minimal. Batch-oriented connector writes are introduced by the
public SPI in slice 02 without adding prefetch to the execution loop.

### RecordTransform

A transform maps exactly one input record to one non-null output record. Filtering, fan-out,
stateful transforms, and asynchronous transforms are outside this slice.

## Builder Configuration

- Source and Sink are required.
- Zero or more transforms run in declaration order.
- `maxBatchRecords` is required to be positive and has a conservative default.
- Built jobs copy the transform list and are immutable.

## Lifecycle

1. Open Source.
2. Open Sink.
3. Pull and process batches until a batch marks end-of-input.
4. Close Sink, then Source.

Only resources whose `open` completed are closed. Closing continues after a close failure so one
bad close does not leak the other resource.

## Failure Model

`SyncJobException` records a `SyncStage` and a partial `SyncResult`:

| Stage | Boundary |
|---|---|
| `SOURCE_OPEN` | Source setup |
| `SINK_OPEN` | Sink setup |
| `SOURCE_READ` | Batch pull and batch-contract validation |
| `TRANSFORM` | Any transform invocation or null transform result |
| `SINK_WRITE` | Sink write |
| `CLOSE` | Resource close when no earlier error exists |

Runtime exceptions are wrapped once at their boundary. If execution already has a primary
failure, close failures are suppressed on that exception. Partial counters include completed
work only: `readCount` increments when a record enters the transform chain and `writtenCount`
increments only after its Sink write returns.

## Metrics

`SyncResult` contains:

- records read;
- records written;
- batches completed;
- maximum observed batch size;
- elapsed nanoseconds.

Elapsed time is monotonic and informative, not a performance SLA. Failed jobs expose a partial
snapshot through `SyncJobException`.

## Backpressure and Bounds

The Sink call is on the same thread as the Source pull loop. A blocked Sink therefore prevents
the next Source read. The runtime maintains no data queue. The explicit batch limit and runtime
validation prevent a connector from accidentally defeating the bound.

This is intentionally not the distributed credit protocol. Phase 1 will preserve the same
boundedness invariant across network buffers and worker queues.

## Compatibility

The kernel classes remain under `io.astrasync.engine.kernel`. Slice 02 adapts the public connector
SPI to this execution behavior. It may replace internal record types without changing the
lifecycle, failure, or bounded-pull invariants.

## Test Strategy

- Multi-batch happy path with ordered transforms.
- Final non-empty batch and empty terminal batch.
- Oversized and empty non-terminal batch rejection.
- A probe Source/Sink demonstrating no prefetch.
- Failure injection at every lifecycle/data stage.
- Primary plus close failure suppression.
- Builder validation and defensive-copy/null tests.
