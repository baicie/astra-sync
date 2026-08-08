# Phase 6 Slice 18 Threat Model

## Assets

- Job specifications, lifecycle state, checkpoint/failure metadata, and tenant membership.
- OIDC access/refresh tokens, Console session identifiers, and CSRF secrets.
- Role assignments, platform bootstrap authority, and audit evidence.
- Scheduler heartbeat capabilities, database credentials, and Kubernetes service accounts.

Connector credentials referenced by a Job are high-value assets but remain owned by the deployment
secret boundary; Slice 18 must not copy them into identity or audit storage.

## Trust Boundaries

1. Untrusted browser or CLI to Console/API ingress.
2. Console BFF to external OIDC provider.
3. Console to API Server internal gRPC.
4. API Server to PostgreSQL identity, Job, and audit stores.
5. Controller/Scheduler to Kubernetes and PostgreSQL.
6. Coordinator to the Scheduler heartbeat endpoint.

## Threats and Required Controls

| Threat | Required control | Verification |
|---|---|---|
| Forged or issuer-confused JWT | Exact issuer/audience, asymmetric algorithm allowlist, bounded JWKS discovery | Unit tests with wrong issuer, audience, alg, key, and `kid` |
| Expired or replayed identity | Lifetime validation, short access-token TTL, nonce/state/PKCE, bounded sessions | Clock and replay tests |
| Browser token theft | BFF keeps tokens server-side; opaque Secure HttpOnly cookie; no local/session storage | Browser and header tests |
| Session fixation | Rotate session ID after login and privilege change; hash IDs at rest | Session lifecycle tests |
| CSRF on future mutations | Same-origin validation, session-bound CSRF token, SameSite cookie | Cross-origin request tests |
| Namespace tampering | API extracts typed request scope and checks local membership before repository access | Cross-tenant tests for every RPC |
| Role escalation through claims | Ignore token roles/groups for authorization; local assignment only | Claim injection tests |
| Confused-deputy Console | API revalidates token and RBAC; Console tenant filter is not authority | Direct forged Console request tests |
| Workload token used as user token | Separate token formats, endpoints, validators, and audiences | Cross-credential negative tests |
| User token used for heartbeat | Heartbeat accepts only execution UID/epoch capability token | Existing and added negative tests |
| Stale role cache | Revisioned bounded cache, immediate local invalidation, maximum TTL | Revocation race tests |
| Tenant or Job enumeration | Authorize before lookup; consistent `PermissionDenied` outside scope | Existence-oracle tests |
| Missing permission on new RPC | Startup service-descriptor inventory; deny unknown method | Registration completeness test |
| Audit bypass on mutation | Repository unit of work atomically writes state and audit | Transaction rollback integration test |
| Controller/Scheduler bypasses user API audit | Fixed service actors, dedicated DB roles, audited repository unit of work | Automated transition audit integration test |
| Audit tampering | Append-only DB grants, restricted purge role, retention evidence | Database privilege integration test |
| Secret leakage in audit/logs | Action-specific allowlist and redaction; no bodies/options/tokens | Sentinel secret tests |
| Header spoofing | Trust forwarded headers only from configured proxy networks | Proxy boundary tests |
| Bearer interception | TLS required in production; reject insecure startup | Configuration tests |
| Resource exhaustion | Token/JWKS/session/page bounds, request timeouts, rate limits at ingress | Boundary and load tests |
| Bootstrap takeover | Exact issuer/subject, idempotent one-time assignment, no email wildcard | Bootstrap tests and deployment check |
| IdP or database outage | Fail closed; bounded stale JWKS only; no anonymous fallback | Dependency failure tests |

## Abuse Cases

### Cross-tenant read

A viewer changes `namespace=tenant-b` in a REST body or Console URL. The API Server resolves
`tenant-b` from the typed RPC request, finds no active membership, records a denied `jobs.read`
decision, and returns `PermissionDenied` without querying the Job repository.

### Cross-tenant mutation

An operator with `jobs.start` in tenant A replays a valid request against tenant B. The request is
denied before version or lifecycle state is read. A guessed Job name or UID produces the same
external response as a nonexistent resource.

### Stolen Console cookie

An attacker obtains the opaque cookie. Secure/HttpOnly/SameSite flags reduce transport and script
exposure; server-side idle/absolute expiry, revocation checks, and session rotation bound use. A
state-changing request also requires the session-bound CSRF proof. Logout or principal disablement
revokes the server-side row.

### Heartbeat credential presented to API

The UUID heartbeat token is not a JWT for the configured OIDC issuer and fails user authentication.
The Job API never interprets it as a principal. Conversely, an OIDC token does not match the
execution record's stored heartbeat capability and is rejected by the Scheduler.

### Audit storage unavailable

A Job mutation reaches its repository unit of work but audit insertion fails. The transaction rolls
back and returns `Unavailable`; no state change is committed. A denied request remains denied even
if its separate audit insertion fails, while the service emits a high-severity operational signal.

## Residual Risks

- A compromised external IdP can authenticate an attacker as an existing subject.
- A compromised platform administrator can grant broad access; audit improves detection, not
  prevention.
- PostgreSQL superusers can alter audit data; external immutable export is future hardening.
- Authorization cache TTL creates a documented short revocation window across replicas.
- Traffic metadata remains visible to infrastructure that terminates TLS.
- A PostgreSQL superuser or process granted unrestricted table writes can bypass repository-level
  audit coupling; database role review and external immutable export remain defense in depth.
