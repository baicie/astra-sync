# ADR-050: Tenant Identifier and Audit Cross-Region Semantics

## Status

Proposed (Phase 7 Slice 25 design cluster)

## Context

ADR-036 records that the tenant identifier is a UUID issued by the
control plane on first registration, that authentication is
delegated to an external OIDC provider, and that tenant
authorization (RBAC, role assignments, membership) is local to the
cluster. ADR-042 records that every authenticated mutation writes
a row to the `audit_events` table and that the audit query
interface is tenant-scoped.

ADR-048 records that the audit table is one of the tables that the
multi-region PostgreSQL replication channel replicates to the
secondary region. The replication is asynchronous, so a secondary
region's audit query may see events that the primary region has
not yet committed.

Without an explicit cross-region model, a multi-region deployment
faces three questions:

1. Does the secondary region assume a foreign tenant's
   authorization on failover?
2. Does the secondary region accept audit events that the primary
   region has not yet committed?
3. Does a cross-region audit query exist, and if so, what is its
   read-consistency contract?

The first question is the most dangerous. A naive implementation
that assumes foreign authorization on failover would silently
extend a tenant's RBAC scope across regions, violating ADR-036.

## Decision

The multi-region tenant and audit model is **region-local with
explicit cross-region rejection**:

### Tenant Authorization is Region-local

A region does not inherit a foreign tenant's authorization. The
secondary region's API Server, on failover, requires the operator
to explicitly grant the foreign tenant's RBAC role in the
secondary region before the secondary region accepts the tenant's
requests. The grant is a desired-state mutation that requires the
operator's RBAC role (ADR-036) and that writes a new audit event
of type `region-tenant-rbac-granted`.

The rule preserves ADR-036's "tenant authorization is local to the
cluster" boundary. A tenant whose Job is region-pinned to region A
is not automatically authorized to operate in region B. The
operator must opt in by issuing an explicit RBAC grant.

The rule is enforced at the API Server's authorization layer, not
at the data plane. The data plane does not know about regions; the
data plane executes the Job in the active region under the
operator-granted authorization.

### Audit Events are Region-local

The audit table is replicated to the secondary region (ADR-048 §
"Coordinating metadata replication"), but the secondary region's
audit query interface serves only the events the secondary region
has durably replicated. A query that asks for events outside the
secondary region's replicated window is rejected with an error of
type `audit-query-cross-region-not-supported`. The error code is
stable; the audit explorer UI surfaces the error to the operator.

The rule preserves ADR-042's "audit query interface is
tenant-scoped" boundary. A region-local query is well-defined; a
cross-region query is not implemented in this slice. A future slice
that wants a cross-region audit query must define the read-
consistency contract (strong vs eventual) and the tenant
authorization boundary for the cross-region query, and must
amend ADR-042.

### Audit Events are Not Split Across Regions on Failover

A failover does not split the audit trail. The primary region's
audit table is replicated to the secondary region before the
failover; after the failover, the secondary region's audit table
continues to record new events in the secondary region's local
table. The replication is one-way: the primary region does not
pull the secondary region's events.

The rule preserves ADR-042's "audit query interface is
tenant-scoped" boundary. A tenant that queries the secondary
region's audit table sees the secondary region's view of the
tenant's history; a tenant that queries the primary region's
audit table sees the primary region's view. The two views may
diverge during the failover window; the divergence is recorded
as an audit event of type `region-audit-divergence` and is
resolved by the operator.

### Cross-Region Audit Query is Not in Scope

A cross-region audit query is a separate concern. The slice
records the boundary but does not implement the query. A future
slice that wants a cross-region audit query must define:

- The read-consistency contract (strong vs eventual).
- The tenant authorization boundary (the tenant that is
  authorized to query the primary region's audit table is not
  automatically authorized to query the secondary region's audit
  table).
- The audit event type for the cross-region query.

The future slice must amend ADR-042 and must record a new ADR
that links to ADR-050.

## Consequences

### Positive

- The region-local authorization rule preserves ADR-036's
  boundary. A tenant's RBAC scope does not silently extend across
  regions.
- The region-local audit query interface preserves ADR-042's
  boundary. A region-local query is well-defined; a cross-region
  query is explicitly out of scope.
- The one-way audit replication is consistent with the one-way
  control-plane replication (ADR-048). The secondary region is a
  consumer of the primary region's audit events, not a producer.
- The rule is enforced at the API Server's authorization layer.
  The data plane is unchanged.

### Negative

- The operator must explicitly grant a foreign tenant's RBAC role
  in the secondary region on failover. The grant is a
  desired-state mutation that requires the operator's RBAC role.
- The audit query interface returns `audit-query-cross-region-
  not-supported` for cross-region queries. The implementation
  slice must record the error code in the audit explorer's
  contract (ADR-042's "Audit explorer" section).
- The audit trail may diverge during the failover window. The
  divergence is recorded as an audit event; the operator resolves
  the divergence manually.

## References

- [ADR-036: External OIDC and Local Tenant Authorization](adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](adr-037-transactional-control-plane-audit-trail.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](adr-042-tenant-scoped-audited-security-event-queries.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](adr-048-multi-region-control-plane-replication.md)
