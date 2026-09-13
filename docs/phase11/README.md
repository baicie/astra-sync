# Phase 11: Cross-Region Disaster Recovery Drills

## Status

**Complete.** Phase 11 added a repeatable two-region outage drill and
documented the current fail-closed boundary on 2026-09-06.

## Goals

1. Exercise a primary-region outage against the real two-region Compose
   topology.
2. Verify that the secondary remains reachable and exposes replication state
   after the primary is stopped.
3. Verify that promotion and recovery reject missing prerequisites with stable
   gRPC status codes.
4. Restore the primary region and verify both transport endpoints recover.
5. Publish an operator drill template with evidence and escalation fields.

## Non-goals

- Adding protobuf fields, dependencies, storage backends, or deployment
  configuration.
- Claiming a production promotion when the Compose topology remains static.
- Claiming checkpoint recovery from a local object-store directory that does
  not contain a replicated checkpoint manifest and state files.
- Introducing automatic promotion or active-active behavior.

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 28.1 | Outage detection and recovery wait helpers | Complete |
| 28.2 | Secondary continuity and fail-closed acceptance drill | Complete |
| 28.3 | Stable gRPC error mapping | Complete |
| 28.4 | Drill design, verification record, and runbook template | Complete |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Checkpoint sequence is acknowledged before the fault | Complete |
| Primary HTTP and gRPC endpoints become unavailable | Complete |
| Secondary topology and replication status remain readable | Complete |
| Promotion rejects a secondary self-promotion attempt | Complete |
| Recovery rejects an unavailable local checkpoint prerequisite | Complete |
| Primary HTTP and gRPC endpoints recover after restart | Complete |
| Domain errors are mapped to stable, sanitized gRPC codes | Complete |

The Compose acceptance test is intentionally evidence of the current runtime
boundary. It does not report a completed promotion or recovery because the
test deployment still uses static topology, independent object-store
directories, and no real sink capability catalog.

## Records

- [Design](28-multi-region-disaster-recovery/design.md)
- [Implementation plan](28-multi-region-disaster-recovery/implementation-plan.md)
- [Verification](28-multi-region-disaster-recovery/verification.md)
- [Disaster recovery drill template](../runbooks/multi-region-disaster-recovery-drill-template.md)
- [Phase 10 README](../phase10/README.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [ADR-050: Tenant Identifier and Audit Cross-Region Semantics](../adr/adr-050-tenant-identifier-and-audit-cross-region.md)

## Architectural Boundary

The drill stops and restarts one API Server container while PostgreSQL and
the disposable Compose volumes remain owned by the test deployment. It
validates availability, replication acknowledgement, topology visibility, and
fail-closed behavior without changing the single-active-region or epoch
fencing decisions in ADR-010 and ADR-049.

## Next Delivery

Phase 12 integrates the multi-region acceptance suite into the CI pipeline
with change detection, failure diagnostics, cleanup, and immutable action
references. See the [Phase 12 README](../phase12/README.md).
