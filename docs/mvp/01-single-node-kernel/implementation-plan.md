# Slice 01 Implementation Plan

## Preconditions

- [x] Confirm branch base and dirty worktree.
- [x] Read the supplied architecture and existing ADRs.
- [x] Record scope, non-goals, contracts, failure semantics, and acceptance tests.
- [x] Add ADR-011 before changing the execution contract.

## Steps

### 1. Restore the Testable Baseline

- [x] Make the initial connector and engine API sketches syntactically valid with minimal semantic
  change.
- [x] Do not implement the slice-02 public connector model in this step.
- [x] Gate: Maven reaches the engine test compilation phase.

### 2. Introduce Bounded Data Contracts

- [x] Make `SyncRecord` immutable and null-safe.
- [x] Add validated `SyncBatch` with end-of-input.
- [x] Extend Source/Sink with default lifecycle methods.
- [x] Gate: contract tests cover defensive copies and invalid batches.

### 3. Implement the Execution Loop

- [x] Add a positive maximum batch setting.
- [x] Pull, transform, and write synchronously with no prefetch.
- [x] Validate Source output at the runtime boundary.
- [x] Gate: ordered multi-batch and no-prefetch tests pass.

### 4. Add Failure and Metric Evidence

- [x] Add stages, partial results, duration, batch counters, and maximum observed size.
- [x] Close all opened resources in reverse order and preserve suppressed failures.
- [x] Gate: injected failures at Source, transform, Sink, and close boundaries pass.

### 5. Verify and Archive

- [x] Run focused tests, reactor tests, formatting checks, and inspect the final diff/status.
- [x] Record exact commands, environment, results, limitations, and commit in `verification.md`.
- [x] Update the slice and Phase 0 status.
- [x] Gate: no undocumented failure and no untracked implementation file remains.

## Change Control

If implementation requires a different threading model, buffering policy, delivery claim, or
connector ownership boundary, update the design and add/supersede an ADR before changing code.
