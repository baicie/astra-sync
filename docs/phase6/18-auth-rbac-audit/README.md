# Phase 6 Slice 18: Authentication, Tenant RBAC, and Audit

## Status

Runtime foundation required by Slices 19 and 20 complete; standalone tenant and role
administration API also implemented with transactional audit coupling. Production OIDC
interoperability, transport hardening, and rollout remain operator-controlled gates. The
Console BFF session boundary is complete at repository level.

Slice 18 establishes the security boundary used by Console and API mutations. It delegates user
authentication to an external OIDC provider, keeps AstraSync roles in PostgreSQL, authorizes public
RPCs at the API Server, and makes control-plane mutations auditable.

## Design Outcomes

- OIDC Authorization Code with PKCE for the Console BFF and bearer tokens for CLI/API clients.
- Opaque, server-side Console sessions with secure cookie and CSRF requirements.
- Existing Job namespace as the tenant resource scope, backed by explicit tenant membership.
- Deny-by-default method and permission mapping for every public RPC (JobService,
  JobValidationService, ConnectorCatalogService, ConnectionService, AuditService,
  IdentityService, AccessService).
- Self-scope authorization for caller-safe reads and platform role grants, supporting
  `IdentityService` and the platform role variants of `AccessService`.
- Separate user, service, and execution-capability credential domains.
- Append-only PostgreSQL audit records transactionally coupled to state changes — including
  the new membership and platform-role mutations, which commit the data change and the
  matching audit row in a single PostgreSQL transaction.
- Guarded development compatibility and fail-closed production startup rules.

## Records

- [Design](design.md)
- [Threat model](threat-model.md)
- [Authorization matrix](authorization-matrix.md)
- [Implementation plan](implementation-plan.md)
- [Design verification](verification.md)
- [Admin runbook](admin-runbook.md)
- [BFF tests](bff-tests.md)
- [Authn interceptor notes](authn-interceptor.md)
- [Transactional audit unit](transactional-audit.md)
- [ADR-036: External OIDC and Local Tenant Authorization](../../adr/adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](../../adr/adr-037-transactional-control-plane-audit-trail.md)

## Rollout Gate

The delivered foundation preserves the existing lifecycle version and execution-epoch fences. RBAC is
an additional admission check; it does not replace optimistic versions, heartbeat capability
tokens, or Kubernetes service-account boundaries. Production requires OIDC, TLS, session keys,
tenant memberships, audit retention, and fail-closed configuration owned by the deployment. The
remaining TLS and rollout work is listed in [implementation-plan.md](implementation-plan.md).
