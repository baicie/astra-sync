# Phase 6 Slice 18 Verification

## Status

Runtime foundation required by Slices 19 and 20 complete; IdentityService and AccessService
administration complete with transactional audit. Production OIDC interop, bootstrap tooling
hardening, and rollout remain tracked by the Slice 18 implementation plan.

## Design Checks

| Check | Result |
|---|---|
| Existing API, Console, Scheduler, Controller, and heartbeat trust boundaries inventoried | PASS |
| All eight JobService RPCs have explicit permission and typed namespace scope | PASS |
| All IdentityService and AccessService RPCs have explicit permission mappings | PASS |
| Human, service, and execution-capability credentials remain separate | PASS |
| Cross-tenant, CSRF, replay, cache, bootstrap, audit, and outage threats have controls | PASS |
| Principal, tenant, membership, session, revision, and audit persistence is specified | PASS |
| Mutation/audit atomicity and secret-redaction rules are specified | PASS |
| Controller and Scheduler direct-write paths have service-actor audit requirements | PASS |
| Production fail-closed and non-production compatibility behavior is specified | PASS |
| Rollout, rollback, implementation order, and acceptance criteria are specified | PASS |
| Markdown link and whitespace validation | PASS |

## Traceability

| Requirement | Design evidence |
|---|---|
| External identity without password ownership | Design Authentication section and ADR-036 |
| Tenant isolation | Tenant Model, Authorization, and threat-model namespace tampering case |
| RBAC completeness | Authorization matrix and startup descriptor inventory |
| Browser security | Console BFF session design and CSRF threat control |
| Workload isolation | Trust Topology and heartbeat cross-credential abuse case |
| Durable evidence | Audit Contract and ADR-037 |
| Standalone identity and membership administration | IdentityService and AccessService contracts in `api/protobuf/v1` |
| Membership / audit transactional integrity | `GrantTenantRole`, `RevokeTenantRole`, `GrantPlatformRole`, `RevokePlatformRole` all share `BeginTx` + audit-in-tx + `Commit` |
| Safe adoption | Rollout, production startup rules, and implementation plan |

## RPC Coverage

The policy registry maps every public gRPC method to a permission and scope function. The
generation tool and the registry completeness test fail when a method is added or removed
without a corresponding registry entry.

| Service | Methods covered | Source of truth |
|---|---|---|
| `JobService` | All eight lifecycle methods (Create, Get, List, Update, Delete, Start, Stop, GetJobStatus) | `internal/authn/interceptor.go` `NewRegistry()` |
| `JobValidationService` | `ValidateJobSpec` | same |
| `ConnectorCatalogService` | `ListConnectorDescriptors`, `GetConnectorDescriptor` | same |
| `ConnectionService` | All ten lifecycle methods (Create, Get, List, Update, Rotate, Enable, Disable, Delete, Test, GetTest) | same |
| `AuditService` | `ListAuditEvents` | same |
| `IdentityService` | `GetCurrentPrincipal`, `ListTenants` (self-scope) | same |
| `AccessService` | `ListMembers`, `GrantTenantRole`, `RevokeTenantRole`, `ListRoles`, `GrantPlatformRole`, `RevokePlatformRole` | same |

Self-scope methods (`IdentityService.*`, `AccessService.ListRoles`,
`AccessService.GrantPlatformRole`, `AccessService.RevokePlatformRole`) authorize the caller
against their own principal context without resolving a tenant scope; the service handler
performs any role-specific authorization (e.g. `principal.PlatformAdmin`) once the principal
has been authenticated.

## Transactional Audit Unit

Every membership or platform role mutation runs a single PostgreSQL transaction that commits
both the data change and the matching security audit row atomically. The new
`AccessRepository` methods take an `audit SecurityAuditEvent` parameter alongside the
mutation arguments so the call site cannot accidentally split the writes into two
transactions:

| Repository method | Data mutation | Audit row |
|---|---|---|
| `GrantTenantRole` | Membership upsert + tenant `authz_revision` bump | `access.member.granted` |
| `RevokeTenantRole` | Membership disable + tenant `authz_revision` bump | `access.member.revoked` |
| `GrantPlatformRole` | Platform-role grant/reactivation | `access.platform_role.granted` |
| `RevokePlatformRole` | Platform-role disable | `access.platform_role.revoked` |

The unit tests in `control-plane/auth/postgres/repository_integration_test.go` exercise the
full transaction sequence and assert that the audit query returns the matching events. The
unit tests in `control-plane/api-server/internal/service/access_service_test.go` assert at
the service layer that an audit failure never produces a half-applied mutation.

A `SelfScope` flag was added to the authn `Policy` struct so self-service callers do not have
to fabricate a tenant scope. The interceptor now requires the resolved principal to hold the
permission directly (via any active tenant membership, or unconditionally when
`principal.PlatformAdmin == true`). This keeps the registered method count complete while
supporting platform-level operations whose authorization happens in the service handler.

## Runtime Evidence

The Phase 6 implementation adds OIDC JWT validation, bearer authentication, tenant membership
and role resolution, deny-by-default generated-method coverage, transactional audit
persistence for membership/platform-role changes, opaque encrypted Console sessions, PKCE
login state, CSRF checks, and production fail-closed configuration. Go unit and PostgreSQL
integration suites cover identity materialization, cross-tenant authorization, token/session
expiry, login replay, mutation/audit atomicity, accessor transactions, and self-scope
authorization decisions. The end-to-end repository verification commands and results are
recorded in
[`../20-connector-catalog-connections/verification.md`](../20-connector-catalog-connections/verification.md).

No repository test can claim interoperability with a deployment's chosen identity provider or
a completed production rollout. The Slice 18 implementation plan continues to track the
operator-driven work that is intentionally outside the repository boundary.
