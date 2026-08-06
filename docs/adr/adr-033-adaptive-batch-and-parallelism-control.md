# ADR-033: Adaptive Batch and Parallelism Control

## Status

Accepted

## Context

The fixed `maxBatchRecords` and worker assignment limits preserve boundedness, but they leave
throughput on the table when source, sink, and exchange pressure vary during a run. Slice 14
established Arrow and benchmark contracts; the runtime now needs a controlled way to use measured
signals without weakening backpressure, checkpoint, or epoch guarantees.

## Decision

Add opt-in adaptive batch and non-checkpoint parallelism policies. Each policy is bounded and uses
an EWMA, 25% hysteresis, and a cooldown after every change. Batch decisions use processing latency,
publisher wait, and exchange occupancy. Parallelism decisions use completed-task latency and
remaining split backlog. The task's existing maximum batch size and the available Worker count are
hard caps.

Carry adaptive batch settings through JobSpec and both Worker task request forms. Workers reject a
request whose materialized policy differs from the request. Existing constructors and missing
JobSpec objects create fixed policies. Checkpoint scheduling remains ordered and sequential; only
the batch read limit may adapt there.

## Consequences

The runtime can react to slow sinks and under-filled exchanges without unbounded queues or abrupt
task cancellation. The protocol gains a small version-compatible optional message. Tuning is
deterministic and testable, but it introduces configuration and telemetry semantics that must remain
documented. Parallelism is intentionally excluded from the checkpoint barrier scheduler until an
explicit checkpoint-aware scheduling design exists.
