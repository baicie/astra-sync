# Phase 5 Slice 16 Verification

## Required Local Checks

| Check | Result |
|---|---|
| Focused exchange, checkpoint, parser, Worker, and protocol tests | PASS: 121 tests across connector-api, engine, runtime, checkpoint, and network |
| `mvn -B -ntp verify -DskipITs` | PASS: 31-module reactor |
| `mvn -B -ntp spotless:check` | PASS |
| Spill exchange JMH smoke invocation | PASS: `SpillExchangeBenchmark.spillPublishReceive`, 293.320 ops/s smoke result |
| `git diff --check` | PASS |

## Acceptance Evidence

- Spill is opt-in and fixed in-memory constructors remain behaviorally compatible.
- Queue slots, encoded frame bytes, frame dimensions, and temporary file ownership are bounded.
- Frames preserve rows and end-of-input state, reject malformed/unsupported input, and delete after receive.
- Exchange failure wakes blocked producers and consumers and removes owned spill artifacts.
- Checkpoint manifests remain JSON-compatible, atomically forced, cache-reloadable, and protected by
  the existing epoch, plan, sequence, and completion invariants.
- CI executes all applicable repository checks and the spill smoke benchmark before squash merge.

## Pull-request Gate

Pending PR and post-merge evidence. The local gates above are green; the PR gate will be recorded
after the remote branch is created and GitHub Actions completes.
