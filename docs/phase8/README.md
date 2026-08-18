# Phase 8: Multi-Region Implementation

Phase 8 implements the multi-region standby, failover, and recovery
semantics designed in Phase 7 Slice 25 (ADR-048, ADR-049, ADR-050).

## Status

**In Progress.** This phase is gated on the Phase 7 Slice 25 design
acceptance (ADR-048/049/050 accepted 2026-08-18).

## Goals

1. Implement asynchronous PostgreSQL replication topology for control-plane
   state across regions.
2. Implement cross-region gRPC channel for checkpoint replication using the
   Phase 7 Slice 23 mTLS boundary.
3. Implement operator-initiated region promotion command with epoch bump
   and fencing.
4. Implement sink capability revalidation on failover.
5. Implement checkpoint-coupled recovery in the secondary region.
6. Add multi-region operational runbook template under `docs/runbooks/`.

## Non-goals

- **Auto-promotion.** The failover is operator-initiated per ADR-010.
- **Cross-region audit query.** The audit query interface stays region-local
  per ADR-050.
- **Active-active multi-region.** ADR-010 is not weakened.
- **Cross-region identity replication.** ADR-036 is not weakened.
- **New observability signal.** The Phase 7 Slice 26 handbook records the
  existing metrics; this phase adds multi-region metrics only if required
  by the implementation.

## Entry Criteria (ADR-044 Phase 7 Entry Criteria)

All Phase 7 entry criteria are satisfied:

| Item | ADR | Status |
|------|-----|--------|
| Cross-cluster control-plane mTLS | ADR-045 | Complete |
| Operational runbook templates | ADR-046 | Complete |
| Multi-region standby design | ADR-048/049/050 | Design Accepted |

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| Slice 25.1 | PostgreSQL replication topology and region topology discovery | Pending |
| Slice 25.2 | Cross-region gRPC channel with mTLS | Pending |
| Slice 25.3 | Operator-initiated region promotion with epoch bump | Pending |
| Slice 25.4 | Sink capability revalidation on failover | Pending |
| Slice 25.5 | Checkpoint-coupled recovery | Pending |
| Slice 25.6 | Multi-region runbook template | Pending |

## Open Decisions (from Slice 25 Design)

The implementation must answer the following before code lands:

1. **Checkpoint replication transport.** PostgreSQL logical replication,
   object-storage-backed write-ahead log, or Kubernetes-native CSI snapshot.
2. **Region topology discovery.** Kubernetes `MultiClusterService`,
   Consul-style service mesh, or deployment-configured endpoint list.
3. **Auto-promotion policy.** Whether auto-promotion is in scope and, if so,
   what RBAC role authorizes it.
4. **Region-pinned connector descriptor metadata.** Whether to add a
   `regionAffinity` field to the connector descriptor catalogue.

## Dependencies

- Phase 7 Slice 23 (control-plane mTLS) — ADR-045
- Phase 7 Slice 24 (operational runbook templates) — ADR-046
- Phase 7 Slice 26 (observability handbook) — ADR-047
- Phase 6 Slice 22 (transport hardening) — ADR-043

## Records

- [Phase 8 Slice 25.1 README](25-multi-region/25-1-replication-topology.md)
- [Phase 8 Slice 25.1 Implementation Plan](25-multi-region/25-1-implementation-plan.md)
- [Phase 7 Slice 25 design cluster](../phase7/25-multi-region/README.md)
- [Phase 7 Slice 25 design](../phase7/25-multi-region/design.md)
- [Phase 7 Slice 25 implementation plan](../phase7/25-multi-region/implementation-plan.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [ADR-050: Tenant Identifier and Audit Cross-Region Semantics](../adr/adr-050-tenant-identifier-and-audit-cross-region.md)

## Boundary Notes

Phase 8 inherits all Phase 7 invariant set verbatim:

1. Control plane and data plane stay separate ownership and failure domains.
2. Direct Pipeline stays the default topology; Durable Relay and Batch
   Materialization stay explicit choices.
3. Batch and Stream jobs share the same Job / Connector / State concepts.
4. Delivery guarantees remain based on real capability negotiation. The
   phase does not add a silent fallback.
5. Every execution path keeps a bounded batch, bounded queue, and explicit
   backpressure.
6. Coordination metadata, large state, and bulk data keep their dedicated
   storages.
7. One Job has one active execution epoch at a time. Fencing stays mandatory.
