# Phase 5 Slice 16 Design: Spillable Exchange and Checkpoint Persistence

## Goals

1. Add a bounded, opt-in disk spill path for the existing Worker exchange.
2. Preserve queue backpressure, row order, end-of-input state, failure propagation, and resource
   cleanup when frames are written, read, or deleted.
3. Reduce repeated checkpoint JSON parsing and duplicate durable-write plumbing without changing
   the manifest format or recovery invariants.
4. Carry only portable spill policy fields through JobSpec and Worker protocol; resolve filesystem
   paths on the process that owns the exchange.

## Configuration

`spec.runtime.spill` is absent or disabled by default. An enabled policy requires:

- `maxBytes`: the total encoded frame bytes allowed in one exchange;
- `maxFiles`: the maximum queued spill files and therefore the disk-backed slot bound.

The Worker resolves `ASTRASYNC_WORKER_SPILL_DIRECTORY` when the policy is enabled. A missing,
non-directory, or non-writable root fails task materialization. Existing fixed constructors create
an in-memory `BatchExchange` and never touch disk.

## Spill Frame

Each frame has a magic value, format version, end-of-input flag, row count, and length-delimited
column/value entries. Scalar values use explicit type tags rather than Java serialization. The
codec supports null, strings, integral and floating values, decimal, binary, local date/time, and
offset date/time values. Frame length, row count, column count, string length, and binary length
are checked before allocation. Unsupported connector values fail the exchange explicitly.

The exchange acquires a bounded slot before writing, reserves encoded bytes before publishing the
path, and releases both the slot and bytes after a successful receive or cleanup. A failed writer,
reader, or delete operation wakes both sides through the existing exchange failure signal.

## Checkpoint Persistence

`AtomicCheckpointWriter` centralizes temporary-file creation, direct channel writes, `force(true)`,
atomic replacement, and temporary cleanup for checkpoint and split-progress manifests. Each store
keeps a bounded access-ordered cache of the last state per job during one Coordinator lifetime.
The cache is an optimization only: a new store instance always reloads the durable JSON manifest.
Epoch, plan identity, sequence, completion, and first-success checks remain authoritative.

## Correctness Boundary

Spill changes only where an unconsumed batch is stored. It never reorders queue paths, acknowledges
a checkpoint, or changes a source position. Checkpointed execution keeps its existing ordered
barrier protocol; a spill failure is surfaced before a progress event can be acknowledged.

## Verification

Tests cover fixed compatibility, byte/file bounds, frame round-trip and rejection, receive-order
preservation, cleanup after success/failure, concurrent backpressure, cache reload, atomic
replacement, temporary-file retention, and stale epoch/sequence rejection. A short JMH workload
compares memory and spill enqueue/dequeue decisions; it is a smoke gate, not a throughput SLA.
