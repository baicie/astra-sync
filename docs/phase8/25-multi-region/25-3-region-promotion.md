# Phase 8 Slice 25.3: Operator-initiated Region Promotion

## Status

Implementation Complete (Phase 8, 2026-08-19). Implements the
operator-initiated region promotion described in
ADR-048 §"Cross-Region gRPC Channel" and ADR-049 §"Step 1: Epoch Bump
and Cross-Region Fencing" and §"Step 2: Capability Revalidation".

## Context

ADR-049 records that the promotion command is **operator-initiated**, not
automatic. The command issues a new epoch via the secondary region's etcd
cluster, writes the new epoch to the durable Job record, and bumps the
optimistic version. The new epoch is the fencing token for the data plane.

The promotion command must:
1. Authenticate the operator (RBAC role: `operator`)
2. Validate that the target region is a standby region
3. Issue a new epoch in the secondary region's etcd cluster
4. Write the new epoch to the durable Job record
5. Trigger sink capability revalidation
6. Record an audit event

## Decision

### Promotion Command

```
POST /api/v1/jobs/{job_id}/regions/{region}/promote
```

Request body:
```json
{
  "idempotency_key": "<uuid>",
  "expected_version": 42
}
```

Response:
```json
{
  "job_id": "...",
  "previous_region": "us-east-1",
  "new_region": "eu-west-1",
  "new_epoch": 43,
  "status": "promotion_in_progress"
}
```

### Promotion States

The promotion transitions through states:

| State | Description |
|-------|-------------|
| `promotion_pending` | Promotion command accepted |
| `epoch_bumped` | New epoch issued in etcd |
| `epoch_written` | Epoch written to Job record |
| `capability_revalidating` | Sink capability being revalidated |
| `capability_confirmed` | Sink capability confirmed |
| `failover_complete` | Failover complete |
| `promotion_failed` | Promotion failed with reason |

### Authorization

The promotion command requires the operator RBAC role (`role/operator`).
The role is checked at the API Server authorization layer.

### Audit Events

| Event | When |
|-------|------|
| `region-promotion-started` | Promotion command accepted |
| `region-epoch-bumped` | New epoch issued |
| `region-failover-complete` | Failover succeeded |
| `region-promotion-aborted` | Promotion failed |
| `region-write-conflict` | Optimistic version conflict |

### Failure Handling

| Failure | Behavior |
|---------|----------|
| Sink revalidation timeout | `region-promotion-aborted`; operator retries |
| Epoch conflict | `region-write-conflict`; operator resolves manually |
| Network partition | `region-promotion-aborted`; operator retries after recovery |

## Implementation

### New CLI Command

```go
// Command: astra-sync region promote
// Flags: --region, --job-id, --idempotency-key
```

### New API Endpoint

```
POST /api/v1/jobs/{job_id}/regions/{region}/promote
```

### New gRPC Service

```protobuf
service RegionPromotionService {
  rpc PromoteRegion(PromoteRegionRequest) returns (PromoteRegionResponse);
  rpc GetPromotionStatus(GetPromotionStatusRequest) returns (PromotionStatus);
}
```

### Verification

- **Functional**: Promotion command succeeds, epoch bumps, failover completes
- **Authorization**: Command rejected without operator role
- **Idempotency**: Duplicate commands return existing promotion status
- **Audit**: All events recorded with correct timestamps

## Consequences

### Positive

- Operator has explicit control over failover
- Audit trail records every promotion attempt
- Idempotency prevents duplicate promotions

### Negative

- RTO is bounded by operator response time
- Operator must understand failover implications

## References

- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [ADR-050: Tenant Identifier and Audit Cross-Region Semantics](../adr/adr-050-tenant-identifier-and-audit-cross-region.md)
- [Slice 25.2: Cross-Region gRPC Channel](25-2-cross-region-channel.md)