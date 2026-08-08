# ADR-036: External OIDC and Local Tenant Authorization

## Status

Accepted

## Context

AstraSync needs browser, CLI, and API authentication without becoming a password or identity
provider. The existing Job API accepts a caller-supplied namespace and has no interceptor, while
the Slice 17 Console constrains one namespace only through process configuration. Embedding roles
from an external token would make authorization changes dependent on identity-provider token
lifetimes and would couple AstraSync permissions to provider-specific claim formats.

The platform also has workload credentials, such as the execution-scoped Scheduler heartbeat
token. Those credentials prove one narrow machine capability and must not be accepted as human or
general service identity.

## Decision

Use an external OpenID Connect provider for human and CLI identity proof. The API Server validates
issuer, audience, signature, lifetime, and subject on every bearer-token authentication path.
AstraSync stores principals, tenants, memberships, built-in role assignments, and authorization
revisions in PostgreSQL. Roles are never trusted from arbitrary token claims.

The Console uses an OAuth 2.0 Authorization Code flow with PKCE as a backend-for-frontend. The
browser receives only an opaque, secure, HttpOnly session cookie; the Console keeps provider tokens
in a bounded server-side session. It forwards the access token to the API Server, which repeats
authentication and authorization independently. CLI clients send the same class of access token
directly to the API Server.

Existing Job namespaces become tenant resource scopes. The API resolves the namespace from the
typed request and requires a matching local tenant membership before invoking the Job service.
`CONSOLE_NAMESPACE`, when configured, is an additional deployment allowlist and never grants
membership. Unknown RPC methods and resources without an explicit permission mapping are denied.

Workload capability tokens remain separate, endpoint-specific credentials. They are not OIDC
tokens, do not enter the user RBAC engine, and cannot call Job APIs.

## Consequences

AstraSync does not store passwords or implement account recovery. Identity-provider availability
and key rotation become authentication dependencies. Permission revocation can take effect quickly
because roles remain local, but PostgreSQL authorization lookups and cache invalidation are added to
the request path. Console sessions require durable storage and explicit expiration. Development can
retain a guarded disabled-auth mode, while production startup must reject that mode and insecure
bearer-token transport.
