# Slice 07 Implementation Plan

- [x] Define `SinkCommitContext`, exactly-once sink specializations, and stable token generation.
- [x] Negotiate exactly-once from replayable source and transactional/idempotent sink capabilities.
- [x] Carry the requested mode through `BatchTask` and the versioned checkpoint Worker protocol.
- [x] Add JDBC marker-table idempotency with an atomic data-plus-marker transaction.
- [x] Preserve unary at-most-once and checkpointed at-least-once task paths.
- [x] Add compiler, Worker, network, JDBC, and remote Coordinator failure-recovery tests.
- [x] Document marker lifecycle, connector requirements, and the exactly-once boundary.
