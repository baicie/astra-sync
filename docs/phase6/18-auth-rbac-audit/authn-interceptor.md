# Slice 18.2 API Authorization Interceptor and Policy Registry

## Purpose

Every public gRPC method exposed by the API Server runs through a single
unary interceptor that performs authentication, principal resolution, tenant
scope resolution, permission mapping, and authorization in that order. The
interceptor owns a startup-validated deny-by-default policy registry and never
invokes a service handler until every check has succeeded.

## Source-of-truth registry

`control-plane/api-server/internal/authn.Registry` declares one
`Policy{Permission, Scope, ResolvePermission}` per fully qualified method
constant generated from the Slice 19/20 protobuf files. The constructor seeds
the registry from the generated `JobService_*`, `JobValidationService_*`,
`ConnectorCatalogService_*`, `ConnectionService_*`, and `AuditService_*`
constants, and `ValidateServices(services...)` cross-checks every entry
against the registered gRPC service descriptors. A failure to register or
over-register any method causes `ValidateServices` to return an error so
process startup fails closed.

### Public method whitelisting

`Registry.PublicMethod(fullMethod)` declares one method as reachable without
authentication. The interceptor short-circuits the policy lookup and audit
emission for whitelisted methods, which are limited to the gRPC health probe
contract and operator-managed diagnostics endpoints. Whitelisted methods are
still required to be reachable over TLS.

## Authorization pipeline

1. The interceptor reads the `authorization: Bearer <token>` metadata, the
   `x-request-id` correlation header, and any other trusted-proxy supplied
   fields. Header length and shape are bounded.
2. `Authenticator.Authenticate` validates the bearer token against the
   configured OIDC validator and materializes an `auth.Principal`.
3. `Policy.Scope(request)` extracts the tenant scope from the typed request
   message. `Policy.Permission(request)` is either a static field on the
   policy or a request-derived resolver (e.g. `ValidateJobSpec` chooses the
   permission based on `JobValidationPurpose`). Methods marked `SelfScope`
   skip the tenant-scope step and authorize the caller against their own
   principal context (used by `IdentityService.*`, `AccessService.ListRoles`,
   `AccessService.GrantPlatformRole`, and `AccessService.RevokePlatformRole`).
4. `Principal.MembershipForScope(scope)` locates the membership row for the
   extracted tenant namespace. A missing or inactive membership produces
   `PERMISSION_DENIED` and a synchronous `authorization.denied` audit row.
   `SelfScope` methods instead resolve the required permission directly from
   the principal — any active tenant membership that already grants the
   permission, or unconditionally when the principal holds `platform_admin`.
5. `Authorizer.Authorize(ctx, tenantID, permission)` resolves the
   permission, active status, and policy revision; a stale revision returns
   `POLICY_STALE` and a denied audit row. Self-scope services that need
   platform-level checks (e.g. platform role grants) perform their own
   platform-admin check inside the handler before returning success.

Only after all five steps succeed does the interceptor forward the request
to the registered service handler. The principal is stored on the context so
downstream repositories can resolve the actor and write audit rows without
re-authenticating.

## Defense in depth

Repositories that mutate tenant-scoped state accept an authorization context
containing the principal, tenant UUID, permission, request ID, and policy
revision. The interceptor is the only call site; repositories refuse direct
invocations when the context is absent. This guarantees that audit emission
is impossible to bypass at the SQL boundary.

## Health, reflection, and diagnostics

- `grpc.health.v1.Health/Check` and `Watch` are not registered in production.
  The HTTP `/health` and `/ready` endpoints exposed by the API Server cover
  external probes and require no gRPC traffic.
- gRPC reflection is only enabled when `environment != "production"`. The
  deployment refuses `environment=production` together with
  `reflection.enabled=true` to prevent unauthenticated introspection of the
  descriptor inventory.
- The protected diagnostics permission (`diagnostics.read`) is the only path
  to opt-in RPCs that intentionally expose platform internals. The
  permission is mapped to `platform_admin` and is never granted to a tenant
  role.

## Error semantics

| Failure | gRPC code | Audit outcome |
|---|---|---|
| Missing or malformed bearer token | `UNAUTHENTICATED` | `UNAUTHENTICATED` |
| Unmapped method (no policy) | `PERMISSION_DENIED` | `UNMAPPED_METHOD` |
| Missing tenant scope in request | `INVALID_ARGUMENT` | `INVALID_SCOPE` |
| Unresolvable purpose (validation RPC) | `INVALID_ARGUMENT` | `INVALID_POLICY_INPUT` |
| Cross-tenant or missing membership | `PERMISSION_DENIED` | `TENANT_DENIED` |
| Policy revision stale | `PERMISSION_DENIED` | `POLICY_STALE` |
| Authenticated but lacks permission | `PERMISSION_DENIED` | `PERMISSION_DENIED` |

The audit emission is synchronous and uses a detached context so a slow audit
write cannot delay the gRPC deadline. The application database role has no
`UPDATE` permission on `astrasync_security_audit_events`.

## Verification

| Check | Command or scope | Result |
|---|---|---|
| Registry completeness | `authn.TestRegistryCoversEveryPublicMethodExactly` | PASS |
| Whitelisted method short-circuit | `authn.TestRegistryPublicMethodSkipsAuthorization` | PASS |
| Request ID propagation | `authn.TestRequestIDFromContextPropagatesAuditLabel` | PASS |
| Cross-tenant denial before handler | `authn.TestInterceptorDeniesCrossTenantBeforeHandlerAndAuditsSynchronously` | PASS |
| Authorization cache revival | `authn.TestInterceptorAuthenticatesResolvesNamespaceAndAuthorizesBeforeHandler` | PASS |
| Self-scope accept with matching permission | `authn.TestInterceptorSelfScopeAcceptsCallerWithMatchingPermission` | PASS |
| Self-scope reject without permission | `authn.TestInterceptorSelfScopeRejectsCallerWithoutPermission` | PASS |
| Service-descriptor cross-check | `authn.Registry.ValidateServices` on every startup | PASS |

## Failure Semantics Summary

- The interceptor never returns success without a successful authorization
  decision; the handler is unreachable otherwise.
- A repository that mutates tenant-scoped state fails closed when its
  authorization context is missing, even if called from within a service
  handler.
- A denied audit write is logged at high severity but cannot convert a denial
  into an allow.
