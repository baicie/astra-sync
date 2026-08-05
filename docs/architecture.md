# AstraSync Architecture Baseline

## Status

Accepted as the long-term architecture baseline. Delivery is incremental and each MVP slice
must state which parts of this document it implements.

## Product Positioning

AstraSync is a distributed data synchronization runtime for databases, message queues, file
systems, data lakes, and data warehouses. The target system unifies bounded full loads,
unbounded change streams, and snapshot-to-CDC handoff behind one job and connector model.

The architecture deliberately reuses established ideas:

- Flink-style DAG execution, checkpoints, state, backpressure, and coordinator/worker roles.
- SeaTunnel-style source enumerator/reader and sink writer/committer boundaries.
- Debezium-native database log capture rather than generic polling presented as CDC.
- Kafka Connect-style connector tasking and offset ownership without forcing Kafka into every
  data path.
- Airbyte-style separation between the control platform and connector workloads.
- Arrow columnar batches for high-throughput paths and row records for low-latency paths.
- Iceberg-style stable field identity and explicit schema evolution.

## Invariants

1. The control plane and data plane are separate ownership and failure domains.
2. Direct transfer is the default. A durable relay is an explicit topology choice.
3. Batch and stream jobs share the same job, connector, and state concepts.
4. Delivery guarantees are negotiated from real source, runtime, and sink capabilities.
5. Every execution path has bounded batches, bounded queues, and explicit backpressure.
6. Coordination metadata, large state, and bulk data use purpose-specific storage.
7. One execution epoch is active for a job at a time; stale writers are fenced.

## Target Topology

```text
Client / CLI / Console
        |
        v
API Server -> Job Compiler -> Controller -> Scheduler
        |                         |
        +---- PostgreSQL / etcd --+
                                  |
                                  v
                         Job Coordinator
                                  |
                     +------------+------------+
                     v                         v
                 Source Worker  -> Exchange -> Sink Worker
                     |                         |
                     +------ Checkpoint -------+
                                |
                         Object Storage
```

The MVP intentionally starts as one Java process. This is a deployment reduction, not a
different architecture: connector and runtime boundaries must remain usable when coordinator
and worker processes are introduced.

## Data Paths

### Direct Pipeline

```text
Source -> bounded runtime exchange -> Sink
```

This is the default for low-latency, same-region synchronization. Reliability ultimately comes
from source positions, checkpoint state, and sink commit semantics.

### Durable Relay

```text
Source -> Kafka/Pulsar -> one or more sink jobs
```

Use it for replay, independent downstream lifecycles, fan-out, or cross-region buffering. It is
not an MVP dependency.

### Batch Materialization

```text
Source -> staged files -> manifest commit -> Destination
```

Use it for very large migration, reusable intermediate results, or constrained cross-cloud
links. It is not an MVP dependency.

## Consistency Model

Checkpoint is the future consistency boundary joining source positions, operator state, and
pending sink commits. Until checkpointing exists, the runtime must not claim end-to-end
exactly-once. A requested guarantee that cannot be satisfied is rejected before data is read;
it is never silently upgraded or downgraded.

## Delivery Phases

| Phase | Outcome |
|---|---|
| 0 | Versioned JobSpec, connector SPI, row protocol, bounded single-node runtime, file/JDBC connectors, basic metrics |
| 1 | Coordinator/worker batch runtime, splits, slots, direct exchange, distributed backpressure, resumable full load |
| 2 | Checkpoints, state backend, writer/committer transactions, epochs, and recovery |
| 3 | Native CDC connectors and snapshot-to-CDC handoff |
| 4 | Reconciliation control plane and high availability |
| 5 | Arrow, adaptive batching/parallelism, spill, and checkpoint optimization |
| 6 | Console, RBAC, registry, lineage, validation, alerts, and Kubernetes operations |

The executable Phase 0 plan is archived in [MVP Delivery Plan](mvp/README.md).

## Decision Records

The durable architecture decisions are indexed in [Architecture Decision Records](adr/README.md).
ADR-001 through ADR-010 cover the long-term architecture. ADR-011 defines the bounded
single-node execution model used to enter Phase 0. ADR-012 fixes the strict JobSpec v1 boundary,
and ADR-013 keeps planning descriptor-only until every validation and capability gate passes.
ADR-021 through ADR-025 define the completed Phase 1 distributed batch runtime, split enumeration,
Worker protocol, resumable progress, and operational JDBC deployment.
ADR-026 through ADR-028 define the checkpoint, sink-commit, and native CDC consistency boundaries.
ADR-029 defines the durable desired-state Job lifecycle and epoch-fenced Phase 4 control-plane
foundation.
