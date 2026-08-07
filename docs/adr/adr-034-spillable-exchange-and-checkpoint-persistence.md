# ADR-034: Spillable Exchange and Checkpoint Persistence Optimization

## Status

Accepted

## Context

The Phase 1 exchange is bounded in memory, which protects the process but makes a slow sink
stall the source as soon as the in-memory batch capacity is full. Checkpoint stores also
serialize and reread the complete JSON state for every progress event. Phase 5 needs a bounded
disk escape hatch without weakening backpressure, checkpoint ordering, or epoch fencing.

## Decision

1. Keep the existing in-memory `BatchExchange(int)` constructor as the default compatibility path.
   A JobSpec may opt into a spill policy with positive byte and file limits.
2. A spill-enabled Worker writes each queued `RowBatch` to a versioned, length-checked frame in a
   task-owned temporary directory. The queue still has a fixed slot capacity and the spill area
   has a hard byte limit. A consumed frame is deleted immediately.
3. Spill frames support the connector API scalar values used by the Arrow foundation, preserve
   insertion-ordered columns and the end-of-input flag, and reject unsupported values or malformed
   frames before materialization.
4. Spill directories are process-local resources. The Worker receives only the policy signature
   over the protocol and resolves its directory from `ASTRASYNC_WORKER_SPILL_DIRECTORY`; the
   Coordinator never assumes that a Worker filesystem is shared.
5. Checkpoint and resumable-progress stores retain their JSON manifest format and atomic replace
   semantics. A shared durable writer writes and forces one temporary file, atomically replaces
   the manifest, and removes temporary artifacts on both success and failure. A bounded per-store
   state cache avoids rereading the same manifest during a sequential Coordinator run.
6. Checkpoint tasks carry the same spill policy signature as ordinary tasks. Spill remains inside
   the task exchange: a batch is acknowledged only after the existing sink commit and ordered
   checkpoint progress callback, so checkpoint sequence, completion, plan identity, and epoch
   checks remain unchanged.

## Consequences

The opt-in path tolerates larger bursts while remaining bounded by memory slots and disk bytes.
It adds local disk latency and requires a writable Worker spill directory. Failure of the spill
volume fails the exchange and wakes both sides. Existing jobs remain in-memory and preserve their
current behavior. Checkpoint recovery remains format-compatible while reducing repeated JSON work.
