# ADR-007: Storage Separation

## Status

Accepted

## Context

Different types of data have different consistency, capacity, and performance requirements:
- Business metadata needs strong consistency and transactional support
- Coordination data needs high availability and low latency
- Checkpoint/state data needs massive capacity and cost efficiency

Using a single database for everything would either:
- Be too expensive for large state
- Lack necessary consistency guarantees
- Create operational complexity

## Decision

Separate storage by data type:

### PostgreSQL (ACID Database)

**Use for:**
- Connection configurations
- Job specifications and versions
- Schema definitions
- User and RBAC data
- Audit events
- Execution history

**Rationale:** Strong consistency, ACID transactions, mature ecosystem

### etcd (Distributed KV)

**Use for:**
- Controller leader election
- Job coordinator leases
- Worker heartbeats and leases
- Job epoch numbers
- Task assignments
- Temporary resource locks

**Forbidden:** Checkpoints, large state, logs

**Rationale:** Strong consistency, Raft-based HA, low latency

### Object Storage (S3/OSS/HDFS)

**Use for:**
- Checkpoint data
- Savepoints
- RocksDB SST files
- Spill files
- Connector packages
- Job artifacts

**Rationale:** Massive capacity, low cost, good durability

## Consequences

### Positive
- Optimized storage for each data type
- Independent scaling of storage systems
- Clear ownership and backup strategies
- Reduced operational complexity per system

### Negative
- Multiple systems to operate
- Potential consistency challenges between stores
- Increased network dependencies

## References

- [etcd Use Cases](https://etcd.io/docs/v3.5/learning/why/)
- [Flink State Backend](https://nightlies.apache.org/flink/flink-docs-stable/docs/ops/state/state_backends/)
