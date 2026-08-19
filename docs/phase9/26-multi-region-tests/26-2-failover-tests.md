# Phase 9 Slice 26.2: Failover Integration Tests

## Status

Proposed. Implements failover integration tests for multi-region topology.

## Context

Phase 8 implemented region promotion, capability revalidation, and
checkpoint recovery. Phase 9 validates these through integration tests.

The failover integration tests verify:

1. Promotion succeeds and job is promoted to secondary region
2. Epoch is bumped and checked properly
3. Capability revalidation passes
4. Job continues executing in new region

## Decision

### Test Cases

| Test | Description |
|------|-------------|
| `TestFailover_HappyPath` | Promotion succeeds, job continues |
| `TestFailover_Idempotency` | Same idempotency key returns same result |
| `TestFailover_VersionMismatch` | Wrong expected version rejected |
| `TestFailover_CapabilityTimeout` | Timeout aborts promotion |

### Assertions

After each failover test:

- Secondary region is the active region
- Epoch is incremented
- Promotion status is `failover_complete`
- Sink capability is confirmed

## Implementation

### New File

```
tests/integration/multi-region/failover/
  failover_test.go
```

## Consequences

### Positive

- Failover semantics validated
- Idempotency guarantees verified
- Version conflict detection validated

### Negative

- Tests slower than unit tests
- Requires Docker Compose

## References

- [Phase 9 Slice 26.1 README](26-1-e2e-framework.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)