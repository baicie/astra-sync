# Phase 5: Performance Optimization

Phase 5 improves bulk throughput and resource efficiency while preserving bounded memory,
backpressure, checkpoint correctness, and epoch fencing.

**Status: In Progress**

## Delivery Slices

| Slice | Scope | Status |
|---:|---|---|
| 14 | Bounded Arrow batches, deterministic Row conversion, IPC framing, and benchmark baseline | Complete |
| 15 | Adaptive batch sizing and parallelism based on measured workload signals | Complete |
| 16 | Spillable exchange and checkpoint persistence optimization | Planned |

## Slice 14 Boundary

Slice 14 turns the empty `arrow-format` scaffold into a usable columnar primitive. Every batch has
a strict child-allocator limit and explicit close ownership. Supported scalar rows round-trip
through Arrow vectors and a versioned IPC frame, including terminal batch state. A JMH module
provides reproducible row, vector, conversion, and IPC baselines plus a non-threshold CI smoke gate.

This slice does not select Arrow in the existing Worker data path, add native Arrow JDBC readers,
adapt batch sizes, spill buffers, or change checkpoint cadence. Those changes depend on the format
and measurement contracts established here.

## Records

- [Slice 14](14-arrow-batch-foundation/README.md):
  [design](14-arrow-batch-foundation/design.md),
  [implementation plan](14-arrow-batch-foundation/implementation-plan.md), and
  [verification](14-arrow-batch-foundation/verification.md)
- [Slice 15](15-adaptive-batch-control/README.md):
  [design](15-adaptive-batch-control/design.md),
  [implementation plan](15-adaptive-batch-control/implementation-plan.md), and
  [verification](15-adaptive-batch-control/verification.md)
- [ADR-032: Bounded Arrow Batch Foundation](../adr/adr-032-bounded-arrow-batch-foundation.md)
- [ADR-033: Adaptive Batch and Parallelism Control](../adr/adr-033-adaptive-batch-and-parallelism-control.md)
