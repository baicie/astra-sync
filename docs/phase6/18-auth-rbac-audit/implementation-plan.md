# Phase 6 Slice 18 Implementation Plan

## Design Gate

- [x] Inventory browser, API, Controller, Scheduler, Coordinator, PostgreSQL, and Kubernetes trust
  boundaries.
- [x] Choose external OIDC, Console BFF sessions, and local PostgreSQL tenant authorization.
- [x] Define built-in roles, permissions, and all JobService method mappings.
- [x] Define audit atomicity, redaction, retention ownership, and failure behavior.
- [x] Define threat controls, rollout, rollback, and production fail-closed rules.

## 1. Authentication Domain and Persistence

- [x] Add principal, tenant, membership, platform-role, session, and audit domain types to
  `control-plane/auth`.
- [x] Add additive PostgreSQL migrations, restricted audit grants, and reversible application
  startup checks.
- [x] Implement exact issuer/subject bootstrap with idempotency and last-admin protection.
- [x] Add bounded OIDC discovery/JWKS validation with issuer, audience, algorithm, lifetime, and
  token-dimension limits.
- [x] Test key rotation, stale-if-error, redirect rejection, malformed claims, and dependency
  failure behavior.

## 2. API Authentication and Authorization

- [x] Add gRPC authentication, request-context, scope-resolution, and authorization interceptors.
- [x] Forward approved bearer/request metadata through the JSON gateway without forwarding
  spoofable identity headers.
- [x] Register all JobService methods in a startup-validated deny-by-default policy registry.
- [x] Preserve public minimal health/readiness; disable or protect reflection in production.
- [x] Register IdentityService and AccessService methods (self-scope for caller-safe reads and
  platform role grants, tenant-scoped membership mutators) and extend the registry-completeness
  test.
- [x] Add cross-tenant, existence-oracle, role-revocation, and unknown-method tests.

## 3. Console BFF

- [x] Add login, callback, logout, current-session, and tenant-list routes.
- [x] Implement Authorization Code with PKCE, state/nonce validation, opaque session cookies, token
  encryption, rotation, idle/absolute expiry, and revocation.
- [x] Forward access tokens to the API and derive tenant selection from authorized membership.
- [x] Treat `CONSOLE_NAMESPACE` only as an optional deployment upper bound.
- [x] Add CSRF and same-origin foundations before Slice 19 enables mutations.

## 4. Tenant and Role Administration

- [x] Define and generate IdentityService, AccessService, and bounded pagination contracts.
- [x] Implement tenant creation with an initial administrator in one transaction.
- [x] Implement membership list/grant/replace/revoke with expected authorization revision.
- [x] Seed immutable built-in role definitions and reject custom/raw permissions.
- [x] Combine every membership / platform-role mutation with its security audit row in a single
  PostgreSQL transaction (`AccessRepository.GrantTenantRole`, `RevokeTenantRole`,
  `GrantPlatformRole`, `RevokePlatformRole`). The auth package interface now passes the
  `SecurityAuditEvent` alongside the mutation arguments so the call site cannot split the
  writes.
- [x] Add race tests for last-admin removal, concurrent grants, suspension, and revision changes.

## 5. Audit Trail

- [x] Add the append-only event schema, monthly partition policy, and action metadata allowlists.
- [x] Refactor Job and authorization mutations behind a unit of work that inserts required audit
  events atomically.
- [x] Route Controller and Scheduler state transitions through fixed service actors and the same
  audited unit of work; use dedicated least-privilege database roles.
- [x] Record authentication/session events and denied decisions synchronously.
- [x] Add bounded AuditService queries with tenant authorization and read auditing.
- [x] Verify rollback on audit failure, database update/delete denial, redaction sentinels, and
  retention operations.

## 6. Transport, Deployment, and Rollout

- [ ] Add production TLS requirements, trusted-proxy validation, secret references, and startup
  configuration checks to API Server, Console, Docker, and Helm.
- [ ] Add non-production shadow authentication and would-deny metrics.
- [ ] Document IdP registration, bootstrap, key rotation, role recovery, session revocation, audit
  retention, backup, and rollback runbooks.
- [ ] Run unit, PostgreSQL integration, browser, cross-service, Helm, image, and security-negative
  suites in CI.
- [ ] Enable enforcement in staging before production and remove bootstrap configuration.

## Delivery Slices

Implementation should be submitted as reviewable commits or sub-slices in this order:

1. Auth domain, schema, OIDC validator, and policy registry.
2. API interceptors and JobService enforcement.
3. Console BFF sessions and tenant discovery.
4. Role administration and transactional audit.
5. TLS/deployment hardening, migration runbooks, and end-to-end verification.
