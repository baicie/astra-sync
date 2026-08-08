# Phase 6 Slice 18 Implementation Plan

## Design Gate

- [x] Inventory browser, API, Controller, Scheduler, Coordinator, PostgreSQL, and Kubernetes trust
  boundaries.
- [x] Choose external OIDC, Console BFF sessions, and local PostgreSQL tenant authorization.
- [x] Define built-in roles, permissions, and all JobService method mappings.
- [x] Define audit atomicity, redaction, retention ownership, and failure behavior.
- [x] Define threat controls, rollout, rollback, and production fail-closed rules.

## 1. Authentication Domain and Persistence

- [ ] Add principal, tenant, membership, platform-role, session, and audit domain types to
  `control-plane/auth`.
- [ ] Add additive PostgreSQL migrations, restricted audit grants, and reversible application
  startup checks.
- [ ] Implement exact issuer/subject bootstrap with idempotency and last-admin protection.
- [ ] Add bounded OIDC discovery/JWKS validation with issuer, audience, algorithm, lifetime, and
  token-dimension limits.
- [ ] Test key rotation, stale-if-error, redirect rejection, malformed claims, and dependency
  failure behavior.

## 2. API Authentication and Authorization

- [ ] Add gRPC authentication, request-context, scope-resolution, and authorization interceptors.
- [ ] Forward approved bearer/request metadata through the JSON gateway without forwarding
  spoofable identity headers.
- [ ] Register all JobService methods in a startup-validated deny-by-default policy registry.
- [ ] Preserve public minimal health/readiness; disable or protect reflection in production.
- [ ] Add cross-tenant, existence-oracle, role-revocation, and unknown-method tests.

## 3. Console BFF

- [ ] Add login, callback, logout, current-session, and tenant-list routes.
- [ ] Implement Authorization Code with PKCE, state/nonce validation, opaque session cookies, token
  encryption, rotation, idle/absolute expiry, and revocation.
- [ ] Forward access tokens to the API and derive tenant selection from authorized membership.
- [ ] Treat `CONSOLE_NAMESPACE` only as an optional deployment upper bound.
- [ ] Add CSRF and same-origin foundations before Slice 19 enables mutations.

## 4. Tenant and Role Administration

- [ ] Define and generate IdentityService, AccessService, and bounded pagination contracts.
- [ ] Implement tenant creation with an initial administrator in one transaction.
- [ ] Implement membership list/grant/replace/revoke with expected authorization revision.
- [ ] Seed immutable built-in role definitions and reject custom/raw permissions.
- [ ] Add race tests for last-admin removal, concurrent grants, suspension, and revision changes.

## 5. Audit Trail

- [ ] Add the append-only event schema, monthly partition policy, and action metadata allowlists.
- [ ] Refactor Job and authorization mutations behind a unit of work that inserts required audit
  events atomically.
- [ ] Route Controller and Scheduler state transitions through fixed service actors and the same
  audited unit of work; use dedicated least-privilege database roles.
- [ ] Record authentication/session events and denied decisions synchronously.
- [ ] Add bounded AuditService queries with tenant authorization and read auditing.
- [ ] Verify rollback on audit failure, database update/delete denial, redaction sentinels, and
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
