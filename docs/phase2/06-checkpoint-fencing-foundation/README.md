# Slice 06: Checkpoint and Epoch Fencing Foundation

Slice 06 adds opt-in at-least-once recovery to the Phase 1 distributed batch runtime. The
Coordinator allocates a monotonically increasing execution epoch, persists one checkpoint after
each acknowledged sink commit, and rejects stale execution requests before task materialization
and before a Worker sink commit.

## Enable Recovery

Checkpointed JDBC execution requires `at-least-once` and an explicit stable, non-null, unique
`resumeColumn`:

```yaml
apiVersion: sync.astrasync.io/v1
kind: SyncJob
metadata:
  name: orders-checkpointed
spec:
  source:
    connector: jdbc
    options:
      url: jdbc:postgresql://source.example/orders
      table: orders
      splitColumn: id
      resumeColumn: id
      splitCount: "4"
  sink:
    connector: jdbc
    options:
      url: jdbc:postgresql://target.example/orders
      table: orders_copy
  delivery:
    guarantee: at-least-once
  runtime:
    maxBatchRecords: 1000
```

The JDBC source validates that `resumeColumn` is non-null and unique before the split plan is
accepted. Its checkpoint position is the last committed key, and resumed reads use a strict `>`
predicate within the original split range.

Run the existing Coordinator and Worker entry points with the same process configuration as Phase
1. The Coordinator must have a durable, writable `ASTRASYNC_COORDINATOR_PROGRESS_DIR` that is
owned by one active Coordinator invocation. Workers need the same immutable JobSpec through
`ASTRASYNC_WORKER_JOB_SPEC`, but do not need access to the Coordinator checkpoint directory.

On checkpointed success, Coordinator output includes `executionEpoch`, `recoveredSplits`,
`resumedSplits`, and `executedSplits`. A split with a partial checkpoint is counted as recovered; a
split with a durable completion is counted as resumed and is not materialized again. The Phase 1
at-most-once path retains its existing success output.

## Delivery Boundary

The checkpoint is durable only after the Coordinator records the Worker progress event and sends
the acknowledgement. A process failure after the sink commits and before that record is durable
can replay the committed batch. This Slice therefore provides at-least-once recovery, not
exactly-once delivery. A requested `exactly-once` guarantee remains rejected.

Non-checkpointable jobs continue to use the Phase 1 unary at-most-once protocol and result shape.

See [design.md](design.md), [implementation-plan.md](implementation-plan.md), and
[verification.md](verification.md).
