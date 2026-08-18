# Phase 8 Slice 25.1: Implementation Plan

## Scope

This document is the implementation plan for Slice 25.1. It records the
decisions, dependencies, and verification path for the first slice of the
multi-region implementation.

## Decisions Made

### 1. Checkpoint Replication Transport

**Decision**: Object-storage-backed WAL

The WAL uses the existing checkpoint bucket under a new prefix:
`s3://<checkpoint-bucket>/region-replication/wal/`

WAL entry schema:
```
{
  "region": "us-east-1",
  "sequence": 12345,
  "epoch": 42,
  "checkpointUri": "s3://...",
  "timestamp": "2026-08-18T12:00:00Z",
  "crc32c": "0x12345678"
}
```

### 2. Region Topology Discovery

**Decision**: Deployment-configured ConfigMap

Helm chart values:
```yaml
multiRegion:
  enabled: true
  topology:
    configMapName: astrasync-region-topology
    configMapKey: regions.yaml
```

## Dependencies

| Dependency | ADR | Status | Notes |
|------------|-----|--------|-------|
| Control-plane mTLS | ADR-045 | Complete | Required for cross-region gRPC |
| Object-storage checkpoint | ADR-034 | Complete | Existing bucket reused |
| Helm observability | ADR-047 | Complete | Metrics wired through existing infrastructure |

## Implementation Tasks

### Phase 1: WAL Infrastructure

- [ ] Define WAL entry protobuf schema
- [ ] Implement WAL writer (primary region)
- [ ] Implement WAL consumer (secondary region)
- [ ] Add WAL metrics to observability handbook

### Phase 2: Region Topology

- [ ] Define region topology protobuf/schema
- [ ] Implement ConfigMap topology loader
- [ ] Implement endpoint discovery service
- [ ] Add topology status to API Server health endpoint

### Phase 3: Integration

- [ ] Integrate WAL with Coordinator checkpoint completion
- [ ] Integrate topology with Controller failover detection
- [ ] End-to-end test with two-region topology

## Out-of-Scope

- Auto-promotion (Slice 25.3)
- Sink capability revalidation (Slice 25.4)
- Checkpoint-coupled recovery (Slice 25.5)
- Cross-region audit query (ADR-050)

## Verification

### Functional Verification

1. **WAL append**: Write WAL entry in primary region, verify entry exists in object store
2. **WAL consume**: Start consumer in secondary region, verify entry is replicated
3. **Topology load**: Load ConfigMap, verify endpoints are parsed correctly
4. **Cross-region gRPC**: Establish mTLS channel between regions, verify replication events flow

### Non-Functional Verification

1. **Latency**: WAL append latency < 100ms at p99
2. **Throughput**: WAL consumer can keep up with 1000 entries/second
3. **Durability**: WAL entries survive object store failure

### Boundary Verification

1. **mTLS**: Cross-region channel uses existing TLS certificate
2. **Topology**: No hardcoded region endpoints
3. **Storage**: WAL uses existing checkpoint bucket, not separate bucket

## Open Questions

None. All decisions are made in the design document.

## Rollout

1. **Staging**: Deploy to staging region pair with synthetic data
2. **Read-only secondary**: Enable secondary region in read-only mode (no promotion)
3. **Manual promotion**: Enable operator-initiated promotion with explicit confirmation
4. **Auto promotion (future)**: Enable when operator confidence is established
