# Phase 6 Slice 18 Design: Authentication, Tenant RBAC, and Audit

## Status

Implementation-ready design. No authentication or authorization runtime is delivered by this
document alone.

## Goals

1. Authenticate browser, CLI, and direct API callers through a standards-based external identity
   provider.
2. Resolve stable AstraSync principals and tenant memberships without trusting provider-specific
   role claims.
3. Enforce one explicit permission for every public control-plane RPC before domain code runs.
4. Preserve namespace isolation independently at the Console and API Server boundaries.
5. Record security-relevant decisions and state changes without leaking connector credentials.
6. Preserve existing Job versions, execution epochs, Scheduler heartbeat tokens, and Kubernetes
   service-account boundaries.

## Non-goals

- Password storage, signup, account recovery, MFA implementation, or an AstraSync identity provider.
- Fine-grained field, row, connector-option, or per-Job ACLs.
- Replacing Kubernetes RBAC, database credentials, or execution heartbeat capability tokens.
- Connector secret management, secret rotation, or data-plane mTLS identity.
- Cross-region identity replication, external policy engines, SCIM, or SIEM export.
- Job mutation UI; Slice 18 supplies the permission boundary used by Slice 19.

## Terms

| Term | Meaning |
|---|---|
| Principal | Stable AstraSync identity keyed by OIDC issuer and subject |
| Tenant | Security boundary whose slug is the existing Job namespace |
| Membership | Assignment of one built-in role to one principal in one tenant |
| Platform role | Explicit global assignment for platform-wide administration |
| Permission | Stable action string checked by the API, such as `jobs.read` |
| Session | Opaque Console browser session stored server-side |
| Workload capability | Narrow machine token valid for one endpoint and execution identity |

## Trust Topology

```text
browser -- secure session cookie --> Console BFF -- OIDC access token --> API Server
   |                                      |                                  |
   +-- OIDC code + PKCE --> IdP           +-- deployment tenant allowlist    +-- JWT validation
                                                                              +-- tenant RBAC
CLI ------------------------ OIDC access token ------------------------------> +-- JobService
                                                                              +-- audit unit of work

Coordinator -- execution UID/epoch heartbeat token --> Scheduler heartbeat endpoint
Controller -- Kubernetes service account + database credential --> reconciliation stores
```

The API Server is the final authorization authority for public Job operations. The Console may
hide controls or restrict tenants for usability, but an API request is never trusted because it
came from the Console. Scheduler heartbeat and Controller reconciliation remain internal workload
paths with their existing, narrower credentials.

## Authentication

### OIDC token validation

Production accepts asymmetric signed JWT access tokens from one configured issuer per deployment.
Validation requires:

- exact HTTPS issuer match and configured audience;
- allowed `RS256` or `ES256` algorithm, valid signature, and known `kid`;
- valid `exp`, optional `nbf`, bounded clock skew, and non-empty `sub`;
- token type accepted by provider configuration when a provider emits `typ` or equivalent claims;
- bounded token size, claim count, string length, and JWKS response size.

JWKS documents are fetched only from the issuer discovery result, cached by `kid` with bounded
entry count and TTL, and refreshed once when a known issuer presents an unknown key. A stale key can
be used only within an explicit short stale-if-error window. Discovery redirects to another origin
are rejected. Tokens are never logged.

The stable external key is `(issuer, subject)`. Email, display name, and group claims are profile
metadata only and cannot grant authorization. A valid new identity is materialized as a disabled or
unassigned principal until a tenant membership or platform assignment exists.

### Console BFF session

The Console implements Authorization Code with PKCE and validates `state`, `nonce`, issuer, and
callback URI. It stores provider tokens in a bounded PostgreSQL session row and sends the browser a
random opaque cookie named `__Host-astra_session` with `Secure`, `HttpOnly`, `Path=/`, and
`SameSite=Lax`. Session identifiers are stored as a keyed hash, rotate after login and privilege
change, and have absolute and idle expiration.

The browser never receives an OIDC access or refresh token. Logout deletes the local session before
attempting provider logout. Refresh tokens, when enabled, are envelope-encrypted at rest and never
used after their session is revoked. Every future state-changing Console request must also pass a
same-origin check and a session-bound CSRF token; `SameSite` is defense in depth, not the only CSRF
control.

### Direct API and CLI

CLI and automation clients obtain an access token from the configured provider and send
`Authorization: Bearer <token>` over TLS. Device Authorization Grant may be enabled for an approved
provider, but the API receives the same validated access-token form. Static long-lived user API
keys are outside this slice.

### Internal service actors

