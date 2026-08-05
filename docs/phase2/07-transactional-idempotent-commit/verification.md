# Slice 07 Verification

## Automated Checks

| Check | Result |
|---|---|
| Focused connector-api, engine, runtime, JDBC, and Worker tests | PASS; 123 tests, 0 failures, 0 errors |
| Remote Coordinator/JDBC exactly-once replay-window test | PASS; sink commit before simulated failure is retried without duplicate target rows |
| Slice 06 checkpoint Coordinator regression tests | PASS; at-least-once resume and completion behavior preserved |
| `mvn spotless:check` | PASS after repository formatting |
| `git diff --check` | PASS |

## Acceptance Evidence

- `JobCompilerTest` accepts exactly-once only with a replayable source, explicit `resumeColumn`,
  and `IDEMPOTENT_WRITE` or `TRANSACTIONAL_COMMIT` sink capability.
- `InProcessBatchWorkerTest` verifies a stable commit context and rejects a plain checkpoint sink
  before opening resources for an exactly-once task.
- `CommitTokensTest` verifies that execution epoch changes do not change a logical batch token while
  a digest change does.
- `JdbcBatchSinkTest` verifies marker-table deduplication and token/digest mismatch rejection.
- `CheckpointCoordinatorApplicationTest` starts a real TCP Worker, commits the first JDBC batch,
  fails after the sink commit but before checkpoint durability, and proves the next execution
  completes with one copy of every target row.

## Boundary

Exactly-once depends on a replayable stable source position, a correctly implemented sink SPI, and
the target database's transaction durability. A plain JDBC INSERT, a missing marker table privilege,
or a descriptor/runtime mismatch is not treated as exactly-once.
