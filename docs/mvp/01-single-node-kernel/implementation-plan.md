# Slice 01 Implementation Plan

## Preconditions

- [x] Confirm branch base and dirty worktree.
- [x] Read the supplied architecture and existing ADRs.
- [x] Record scope, non-goals, contracts, failure semantics, and acceptance tests.
- [x] Add ADR-011 before changing the execution contract.

## Steps

### 1. Restore the Testable Baseline

- Make the initial connector and engine API sketches syntactically valid with minimal semantic
  change.
- Do not implement the slice-02 public connector model in this step.
- Gate: Maven reaches the engine test compilation phase.

### 2. Introduce Bounded Data Contracts

- Make `SyncRecord` immutable and null-safe.
- Add validated `SyncBatch` with end-of-input.
- Extend Source/Sink with default lifecycle methods.
- Gate: contract tests cover defensive copies and invalid batches.

### 3. Implement the Execution Loop

- Add a positive maximum batch setting.
- Pull, transform, and write synchronously with no prefetch.
- Validate Source output at the runtime boundary.
- Gate: ordered multi-batch and no-prefetch tests pass.

### 4. Add Failure and Metric Evidence

- Add stages, partial results, duration, batch counters, and maximum observed size.
- Close all opened resources in reverse order and preserve suppressed failures.
- Gate: injected failures at Source, transform, Sink, and close boundaries pass.

### 5. Verify and Archive

- Run focused tests, reactor tests, formatting checks, and inspect the final diff/status.
- Record exact commands, environment, results, limitations, and commit in `verification.md`.
- Update the slice and Phase 0 status.
- Gate: no undocumented failure and no untracked implementation file remains.

## Change Control

If implementation requires a different threading model, buffering policy, delivery claim, or
connector ownership boundary, update the design and add/supersede an ADR before changing code.