Controller and Scheduler reconciliation do not impersonate the human who originally changed Job
desired state. Each process uses a fixed service actor such as `service:controller` or
`service:scheduler`, a dedicated least-privilege database role, and the namespace already selected
by its reconciliation input. These actors do not receive tenant membership and cannot present
their database credentials to public APIs.

Every automated Job status transition uses the same audited repository unit of work as an API
mutation and records the service actor, execution UID/epoch, and causating request or desired-state
version when available. Routine successful heartbeats remain operational telemetry rather than one
audit row per interval; heartbeat rejection, execution fencing, takeover, and terminal transitions
are security-relevant audit events. Direct SQL mutation outside the audited repositories is denied
to normal process roles where PostgreSQL grants can enforce it.

## Tenant Model

The current Job `namespace` becomes the tenant slug and remains present in protobuf requests and
PostgreSQL Job keys. A tenant row adds an immutable UUID identity, unique slug, display name,
status, and authorization revision. Renaming a tenant slug is not supported in this slice because
it would rewrite durable Job and Kubernetes identities.

An authenticated principal can discover only tenants for which it has an active membership. Every
Job request still names exactly one namespace. Cross-tenant list or wildcard access is not added.
When `CONSOLE_NAMESPACE` is configured, it is an upper deployment bound: the effective tenant set
is the intersection of that value and the user's memberships. Configuration never grants access.

Tenant suspension denies new reads and mutations but does not stop running Jobs automatically.
Stopping or quarantining active workloads is an explicit administrative operation so that an
identity outage cannot silently change data-plane state.

## Authorization

### Enforcement pipeline

```text
transport -> authenticate -> resolve principal -> resolve request namespace
          -> map full RPC method to permission -> load membership/role
          -> authorize -> invoke JobService -> write required audit event
```

A gRPC unary interceptor owns this pipeline. The JSON gateway forwards the bearer token and request
metadata to gRPC; it does not implement a weaker parallel policy. Health and readiness are public
and return no tenant or dependency details. Reflection is disabled in production or requires the
platform diagnostic permission.

The scope resolver is typed: it extracts namespace from each known request message rather than
parsing serialized data. Startup validates that every registered public RPC has a scope and
permission mapping. Unknown methods, empty scope mappings, unknown permissions, inactive
principals, inactive tenants, and absent memberships are denied by default.

Authorization runs before the Job repository is queried. An authenticated caller without tenant
membership receives `PermissionDenied`, regardless of whether that namespace or Job exists. After
authorization succeeds, normal `NotFound`, version conflict, and lifecycle errors remain visible.

Roles and permissions are defined in [authorization-matrix.md](authorization-matrix.md). Built-in
role definitions are versioned in code and seeded idempotently; operators assign roles, not raw
permission strings. Custom roles are deferred until the built-in model has operational evidence.

### Defense in depth

Repositories that mutate tenant-scoped state accept an authorization context containing principal,
tenant UUID, permission, request ID, and policy revision. This context is not a substitute for the
interceptor; it prevents accidental unaudited mutation paths and supports transactionally coupled
audit events. Existing optimistic Job versions and execution epochs remain mandatory.

## Persistence Model

The conceptual PostgreSQL schema is:

| Table | Key and important fields |
|---|---|
| `astrasync_auth_principals` | UUID, issuer, subject, profile fields, status, timestamps; unique issuer/subject |
| `astrasync_auth_tenants` | UUID, immutable namespace slug, display name, status, authz revision |
| `astrasync_auth_memberships` | tenant UUID, principal UUID, role ID, status, granted by/at; unique tenant/principal |
| `astrasync_auth_platform_roles` | principal UUID, platform role, granted by/at |
| `astrasync_auth_sessions` | hashed session ID, principal, encrypted token material, expiry, last seen, revision |
| `astrasync_audit_events` | append-only event fields described below, partitioned by occurred time |

Foreign keys use restrictive deletion for principals and tenants. Security identities are disabled,
not hard-deleted, while retained audit events reference stable UUIDs and snapshot display fields.
Membership changes increment the tenant authorization revision in the same transaction. Platform
role changes increment a platform revision.

### Bootstrap

The first platform administrator is bootstrapped idempotently from an exact issuer and subject,
never from email or domain. Production startup refuses a broad wildcard bootstrap. After the first
assignment, operators remove the bootstrap configuration and use audited role-management APIs.
Tenant creation and initial tenant-admin assignment occur in one transaction and cannot create an
ownerless tenant.

## API Surface

Slice 18 introduces protobuf-first services with generated JSON gateways:

