# Slice 06 Verification

## Automated Checks

| Check | Result |
|---|---|
| Focused Coordinator, network, Worker, JDBC, compiler, and checkpoint tests | PASS; all 131 tests passed with 0 failures, errors, or skips |
| `mvn.cmd -B -ntp -pl engine/coordinator -am test` | PASS; 11 upstream modules and 131 tests |
| `mvn.cmd -B -ntp clean verify` | PASS; all 29 Reactor modules, compilation, tests, JaCoCo checks, and packaging |
| `mvn.cmd -B -ntp spotless:check` | PASS; all configured Java sources clean |
| `git diff --check` | PASS |

## Acceptance Evidence

- `FileCheckpointStoreTest` verifies atomic reload, monotonic epochs and sequences, stale epoch
  rejection, and split-plan binding.
- `CheckpointNetworkTest` verifies a real TCP progress event blocks until the Coordinator sends the
  checkpoint acknowledgement and rejects a stale epoch before task materialization.
- `InProcessBatchWorkerTest` verifies a stale epoch is rejected before sink commit.
- `JdbcRangeSplitSourceTest` verifies the strict resume query and rejects nullable or duplicate
  resume keys.
- `JobCompilerTest` verifies that checkpointed `at-least-once` plans require a replayable source
  and explicit `resumeColumn`, while `exactly-once` remains rejected.
- `CheckpointCoordinatorApplicationTest` verifies a real TCP JDBC run persists the first batch,
  resumes from its position after failure, records the next execution epoch, skips durable
  completion, and rejects split-plan drift.
- Existing Phase 1 Coordinator tests verify the unary at-most-once path and its result compatibility.

## Failure Semantics

Checkpoint durability follows the sink commit, so a crash between those events can replay one
batch. The implementation makes no exactly-once claim and leaves transactional or idempotent sink
commit support to Slice 07.
