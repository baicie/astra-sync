# ADR-010: Single Active Region per Job

## Status

Accepted

## Context

Cross-region distributed writes create fundamental consistency challenges:
- Network latency differences
- Clock skew between regions
- Split-brain risks during partition
- Conflict resolution complexity

Active-Active multi-region configurations typically result in:
- Complex conflict resolution
- Higher latency for writes
- Increased coordination overhead
- Difficult debugging

## Decision

Each job operates in a single active region:

```
Region A (Active)
├── Job Coordinator
├── Workers
└── Sink Commits

Region B (Warm Standby)
├── Standby Coordinator (passive)
├── Synchronized checkpoints
└── Pre-warmed workers (optional)
```

### Failover Process

1. **Detection**: Control plane detects primary region failure
2. **Epoch Bump**: New epoch assigned in etcd
3. **Fencing**: Old region coordinators/workers fenced
4. **Recovery**: Restore from latest completed checkpoint
5. **Validation**: Verify sink transaction consistency
6. **Resume**: Continue from recovered state

### Cross-Region Replication (for DR only)

Checkpoint manifests replicated asynchronously to standby region:
- Does NOT enable real-time cross-region sync
- Only for disaster recovery purposes
- RPO = checkpoint interval

## Consequences

### Positive
- Simpler consistency model
- Lower write latency (local writes)
- Easier debugging and monitoring
- Clear failover semantics

### Negative
- Higher RPO during failover (checkpoint interval)
- Brief unavailability during region switch
- Requires multi-region control plane for DR

## References

- [Flink HA Configuration](https://nightlies.apache.org/flink/flink-docs-stable/docs/deployment/ha/overview/)
- [Disaster Recovery Patterns](https://docs.microsoft.com/en-us/azure/architecture/resiliency/disaster-recovery)
