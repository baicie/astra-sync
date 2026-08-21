# Phase 8 Slice 25.4: Sink Capability Revalidation on Failover

## Status

Implementation Complete (Phase 8, 2026-08-19). Implements the sink capability
revalidation described in
ADR-049 §"Step 2: Capability Revalidation".

## Context

ADR-009 records that delivery guarantees (Exactly/At-least/At-most) must be
based on real capability negotiation. Before a secondary region's Coordinator
issues its first sink write, the Coordinator must re-run the capability
negotiation against the sink.

ADR-049 records that the revalidation is not optional and is bounded by
`sinkRevalidationTimeout` (default 60 seconds). A revalidation that times
out aborts the failover and surfaces an audit event of type
`region-promotion-aborted`.

Slice 25.3 implemented the promotion manager with the
`CapabilityRevalidator` interface. Slice 25.4 implements the actual
capability revalidation against the sink.

## Decision

### Capability Revalidation Flow

1. **Pre-check**: Verify sink endpoint is reachable from secondary region
2. **Negotiation**: Re-run capability negotiation protocol (ADR-009)
3. **Validation**: Confirm sink's transactional / idempotent capability
4. **Decision**: If capability confirmed, continue; if not, abort failover

### Timeout Handling

The revalidation timeout is configurable via Helm chart value
`multiRegion.sinkRevalidationTimeout` (default 60 seconds).

| Timeout | Behavior |
|---------|----------|
| < 30s | Aggressive; may fail on slow networks |
| 30-60s | Balanced; recommended for most deployments |
| > 60s | Conservative; use for high-latency links |

### Audit Events

| Event | When |
|-------|------|
| `region-capability-revalidation-started` | Revalidation initiated |
| `region-capability-confirmed` | Revalidation succeeded |
| `region-capability-rejected` | Revalidation failed |
| `region-promotion-aborted` | Revalidation timed out |

## Implementation

### Interface Extension

The existing `CapabilityRevalidator` interface in Slice 25.3 is extended:

```go
type CapabilityRevalidator interface {
    Revalidate(ctx context.Context, jobID string, timeout time.Duration) error
}
```

### New Implementation

```
control-plane/
  replication/
    capability/
      revalidator.go     # Capability revalidator implementation
```

## Consequences

### Positive

- Sink capability is re-confirmed in secondary region's network context
- No silent fallback to degraded capability
- Audit trail records revalidation outcome

### Negative

- Revalidation adds latency to failover
- Timeout must be tuned for network characteristics
- Sink must be reachable from secondary region

## References

- [ADR-009: Exactly-Once via Capability Negotiation](../adr/adr-009-capability-negotiation.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [Slice 25.3: Region Promotion](25-3-region-promotion.md)