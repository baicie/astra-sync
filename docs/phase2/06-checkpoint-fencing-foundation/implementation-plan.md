# Slice 06 Implementation Plan

- [x] Define the checkpoint record, atomic file store, split-plan binding, completion record, and
      monotonic execution epoch contract.
- [x] Add opt-in checkpointable source and sink interfaces while preserving the Phase 1 SPI.
- [x] Add the versioned Worker progress protocol, ordered Coordinator acknowledgement, and stale
      epoch rejection at materialization and sink commit boundaries.
- [x] Add JDBC `resumeColumn` validation, strict resume predicates, checkpoint source positions,
      and sink commit tokens.
- [x] Integrate checkpoint recovery into the Coordinator and preserve the Phase 1 unary path.
- [x] Add store, protocol, Worker, JDBC, compiler, and Coordinator recovery/fencing tests.
- [x] Document the at-least-once boundary and verification evidence.
