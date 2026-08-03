# Phase 1 Slice 01 Verification

## Status

Verified on `phase1/01-distributed-batch-runtime`.

## Scope

This record covers the first Phase 1 slice only: in-process Coordinator scheduling, Worker
execution, bounded direct exchange, task-local failure propagation, and resource closure. It does
not verify network transport, durable scheduling, checkpointing, retries, or exactly-once.

## Automated Checks

| Command | Result |
|---|---|
| `mvn.cmd -B -ntp -pl engine/runtime,engine/coordinator,engine/worker -am test -DskipITs` | PASS; 76 tests across Connector API, Engine, Runtime, Coordinator, and Worker with 0 failures, 0 errors, 0 skipped |
| `git diff --check` | PASS |

## Acceptance Evidence

- The root Maven reactor now builds `engine/runtime`, `engine/coordinator`, and `engine/worker`.
- `BatchCoordinatorTest` proves round-robin task assignment, per-Worker serialization, aggregate
  metrics, and duplicate task rejection.
- `BatchExchangeTest` proves capacity-one publication blocks and both waiting sides are released
  when the exchange fails.
- `InProcessBatchWorkerTest` proves concurrent Source/Sink execution, terminal batch handling,
  partial metrics, structured sink failure, and reverse resource closure.
- Existing Phase 0 Engine tests remain green, so the single-process runtime boundary is unchanged.

## Known Limits

Workers are in-process implementations and the split enumerator receives resource-owned tasks;
connector-specific split discovery is still planned. Each task owns its Source and Sink, so shared
sink transactions and cross-task commit coordination are not supported. A task failure cancels
outstanding Coordinator futures; there is no retry or resume behavior. Delivery remains
at-most-once.
