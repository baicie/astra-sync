# Phase 7: Multi-Region and Operational Maturity

Phase 7 extends the AstraSync control plane across regions and consolidates the
operational surface. It starts from the closed Phase 6 baseline and ships
strict, additive changes that preserve the existing architectural invariants
recorded by ADR-001 through ADR-043.

## Status

Design draft. No implementation has started. Phase 7 admission is recorded by
ADR-044; the entry criteria are cross-cluster control-plane mTLS, operational
runbook templates, multi-region standby semantics, and observability
consolidation.

## Goals

1. Replicate durable desired state across regions so that an active region
   loss does not require a manual recovery dance.
2. Fail over running Jobs whose source and sink are region-pinned without
   violating ADR-006 epoch fencing, ADR-009 exactly-once capability
   negotiation, or ADR-027 transactional / idempotent sink commits.
3. Bring the data-plane communication inside the cluster onto a mutual-TLS
   baseline so that future cross-cluster traffic does not require a one-off
   trust boundary per channel.
4. Unify SLF4J, zap, Prometheus, and audit signals into a single handbook so
   that an operator can derive a per-tenant SLO from a dashboard without
   cross-referencing four documentation sets.
5. Hand off IdP registration, key rotation, session revocation, audit
   retention, backup, and rollback runbooks as deployment-side templates,
   not as repository fixtures.

## Non-goals

- Replacing the existing single-region Postgres deployment with a global database.
  ADR-007's three-storage model stays intact.
- Adding a new authentication mechanism. ADR-036's external OIDC stays.
- Adding cross-region identity replication. The tenant model in ADR-036 and
  ADR-037 keeps tenant UUIDs as the durable identifier; cross-region replication
  is a database topic, not an identity topic.
- Adding fine-grained RBAC, custom roles, or per-Job ACLs. The Slice 18
  acceptance already defers these until operational evidence exists.

## Entry Criteria (ADR-044)

| Item | ADR | Required before first Phase 7 slice merges |
|---|---|---|
| Cross-cluster control-plane mTLS | ADR-043 §Consequences | yes |
| Operational runbook templates under `docs/runbooks/` | new | yes |
| Multi-region standby design with epoch fencing | new | yes |
| Observability handbook | new | yes |

## Roadmap (subject to first-slice review)

| Slice | Focus | Status |
|---|---|---|
| Slice 23 | Control-plane mutual TLS between API Server and Console / Scheduler | Design |
| Slice 24 | Operational runbook templates | Design |
| Slice 25 | Multi-region standby, failover, and recovery semantics | Design |
| Slice 26 | Observability handbook and dashboard consolidation | Design |

The first Phase 7 slice will be Slice 23 (control-plane mTLS) because the
other three entry criteria depend on having an authenticated channel between
services.

## Records

- [ADR-044: Phase 6 closeout and Phase 7 entry criteria](../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [Phase 6 closeout reference](../phase6/README.md)

## Boundary Notes

Phase 7 inherits Phase 6's invariant set verbatim:

1. Control plane and data plane stay separate ownership and failure domains.
2. Direct Pipeline stays the default topology; Durable Relay and Batch
   Materialization stay explicit choices.
3. Batch and Stream jobs share the same Job / Connector / State concepts.
4. Delivery guarantees remain based on real capability negotiation. The
   slice does not add a silent fallback.
5. Every execution path keeps a bounded batch, bounded queue, and explicit
   backpressure.
6. Coordination metadata, large state, and bulk data keep their dedicated
   storages.
7. One Job has one active execution epoch at a time. Fencing stays mandatory.

A Phase 7 slice that touches any of these invariants must ship its own ADR
before code, the same way Slice 22 did.