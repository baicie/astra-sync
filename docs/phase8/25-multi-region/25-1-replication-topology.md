# Phase 8 Slice 25.1: PostgreSQL Replication Topology and Region Topology Discovery

## Status

Implementation Complete (Phase 8, 2026-08-19). This slice implements the first
two open decisions from the Phase 7 Slice 25 design cluster:

1. Checkpoint replication transport
2. Region topology discovery

See [`docs/phase8/closeout.md`](../closeout.md) for the implementation record
and Phase 9 [`docs/phase9/closeout.md`](../../phase9/closeout.md) for the
end-to-end test coverage.

## Context

ADR-048 §"Checkpoint replication" records that the checkpoint store
replicates through an object-storage-backed channel. The ADR leaves the
transport open: PostgreSQL logical replication, object-storage-backed
write-ahead log, or Kubernetes-native CSI snapshot.

ADR-048 §"Cross-Region gRPC Channel" records that the cross-region gRPC
channel carries replication events. The ADR does not specify how the secondary
region discovers the primary region's PostgreSQL endpoint.

This slice answers both questions and implements the resulting code.

## Decision

### Checkpoint Replication Transport: Object-Storage-backed WAL

The checkpoint manifests (ADR-026, ADR-034) replicate through an
**object-storage-backed write-ahead log (WAL)**. The WAL is a new object
prefix under the existing checkpoint bucket:

```
s3://<checkpoint-bucket>/region-replication/
  wal/
    <region-primary>/<wal-sequence>.arrow.gz
```

The WAL entries are appended by the primary region's Coordinator and
consumed by the secondary region's replication consumer. The WAL entries
contain:

- The checkpoint manifest URI (already in the Job record)
- The epoch at which the checkpoint completed
- A monotonically increasing sequence number
- A CRC-32C checksum

**Why object-storage WAL over PostgreSQL logical replication or CSI snapshot:**

| Option | Pros | Cons |
|--------|------|------|
| Object-storage WAL | Decoupled from PostgreSQL, works across cloud regions, incremental | Extra storage cost |
| PostgreSQL logical replication | Zero extra storage, native | Couples to PostgreSQL topology, harder cross-cloud |
| CSI snapshot | Consistent with K8s ecosystem | Snapshot granularity too coarse, vendor-specific |

The object-storage WAL is chosen because it is the only option that works
across cloud regions without coupling to the PostgreSQL topology.

### Region Topology Discovery: Deployment-configured Endpoint List

The secondary region discovers the primary region's endpoints through a
**deployment-configured endpoint list** passed as Kubernetes ConfigMap.
The ConfigMap is read at startup by both the API Server and the Controller:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: astrasync-region-topology
data:
  regions.yaml: |
    regions:
      - name: us-east-1
        role: primary
        endpoints:
          apiServer: https://api.us-east-1.astrasync.example
          postgres: postgresql://pg.us-east-1.astrasync.example:5432
          objectStorage: s3://astrasync-checkpoints-us-east-1
      - name: eu-west-1
        role: standby
        endpoints:
          apiServer: https://api.eu-west-1.astrasync.example
          postgres: postgresql://pg.eu-west-1.astrasync.example:5432
          objectStorage: s3://astrasync-checkpoints-eu-west-1
```

**Why deployment-configured over MultiClusterService or Consul:**

| Option | Pros | Cons |
|--------|------|------|
| Deployment-configured | Explicit, auditable, no extra service | Manual updates |
| MultiClusterService | Native K8s, dynamic | Requires service mesh, K8s version dependency |
| Consul | Service mesh integration, dynamic | Extra dependency, operational complexity |

The deployment-configured approach is chosen because it is explicit, auditable,
and does not introduce a new service mesh dependency. Operators update the
ConfigMap when topology changes.

### Cross-Region gRPC Channel

The cross-region gRPC channel uses the Phase 7 Slice 23 mTLS boundary
(ADR-045). The channel carries:

- WAL replication events (primary → secondary)
- Region health status (bidirectional)

The channel does NOT carry user requests. User requests stay region-local.

## Implementation

### New Go Packages

```
control-plane/
  replication/           # Replication package
    wal/
      writer.go         # WAL appender (primary region)
      consumer.go       # WAL consumer (secondary region)
    topology/
      config.go        # ConfigMap topology loader
      discovery.go     # Endpoint discovery
```

### New Helm Chart Values

```yaml
multiRegion:
  enabled: true
  replication:
    walPrefix: region-replication/wal
    flushInterval: 1s
    batchSize: 100
  topology:
    configMapName: astrasync-region-topology
```

### Verification

- Functional: WAL entries are appended by primary and consumed by secondary
- Performance: WAL append latency < 100ms at p99
- Boundary: WAL transport does not couple to PostgreSQL topology

## Consequences

### Positive

- Checkpoint replication is decoupled from PostgreSQL topology
- Region discovery is explicit and auditable
- Cross-region gRPC uses existing mTLS boundary

### Negative

- Extra object-storage cost for WAL storage
- Manual ConfigMap updates when topology changes
- WAL retention policy must be managed by operator

## References

- [ADR-026: Durable Checkpoint and Epoch Fencing Foundation](../adr/adr-026-checkpoint-fencing-foundation.md)
- [ADR-034: Spillable Exchange and Checkpoint Persistence Optimization](../adr/adr-034-spillable-exchange-and-checkpoint-persistence.md)
- [ADR-045: Control-Plane Mutual TLS and Network Boundary](../adr/adr-045-control-plane-mtls-and-network-boundary.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
