# Phase 5 Slice 15 Implementation Plan

## 1. Contract and configuration

- [x] Create the Slice 15 design, implementation plan, verification record, and ADR-033.
- [x] Add strict optional adaptive batch and parallelism JobSpec objects with disabled defaults.
- [x] Define bounded policy, sample, EWMA, hysteresis, cooldown, and fixed-compatibility contracts.

## 2. Runtime and protocol

- [x] Add adaptive controllers and integrate batch sizing into single-node and Worker execution.
- [x] Add adaptive split scheduling while preserving serialized Worker queues and failure handling.
- [x] Carry batch policy through normal/checkpoint Worker protocol requests and validate materialized tasks.

## 3. Verification and benchmarks

- [x] Test bounds, fast/slow signals, queue pressure, cooldown, overflow rejection, and disabled mode.
- [x] Test adaptive scheduling, no cancellation of in-flight tasks, failure propagation, and checkpoint ordering.
- [x] Add JMH controller decision workloads and a CI smoke invocation.
- [x] Run Maven verify, Spotless, benchmark smoke, and repository diff checks.
- [x] Record local verification evidence; require every PR check before squash merge.