| Service / operation | Purpose |
|---|---|
| `IdentityService.GetCurrentPrincipal` | Current principal and session-safe profile |
| `IdentityService.ListTenants` | Active tenant memberships visible to the caller |
| `AccessService.ListMembers` | Tenant member/role listing |
| `AccessService.GrantRole` | Add or replace one tenant role |
| `AccessService.RevokeRole` | Disable a membership |
| `AccessService.ListRoles` | Built-in role and permission descriptions |
| `AuditService.ListEvents` | Tenant-scoped, bounded audit query |

Requests use opaque pagination tokens with fixed maximum page sizes. Role mutations require an
idempotency key and expected tenant authorization revision. Audit filtering supports bounded time
range, actor, action, decision, and resource identity; unrestricted export is not part of the first
implementation.

The Console adds `/auth/login`, `/auth/callback`, `/auth/logout`, and same-origin session/tenant
adapters. After login, its selected tenant comes from `ListTenants` and remains server-validated.
Slice 17 read routes remain compatible but stop treating process configuration as identity.

## Audit Contract

Each event contains:

- immutable event UUID and database-generated UTC occurrence time;
- request ID and optional trace ID;
- actor principal UUID, actor type, issuer/subject fingerprint, and session or workload identifier;
- tenant UUID and namespace when scoped;
- stable action, resource type, resource identity, and optional Job UID;
- authorization decision and policy revision;
- operation outcome, gRPC code, and HTTP status when applicable;
- expected/before/after version numbers for mutations;
- bounded structured metadata from an action-specific allowlist.

Audit metadata must not contain bearer/session/heartbeat tokens, secrets, passwords, connector
options, raw JobSpecs, request bodies, or response bodies. Successful mutations and their audit
events commit atomically. Authentication events and denied attempts are inserted synchronously;
their audit write failure emits a high-severity security signal but cannot turn a denial into an
allow. The application database role has no update permission on audit rows.

Automated Controller and Scheduler transitions use service actors. They preserve the initiating
desired-state version or request correlation when available, but they never attribute a later
reconciliation decision directly to a human without that durable causation evidence.

## Transport and Deployment

Bearer credentials require TLS. A production profile fails startup when public HTTP or
Console-to-API gRPC is configured as plaintext. TLS may terminate at an ingress only when the
application is configured with explicit trusted proxy networks; forwarded scheme and client
address headers from any other peer are ignored. Internal mTLS is recommended and becomes required
before cross-cluster control-plane traffic is supported.

`AUTH_MODE=disabled` exists only for local development and tests. Production mode rejects it, emits
a startup error, and does not silently fall back when OIDC discovery is unavailable. Database
migrations run before authenticated listeners become ready. Secret values come from deployment
secret references and are redacted from config logging.

## Caching and Revocation

OIDC keys, principal state, and authorization decisions use separate bounded caches. Membership
cache keys include tenant and platform authorization revisions. The first implementation targets a
maximum 30-second authorization cache TTL; role and tenant changes publish revision changes and
invalidate local entries immediately when possible. Disabled principals and revoked Console
sessions are checked on every refresh and cannot outlive the shorter of token expiry, session
expiry, and revocation cache bound.

Authorization does not fail open on PostgreSQL or cache miss errors. Already authenticated reads
return `Unavailable`; mutations do not execute. A recently cached allow can be used only within its
documented revision/TTL bound, never after a locally observed revocation.

## Rollout

1. Add schema, domain types, OIDC validator, permission registry, and audit interfaces behind
   `AUTH_MODE=disabled` compatibility.
2. Enable shadow authentication in non-production: validate identity and record would-deny results
   without changing existing responses.
3. Bootstrap principals, tenants, and role assignments; verify no unmapped RPC or namespace.
4. Enable enforced authentication/RBAC for API and Console in staging with TLS.
5. Enable required transactional audit for mutations and verify retention/backup procedures.
6. Enable production enforcement; remove bootstrap configuration and plaintext listener paths.

Rollback can disable enforcement only in explicitly non-production environments. Production
rollback uses the previous authenticated release and compatible additive schema; it never switches
to anonymous access.

## Acceptance Criteria

- Valid OIDC identities can establish Console sessions and call direct APIs without exposing tokens
  to browser storage or logs.
- Invalid issuer, audience, signature, lifetime, nonce, state, or PKCE proof is rejected.
- Every JobService RPC has one explicit permission and typed tenant-scope resolver.
- Cross-tenant request tampering is denied before repository access and does not reveal existence.
- Role revocation takes effect within the documented bound and invalidates active Console access.
- User and workload credentials are rejected outside their intended endpoint family.
- Successful mutations cannot commit without their audit event; secrets never enter audit rows.
- Development compatibility is explicit, while production refuses disabled auth and plaintext
  bearer-token transport.
- Existing version conflict, epoch fencing, Scheduler heartbeat, and Controller convergence tests
  remain valid under the new admission boundary.
