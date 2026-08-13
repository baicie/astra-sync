# Slice 18.5 Transactional Audit Unit

## Purpose

Every membership and platform-role mutation has to leave the system in a state that can
be proven from the audit trail. The Slice 18.5 unit of work folds the data mutation and
its security audit row into one PostgreSQL transaction so a denied, cancelled, or crashed
request cannot leave the platform with a granted membership and no audit evidence.

The audit row is written from the same connection and transaction as the data change,
using the same actor identity, request ID, and timestamp. The repository method is the
only call site that performs the audit write — the service layer passes the
`SecurityAuditEvent` next to the mutation arguments so the call site cannot accidentally
split the writes into two transactions.

## Mutation Surface

The `auth.AccessRepository` interface owns the four mutating operations. Each takes a
`SecurityAuditEvent` parameter that the service layer is responsible for assembling from
the request context:

| Repository method | Data mutation | Audit event type |
|---|---|---|
| `GrantTenantRole` | Upserts the membership row to `ACTIVE` and bumps `authz_revision` | `access.member.granted` |
| `RevokeTenantRole` | Marks the membership row `INACTIVE` and bumps `authz_revision` | `access.member.revoked` |
| `GrantPlatformRole` | Inserts or reactivates a `astrasync_auth_platform_roles` row | `access.platform_role.granted` |
| `RevokePlatformRole` | Marks the platform role row `INACTIVE` | `access.platform_role.revoked` |

The helper `writeAuditInTx` performs the audit insert. It encodes the JSON `Attributes`
field, handles a nullable tenant UUID, and translates the PostgreSQL `23505` unique
violation into the existing "security audit event already exists" error so a duplicated
UUID is reported identically to the legacy `WriteSecurityAudit` path.

## Repository Flow

1. `BeginTx` opens a `sql.Tx` with the standard options.
2. The repository re-reads the tenant (`readTenantInTx`) or platform scope inside the
   transaction so a concurrent suspension / disable invalidates the mutation with a
   fresh view rather than a stale snapshot.
3. The repository performs the membership / platform-role mutation through the same
   transaction.
4. `writeAuditInTx` writes the security audit row inside the same transaction.
5. `Commit` releases both writes atomically. Any failure in steps 1-4 aborts the
   transaction and the database rolls back to the pre-mutation snapshot.

The repository never falls back to a non-transactional write path. The audit row cannot
exist without the data mutation, and the data mutation cannot commit without the audit
row.

## Service Layer Responsibilities

`AccessService` is responsible for:

- Constructing the `SecurityAuditEvent` (event ID, event type, actor ID, tenant ID,
  request ID, outcome, attributes, occurred-at) before the repository call.
- Returning the typed gRPC errors so the audit row reflects the eventual decision.
- Calling the repository method exactly once per mutation; the repository never writes
  an audit row by itself outside of the transactional helpers.

The unit tests in `control-plane/api-server/internal/service/access_service_test.go` use
a fake repository that exposes a configurable audit error. When `auditErr` is set, the
fake returns the same error from its `GrantTenantRole` / `RevokeTenantRole` /
`GrantPlatformRole` / `RevokePlatformRole` implementations, and the service tests assert
that no membership or platform-role mutation is recorded in the fake's own state. This
mirrors the PostgreSQL behavior at the SQL boundary: a failure inside the transaction
rolls back both halves of the unit of work.

## Self-Scope Authorization

The four membership mutations stay tenant-scoped; their authorization policies still
require an active membership in the target tenant plus the `members.manage` permission.
The two platform-role variants and the read-side RPCs (`GetCurrentPrincipal`,
`ListTenants`, `ListRoles`) are self-scoped and skip the tenant membership lookup in the
interceptor.

A new `SelfScope` field on `authn.Policy` flags those methods. When set, the interceptor
bypasses `Principal.MembershipForScope` and uses a `principalHasPermission` helper that
returns true when any active tenant membership grants the permission, or unconditionally
when the principal holds `platform_admin`. The platform role grant/revoke handlers then
perform their own platform-admin check before mutating state, since the interceptor has
already accepted the request.

## Tests

| Test | Evidence |
|---|---|
| `postgres.TestRepositoryAccessMutationsAreTransactional` | End-to-end PostgreSQL transaction; asserts the audit row is visible after each mutation. |
| `service.TestAccessServiceTransactionIsAtomicOnAuditFailure` | Fake repository returns an audit error and the service test asserts no membership row was modified. |
| `service.TestAccessServiceTransactionEmitsExactlyOneAuditPerMutation` | Fake repository records the audit events it receives and the service test asserts exactly one per mutation. |
| `authn.TestInterceptorSelfScopeAcceptsCallerWithMatchingPermission` | A principal with the required permission is allowed through to the handler. |
| `authn.TestInterceptorSelfScopeRejectsCallerWithoutPermission` | A principal without the permission is denied even on a self-scoped method. |

## Rollout

The transactional mutation paths are enabled as soon as the deployment upgrades the
control plane. Operators do not need to flip a feature gate for the membership /
platform-role mutations; the existing `WriteSecurityAudit` write path is preserved for
legacy audit emitters so they continue to function during the upgrade window.

The PostgreSQL schema requires no new tables. The `astrasync_security_audit_events`
table is already the append-only audit store from Slice 18.1; this slice only changes
how the writer is invoked.
