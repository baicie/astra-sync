# ADR-006: Epoch Fencing for Coordinator

## Status

Accepted

## Context

When a Job Coordinator fails and a new coordinator takes over, there is a window where:
1. Old coordinator might still be running (network partition)
2. Old workers might still be communicating
3. Sink commits could be duplicated or conflicting

Heartbeat-based approaches are insufficient because:
- Network partitions can cause long GC pauses
- Old coordinators might recover after brief failures
- No protection against split-brain scenarios

## Decision

Implement epoch-based fencing:

```
JobEpoch = monotonically_increasing_number

Every Coordinator Election:
    1. Obtain new Epoch from etcd
    2. Epoch = previous + 1
    3. Store Epoch in job metadata

Every Sink Write Request:
    Include: JobId, ExecutionId, CheckpointId, Epoch

Sink Committer Validation:
    If request.Epoch < currentEpoch:
        REJECT (stale request)
        Log security event
```

### Epoch Increment Triggers

- Controller leader election
- Job restart after failure
- Manual failover
- Rolling upgrade

## Consequences

### Positive
- Prevents split-brain writes
- Clear validation of coordinator identity
- Works across network partitions
- Audit trail of coordinator changes

### Negative
- Requires epoch in all commit protocols
- Committer must maintain current epoch state
- Additional complexity in recovery path

## References

- [Flink Epoch](https://nightlies.apache.org/flink/flink-docs-stable/docs/ops/state/checkpoints/)
- [ZooKeeper Fencing](https://zookeeper.apache.org/doc/current/recipes.html#sc_recipes_Fencing)
