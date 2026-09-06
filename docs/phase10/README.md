# Phase 10: Multi-Region Observability Integration

## Status

**Complete.** Phase 10 integrated the existing multi-region replication
metrics with the API Server exposition path and documented the operator
queries on 2026-09-06.

## Goals

1. Verify promotion, checkpoint event, and recovery samples through one
   API Server metrics registry.
2. Preserve bounded labels and explicit success/failure outcomes.
3. Keep the metrics endpoint isolated, optional, and shut down with the
   owning process.
4. Make the live multi-region metric families discoverable in the handbook.

## Non-goals

- Adding protobuf fields, new dependencies, or a storage backend.
- Changing promotion, replication, recovery, or epoch-fencing semantics.
- Shipping a deployment-owned Prometheus rule file or Grafana dashboard.

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 27.1 | API Server business-path scrape coverage | Complete |
| 27.2 | Metrics catalog and dashboard recipes | Complete |
| 27.3 | Verification and closeout records | Complete |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Promotion sample reaches API Server `/metrics` | ✅ |
| Checkpoint event sample reaches API Server `/metrics` | ✅ |
| Recovery sample reaches API Server `/metrics` | ✅ |
| Empty labels normalize to `_unknown` | ✅ |
| Success and failure outcomes remain bounded | ✅ |
| Metrics listener remains optional and closable | ✅ |
| Go tests, checks, and script tests pass | ✅ |

## Records

- [Design](27-multi-region-observability/design.md)
- [Implementation plan](27-multi-region-observability/implementation-plan.md)
- [Verification](27-multi-region-observability/verification.md)
- [Observability handbook](../observability/README.md)
- [Phase 9 closeout](../phase9/closeout.md)

## Architectural Boundary

This phase reuses the `replicationmetrics.Bundle` registry and recorder
already owned by the API Server process. It does not alter any architecture
invariant or ADR decision. The existing replication services remain the
owners of business observations; the test proves that their shared registry
is exposed by the API Server endpoint.

## Next Delivery

Phase 11 completed the bounded [cross-region disaster recovery drill](../phase11/README.md),
and Phase 12 completed the [CI pipeline integration](../phase12/README.md)
for the multi-region acceptance suite.
