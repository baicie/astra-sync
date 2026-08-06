# Phase 5 Slice 14 Implementation Plan

## 1. Contract and build

- [x] Define allocator ownership, scalar mappings, IPC framing, and benchmark evidence policy.
- [x] Add ADR-032 and Phase 5 Slice 14 records.
- [x] Activate `arrow-format` tests and the `tests/benchmark` Maven module.

## 2. Arrow batch implementation

- [x] Add strict schema inference and explicit-schema validation.
- [x] Encode supported `RowBatch` values into bounded vectors and decode them without type drift.
- [x] Add idempotent batch ownership, closed-state guards, and parent allocator isolation.
- [x] Add versioned single-batch IPC encode/decode with payload and allocation limits.

## 3. Verification and benchmarks

- [x] Test scalar/null/decimal/temporal round trips and explicit all-null/empty schemas.
- [x] Test malformed values, schemas, frames, closed access, allocation limits, and leak-free close.
- [x] Add JMH row scan, Arrow scan, conversion, and IPC workloads plus local runner scripts.
- [x] Add a non-threshold Arrow benchmark smoke gate to Linux CI.
- [x] Run Maven verify, Spotless, benchmark smoke, and repository diff checks.
- [x] Record verification evidence and require every PR check before squash merge.
