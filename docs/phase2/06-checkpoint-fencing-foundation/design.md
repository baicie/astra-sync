# Phase 2 Slice 06: Checkpoint and Epoch Fencing Foundation

## Problem

Phase 1 records only a successful split after the Worker has finished. A failure after a JDBC batch
commit forces the entire split to run again, and an old Coordinator has no protocol-level identity
that a Worker can reject. The next slice needs a durable position and a fencing token without
pretending that a checkpoint alone provides exactly-once delivery.

## Goals

1. Give each running job a monotonically increasing execution epoch.
2. Persist an atomic checkpoint record after each successfully committed batch.
3. Resume a split from its latest durable source position when the connector supports it.
4. Carry job identity, epoch, and checkpoint sequence through the Coordinator/Worker protocol.
5. Fence stale requests before materialization and before every sink commit.
6. Preserve the existing at-most-once behavior for non-checkpointable jobs.

## Non-goals

- A two-phase or XA transaction across the Coordinator, Worker, and JDBC database.
- An exactly-once claim for a sink that is neither transactional nor idempotent.
- Savepoints, arbitrary operator state, CDC offsets, or dynamic split discovery.
- A shared mutable filesystem mounted by every Worker.

## Runtime Contract

The new execution path uses these immutable values:

```text
CheckpointRecord {
  jobId
  executionEpoch
  splitId
  checkpointSequence
  sourcePosition
  sinkCommitToken
  batchDigest
}
```

`executionEpoch` is allocated once for a Coordinator invocation. `checkpointSequence` is strictly
monotonic within a split. `sourcePosition` is connector-owned canonical data, not a byte offset
invented by the runtime. `sinkCommitToken` is opaque to the checkpoint store and is reserved for
the later transactional/idempotent sink protocol. The record is accepted only when its job, epoch,
split fingerprint, and predecessor sequence match the current durable state.

The opt-in connector interfaces are separate from the Phase 1 `BatchSource` and `BatchSink`
interfaces. Existing connectors keep their current behavior. A checkpointable source exposes a
position after a batch and can open from that position. A checkpoint-aware sink reports the token
for the batch it has committed and can reject a stale epoch before writing.

## Execution Flow

```text
Coordinator                 Worker                  CheckpointStore
     | acquire epoch             |                         |
     |-------------------------->|                         |
     | execute(job, epoch, split, cursor)                  |
     |-------------------------->|                         |
     |                            | open source at cursor  |
     |                            | write and commit batch |
     |<-- BatchCommitted(pos, token, seq) ------------------|
     | persist checkpoint --------------------------------->|
     |<-- CheckpointAck(seq) --------------------------------|
     |                            | continue after ack     |
     |<-- TaskCompleted ----------|                         |
```

The progress channel is bounded. A Worker does not acknowledge a committed batch as durable until
the Coordinator has persisted it. The Coordinator may allow more than one in-flight checkpoint only
when the store and connector contract explicitly support ordered acknowledgement; the first
implementation uses one checkpoint barrier per Worker task.

## Failure Semantics

| Failure point | Recovery behavior | Guarantee |
|---|---|---|
| Before sink commit | Previous checkpoint remains authoritative; batch is retried | No loss, possible replay |
| After sink commit before checkpoint durability | Resume from previous position and replay the batch | At-least-once only |
| After checkpoint acknowledgement | Resume after the acknowledged source position | At-least-once |
| Stale epoch before materialization | Worker rejects the request | Stale execution fenced |
| Stale epoch before sink commit | Worker aborts the batch and reports fencing | Stale write prevented |

The checkpoint store uses a per-job lock plus atomic replacement. A newer epoch supersedes an older
one. A stale Coordinator cannot overwrite a checkpoint created by the current epoch. Recovery starts
from the latest complete record, never from a partially written temporary file.

## Protocol Evolution

The current unary execute request is retained for Phase 1 at-most-once jobs. Checkpointed execution
uses a versioned progress stream with these events:

- `ExecuteTask(jobId, executionEpoch, split, checkpointSequence, sourcePosition)`
- `BatchCommitted(taskId, sequence, sourcePosition, sinkCommitToken, batchDigest)`
- `CheckpointAck(taskId, sequence, checkpointFingerprint)`
- `TaskCompleted` or `TaskFailed`

The stream rejects a missing or unexpected event, a sequence regression, a changed split descriptor,
or an epoch mismatch. A protocol version mismatch fails the task before any connector resource is
opened.

## JDBC Scope

The first JDBC checkpointable source requires an explicitly configured, non-null, stable unique
resume column. Its source position is the last committed key and its split query adds a strict
resume predicate within the original range. A range split whose resume key is not unique remains on
the Phase 1 path and cannot request checkpointed recovery.

The JDBC sink continues to commit batches with its existing transaction boundary in Slice 06. It
therefore reports a commit token for observability but does not make duplicate replay invisible.
Slice 07 must add dialect-aware idempotent/upsert or transactional commit support before the compiler
accepts `exactly-once`.

## Verification

- A file checkpoint store writes and reloads ordered records atomically.
- A stale epoch and stale checkpoint sequence are rejected without changing the current record.
- A real TCP Worker sends a batch progress event and blocks until its checkpoint acknowledgement.
- A restarted Coordinator resumes only the unfinished portion of a checkpointable split.
- A sink commit followed by a simulated Coordinator crash replays the batch and is reported as
  at-least-once, not exactly-once.
- Phase 1 unary execution and non-checkpointable JDBC jobs remain behaviorally unchanged.
