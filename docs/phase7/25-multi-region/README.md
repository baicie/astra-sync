# Phase 7 Slice 25: Multi-Region Standby, Failover, and Recovery Semantics

## Status

**Design complete and accepted.** Three ADRs are accepted (ADR-048, ADR-049,
ADR-050). Implementation code is deferred to Phase 8+ on the user's prior
guidance.

This slice closes the Phase 7 entry criterion that ADR-044
§"Phase 7 entry criteria" §3 recorded:

> Multi-region standby and failover semantics. ADR-010 constrained
> AstraSync to one active region per Job. Phase 7 lifts the
> constraint for data-plane Jobs whose source and sink are
> region-pinned, while preserving epoch fencing and durable
> desired state.

The slice is a **design deliverable, not a code deliverable.** It
ships a small set of ADRs that lock the multi-region semantics and a
single design document that enumerates the open questions a future
implementation slice must answer. The implementation itself is
deferred to a follow-on slice that depends on operational evidence
(the slice is "Phase 8+ on the user's prior guidance"; see
[§"Non-goals"](#non-goals)).

## Intended Outcomes

- An ADR that records how the control-plane PostgreSQL state and the
  durable Job record replicate across regions, so that a secondary
  region can read and assume control of the durable desired state.
- An ADR that records how the data plane fails over a region-pinned
  Job without violating ADR-006 epoch fencing, ADR-009 capability
  negotiation, or ADR-027 transactional / idempotent sink commits.
- An ADR that records how the tenant identifier (UUID, set by
  ADR-036) and the audit trail (ADR-042) survive a regional failover
  without cross-region identity replication.
- A design document that walks through the failover sequence
  (detection, epoch bump, fencing, recovery, validation, resume) and
  records the open questions a follow-on implementation slice must
  answer.
- An implementation plan that explicitly enumerates the decisions
  that must be made before code, so a future Slice 25.x
  implementation slice does not re-litigate them.

## Non-goals

- **Implementing the multi-region control plane.** This slice
  records the ADR-level decisions. The PostgreSQL cross-region
  replication topology, the gRPC inter-region channel, and the
  Scheduler HA topology are deferred.
- **Implementing region-pinned data-plane failover.** The data-plane
  Coordinator and Worker code paths stay single-region per the
  existing ADR-010 semantics. The ADR records how the failover
  boundary would look; the actual Job-level failover code is
  deferred.
- **Cross-region identity replication.** Tenant UUIDs remain
  region-local identifiers per ADR-036. A region that loses
  connection to the primary does not assume a foreign tenant's
  desired state without a separate ADR (ADR-050 records the
  decision rule).
- **Active-active multi-region.** ADR-010 is not weakened. A Job
  has one active execution epoch at a time, in one region, on one
  Kubernetes cluster.
- **Multi-region data-source or data-sink support.** Region-pinned
  sources and sinks are an ADR-level concept; the connector
  descriptor catalogue (ADR-040) stays unchanged.
- **A new observability signal.** The Phase 7 Slice 26 handbook
  records the existing metrics, log fields, and audit rows; this
  slice does not add a new SLO category.

## Boundary

This slice:

- Adds three ADRs (`adr-048-multi-region-control-plane-replication.md`,
  `adr-049-region-pinned-data-plane-failover.md`,
  `adr-050-tenant-identifier-and-audit-cross-region.md`) and a
  design document under `docs/phase7/25-multi-region/`.
- Updates the ADR index, the Phase 7 README, and ADR-044's status
  list so the entry criterion's design phase is auditable.
- Inherits Phase 6's invariant set verbatim. Every decision in the
  three ADRs must defend against weakening the seven invariants in
  `docs/phase7/README.md` §"Boundary Notes".
- Records the explicit deferral decisions in the design document so
  a follow-on slice does not silently re-scope them.

This slice does not:

- Touch any Go or Java source code. The implementation is gated on
  operational evidence and a future slice that lands the design.
- Modify any existing ADR (ADR-001 through ADR-047). The three new
  ADRs build on ADR-006, ADR-010, ADR-027, ADR-029, ADR-030,
  ADR-031, ADR-036, and ADR-042; they do not amend them.
- Add a new protobuf field, a new Kubernetes CRD field, or a new
  Helm chart value. The ADR-level contract is recorded; the schema
  change is deferred.

## Critical Invariants the Three ADRs Must Defend

1. **One active execution epoch at a time.** A Job that is
   region-pinned and fails over must keep a single monotonic epoch
   counter across the failover. ADR-006's fencing token must remain
   valid in the secondary region. (ADR-049)
2. **Source and sink capability negotiation stays real.** ADR-009
   forbids a silent fallback. A region-pinned sink must revalidate
   its transactional / idempotent capability in the secondary region
   before any sink commit. (ADR-049)
3. **Sink commit semantics do not weaken.** ADR-027's "transactional
   or idempotent" rule stays. The failover path must not start a
   sink write without a checkpoint that proves the last successful
   commit. (ADR-049)
4. **Durable desired state survives the partition.** ADR-029's
   optimistic-version Job record must be readable from the secondary
   region without a synchronous cross-region call in the steady
   state. (ADR-048)
5. **Tenant authorization is local.** ADR-036's "tenant UUID as the
   durable identifier, RBAC and authorization local to the cluster"
   rule stays. A secondary region does not inherit a foreign tenant's
   role assignments. (ADR-050)
6. **Audit trail is not split across regions.** ADR-042's audit
   query interface stays region-local. A cross-region audit query is
   a separate concern; this slice records the boundary but does not
   implement it. (ADR-050)

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../../adr/adr-049-region-pinned-data-plane-failover.md)
- [ADR-050: Tenant Identifier and Audit Cross-Region Semantics](../../adr/adr-050-tenant-identifier-and-audit-cross-region.md)
- [ADR-010: Single Active Region per Job](../../adr/adr-010-single-active-region.md)
- [ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
