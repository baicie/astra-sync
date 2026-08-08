# Phase 6 Slice 18: Authentication, Tenant RBAC, and Audit

## Status

Design complete; implementation not started.

Slice 18 establishes the security boundary required before the Console can expose Job mutations or
browser-selectable tenants. It delegates user authentication to an external OIDC provider, keeps
AstraSync roles in PostgreSQL, authorizes every Job RPC at the API Server, and makes control-plane
mutations auditable.

## Design Outcomes

- OIDC Authorization Code with PKCE for the Console BFF and bearer tokens for CLI/API clients.
- Opaque, server-side Console sessions with secure cookie and CSRF requirements.
- Existing Job namespace as the tenant resource scope, backed by explicit tenant membership.
- Deny-by-default method and permission mapping for all eight JobService RPCs.
- Separate user, service, and execution-capability credential domains.
- Append-only PostgreSQL audit records transactionally coupled to state changes.
- Guarded development compatibility and fail-closed production startup rules.

## Records

- [Design](design.md)
- [Threat model](threat-model.md)
- [Authorization matrix](authorization-matrix.md)
- [Implementation plan](implementation-plan.md)
- [Design verification](verification.md)
- [ADR-036: External OIDC and Local Tenant Authorization](../../adr/adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](../../adr/adr-037-transactional-control-plane-audit-trail.md)

## Implementation Gate

Implementation must preserve the existing lifecycle version and execution-epoch fences. RBAC is an
additional admission check; it cannot replace optimistic versions, heartbeat capability tokens, or
Kubernetes service-account boundaries.
