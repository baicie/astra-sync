# Phase 8 Slice 25.4: Implementation Plan

## Scope

This document is the implementation plan for Slice 25.4. It records
the decisions, dependencies, and verification path for sink capability
revalidation on failover.

## Decisions Made

### 1. Revalidation Trigger

**Decision**: Triggered by promotion manager (Slice 25.3)

The revalidation is triggered automatically after the epoch bump and
before the failover completes.

### 2. Timeout

**Decision**: Configurable via `multiRegion.sinkRevalidationTimeout`

Default: 60 seconds. Operator must tune for network characteristics.

### 3. Capability Check

**Decision**: Re-run full capability negotiation

The sink's transactional / idempotent capability must be confirmed
in the secondary region's network context.

## Dependencies

| Dependency | ADR | Status | Notes |
|------------|-----|--------|-------|
| Slice 25.3 | ADR-049 | Complete | Promotion manager with CapabilityRevalidator interface |
| Capability negotiation | ADR-009 | Complete | Protocol for Exactly/At-least/At-most negotiation |
| Job connection catalog | ADR-040 | Complete | Connection metadata |

## Implementation Tasks

### Phase 1: Capability Revalidator

- [ ] Implement `CapabilityRevalidator` with timeout handling
- [ ] Add sink endpoint reachability check
- [ ] Add capability negotiation protocol
- [ ] Add audit events

### Phase 2: Integration

- [ ] Wire revalidator to promotion manager
- [ ] Add metrics for revalidation duration
- [ ] Add Helm chart values
- [ ] End-to-end test with failover

## Out-of-Scope

- Capability negotiation protocol changes (ADR-009)
- Sink endpoint discovery (handled by connection catalog)

## Verification

### Functional Verification

1. **Revalidation succeeds**: Capability confirmed, failover continues
2. **Revalidation fails**: Failover aborted, audit event recorded
3. **Timeout**: Failover aborted after timeout

### Non-Functional Verification

1. **Latency**: Revalidation completes within timeout
2. **Metrics**: Duration and outcome are recorded

## Open Questions

None. All decisions are made in the design document.