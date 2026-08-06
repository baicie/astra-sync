# Phase 5 Slice 15 Design: Adaptive Batch and Parallelism Control

## Goals

1. Adjust source read limits from measured processing latency and bounded exchange pressure.
2. Adjust non-checkpoint split concurrency from task latency and remaining backlog.
3. Keep every decision bounded, deterministic, observable through returned metrics, and safe to
   disable.
4. Carry the batch policy through JobSpec, local tasks, remote task requests, and Worker-side
   materialization without changing existing fixed-task callers.

## Batch Policy

`AdaptiveBatchPolicy` contains a minimum, initial limit, target duration, and adjustment cooldown.
The task's existing `maxBatchRecords` remains the hard upper bound. A zero target duration creates
the fixed policy used by all legacy constructors.

Each non-empty batch contributes an `AdaptiveBatchSample` containing processing duration, publisher
queue-wait duration, queue depth, and queue capacity. The controller keeps an EWMA with a 0.75/0.25
old/new weighting. A sample is considered slow when the EWMA is at least 1.25 times the target and
fast when it is at most 0.75 times the target. A full exchange is also slow; an empty exchange is
required for scale-up. Slow samples halve the limit, fast samples double it, and every change is
clamped to `[min, max]` and followed by a cooldown window.

## Parallelism Policy

`AdaptiveParallelismPolicy` contains minimum, initial, maximum parallelism, target task duration,
and cooldown. `AdaptiveParallelismController` uses the same EWMA and hysteresis thresholds. It
scales down on slow completed tasks and scales up only when work remains and completed tasks are
fast. `BatchCoordinator.runAdaptive` submits at most the current target to one serialized queue
per Worker. A lower target waits for in-flight tasks to finish; it never cancels healthy work.

The coordinator caps the configured maximum at the number of available Workers. Existing
round-robin `run` remains the compatibility path.

## Configuration and Wire Contract

`RuntimeSpec` adds optional `adaptiveBatch` and `adaptiveParallelism` objects. Missing objects
produce disabled policies. The Worker protocol carries the batch settings in both normal and
checkpoint task requests. A Worker rejects a task if its materialized policy does not match the
request, preventing coordinator/Worker tuning drift.

## Correctness Boundary

Adaptive decisions only affect the next source read limit and which not-yet-started split receives
an idle Worker. They never reorder rows within a split, skip a batch, alter checkpoint sequence,
or cancel a running task. Checkpoint coordinator scheduling remains ordered and sequential because
its per-split barrier protocol is a correctness boundary.

## Benchmark Contract

JMH adds decision-loop workloads for batch and parallelism controllers. CI runs them as a smoke gate
with a short single iteration; throughput is evidence only and is not a release threshold.
