# Phase 2 Slice 07: Transactional or Idempotent Sink Commit

## Problem

Slice 06 durably acknowledges a sink-committed batch after the sink call returns. A Coordinator
crash between those two events causes the source batch to be replayed. Checkpoint fencing prevents a
stale execution from writing, but it cannot make a legitimate retry of the same batch invisible.

## Goals

1. Close the sink-commit/checkpoint replay window for sinks with a real commit protocol.
2. Negotiate exactly-once only when the source is replayable and the sink advertises transactional
   or idempotent commit support.
3. Give retries of the same logical batch a stable token that is independent of execution epoch.
4. Preserve the Phase 1 unary at-most-once path and Slice 06 at-least-once behavior.
5. Provide a JDBC idempotent sink implementation whose data and commit marker are atomically
   committed in one database transaction.

## Non-goals

- Claiming exactly-once for a sink that only performs an ordinary INSERT or batch commit.
- A distributed XA transaction across the source database, Coordinator, and sink database.
- Making arbitrary user transforms deterministic; transforms remain unsupported in this runtime.
- Inferring a target table's unique key or silently converting arbitrary SQL into an upsert.

## Capability Contract

The compiler accepts `exactly-once` only for `compileCheckpointed` plans when all of the following
hold:

- the source advertises `REPLAYABLE_OFFSET`;
- the source has an explicit stable unique `resumeColumn`; and
- the sink advertises `TRANSACTIONAL_COMMIT` or `IDEMPOTENT_WRITE`.

The static descriptor is only the first negotiation step. A checkpoint task marked exactly-once must
also materialize an `ExactlyOnceBatchSink`. The Worker rejects a mismatched implementation before
opening source or sink resources. This prevents a descriptor-only declaration from silently falling
back to at-least-once behavior.

`IdempotentBatchSink` and `TransactionalBatchSink` are connector-facing specializations of the
exactly-once sink contract. Both receive a `SinkCommitContext` containing the stable logical token,
batch sequence, digest, job, and split. Repeating a context with the same token must not make the
rows visible twice. A token reused with a different batch digest must fail closed.

## Stable Commit Identity

The Worker computes the token before the sink write:

```text
token = SHA-256("astrasync-v1|" + jobId + "|" + splitId + "|" + sequence + "|" + batchDigest)
```

The execution epoch is intentionally excluded. A retry after a Coordinator restart therefore uses
the same token. The epoch remains part of the checkpoint identity and continues to fence stale
writers; it is not part of the logical batch identity.

The checkpoint record stores the same token and digest. A sink must validate the supplied token and
digest as one pair. The token is opaque to the checkpoint store but deterministic to the runtime.

## Execution Flow

```text
Coordinator                 Worker                 Exactly-once sink
     | execute(epoch, cursor) |                         |
     |----------------------->|                         |
     |                        | read batch, seq, digest |
     |                        | write(token, digest)    |
     |                        |<-- committed/no-op -----|
     |<-- BatchCommitted(token, digest) -----------------|
     | persist checkpoint     |                         |
     |----------------------->|                         |
     |                        | advance source cursor  |
```

The existing progress stream remains the checkpoint barrier. The difference is that a retry of a
batch whose sink transaction already committed finds the token and performs no second data write.
The Worker still waits for `CheckpointAck` before advancing the source, and still asserts the epoch
before the sink call and after the acknowledgement.

## JDBC Commit Protocol

JDBC exactly-once mode uses a connector-managed marker table. Each batch transaction contains both:

1. the target row batch; and
2. one unique `commit_token` marker.

If the token already exists, the sink commits no target rows and reports the existing token. If it
does not exist, the sink inserts the rows and marker before committing. A database crash cannot
leave only one side committed when the database honors its transaction contract. The marker table is
configurable with `commitTokenTable` and defaults to a deterministic companion table name derived
from the target table. Existing JDBC jobs that do not request exactly-once keep their current batch
transaction and do not create marker tables.

## Failure Semantics

| Failure point | Recovery behavior | Guarantee |
|---|---|---|
| Before sink commit | Batch is retried from the previous checkpoint | Exactly-once if the sink commits by token |
| After sink commit before checkpoint durability | Same token is retried; sink no-ops | Exactly-once |
| After checkpoint acknowledgement | Resume after the acknowledged position | Exactly-once |
| Stale epoch before sink commit | Worker rejects the write | Stale write prevented |
| Token exists with a different digest | Sink fails closed | No silent corruption |
| Sink lacks the runtime protocol | Task is rejected before resources open | No false exactly-once claim |

At-most-once and at-least-once do not receive the stable-token path merely because a connector
implements it. They continue to use their requested execution mode and existing compatibility
contracts.

## Verification

- Compiler accepts exactly-once only for a replayable source, explicit resume key, and transactional
  or idempotent sink capability.
- A task marked exactly-once rejects a plain `CheckpointableBatchSink`.
- The stable token is identical across execution epochs and changes when the batch digest changes.
- A test sink sees a repeated token as one committed batch and rejects a token/digest mismatch.
- JDBC retry after a simulated Coordinator failure leaves one target batch and one marker.
- A stale epoch is rejected before both normal and exactly-once sink writes.
- Existing Phase 1 and Slice 06 tests remain green.
