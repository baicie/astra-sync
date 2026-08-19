# Phase 9 Slice 26.1: Multi-Region E2E Testing Framework

## Status

Proposed. Implements the multi-region end-to-end testing framework
required by Phase 9.

## Context

Phase 8 implemented multi-region replication, failover, and recovery in
the control plane and data plane. Phase 9 validates these behaviors
through end-to-end integration tests.

The integration tests must run against a real two-region topology without
requiring actual cloud provider resources. The framework uses Docker
Compose to spin up two regional clusters.

## Decision

### Test Framework

The framework uses:

- **Docker Compose**: Two-region topology with control plane and data plane
- **Go test runner**: Test orchestration
- **Container log capture**: Failover verification
- **State assertions**: Final state verification

### Test Topology

```
Primary Region (us-east-1)        Secondary Region (us-west-1)
┌──────────────────┐                ┌──────────────────┐
│ API Server       │                │ API Server       │
│ Controller       │                │ Controller       │
│ PostgreSQL       │                │ PostgreSQL       │
│ Object Storage   │                │ Object Storage   │
└──────────────────┘                └──────────────────┘
        │                                    │
        └──────────── gRPC channel ──────────┘
```

### Test Categories

1. **Smoke**: Boot both regions, verify health
2. **Replication**: Replicate checkpoint, verify secondary has it
3. **Failover**: Promote secondary, verify primary workloads
4. **Recovery**: Restore from checkpoint, verify state

## Implementation

### New Go Packages

```
tests/integration/multi-region/
├── framework/
│   ├── framework.go      # Test framework
│   ├── topology.go       # Topology configuration
│   └── assertions.go     # State assertions
└── docker-compose.yaml   # Two-region topology
```

## Consequences

### Positive

- Real multi-region validation
- Reproducible failures
- CI feedback

### Negative

- Tests slower than unit tests
- Requires Docker

## References

- [Phase 9 README](../README.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)