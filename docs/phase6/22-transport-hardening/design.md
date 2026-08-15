# Phase 6 Slice 22 Design: Transport Hardening

## Problem

The Slice 18 implementation plan explicitly tracked transport hardening and trusted-proxy
validation as remaining work outside the runtime foundation. As of the merge of Slice 21
the control-plane binaries enforce some, but not all, of the requirements documented in
`docs/phase6/18-auth-rbac-audit/design.md` §"Transport and Deployment":

- The API Server HTTP gateway requires TLS in production. The gRPC listener accepts
  plaintext even in production.
- The Console BFF listens on plaintext HTTP. Production requires HTTPS for `CONSOLE_PUBLIC_URL`
  but never configures the listener itself; the BFF is assumed to be fronted by an ingress.
- Neither binary validates which peer may set `X-Forwarded-For`, `X-Forwarded-Proto`, or
  `X-Forwarded-Host`. An attacker that can speak to the listener directly can spoof the
  client IP, the scheme, or the host that downstream code or audit events observe.
- Neither binary emits `Strict-Transport-Security`, `X-Content-Type-Options`, or
  `Referrer-Policy` headers, so a deployment that forgets one of these at the ingress
  leaks a hardened cookie onto a downgradeable response.

These gaps exist even when authentication, authorization, and audit are otherwise fully
operational. A deployment that flips the Phase 6 rollout gates open without closing the
transport layer inherits a bearer-token interception or client-spoofing risk that the
audit trail cannot detect.

## Decisions

### Production TLS on every public listener

In `APP_ENV=production` every listener that the application starts MUST be TLS-terminated
by the application itself. This removes the assumption that an external ingress will
terminate TLS and ensures that the application can enforce cipher suites, protocol
versions, and certificate rotation policy without bypass.

- `control-plane/api-server/cmd/server/main.go` MUST require
  `TLS_CERTIFICATE_FILE` and `TLS_PRIVATE_KEY_FILE` together when
  `APP_ENV=production`. The gRPC server MUST load those credentials with
  `credentials.NewServerTLSFromFile`, not only the HTTP gateway.
- `console/cmd/console/main.go` MUST add `CONSOLE_TLS_CERTIFICATE_FILE` and
  `CONSOLE_TLS_PRIVATE_KEY_FILE` environment variables and call
  `httpServer.ListenAndServeTLS(cert, key)` when they are present. In
  `APP_ENV=production` the listener refuses to start without both.
- The Console keeps its HTTPS-only `CONSOLE_PUBLIC_URL` requirement.

### Trusted-proxy boundary

A new package `control-plane/auth/transport` exports a single helper used by both binaries:

```go
func TrustedProxy(r *http.Request, proxies []*net.IPNet) (clientIP, scheme, host string, trusted bool)
```

- `proxies` is the parsed list of CIDR ranges the deployment has declared as ingress or
  load-balancer networks. In production the slice requires a non-empty list.
- The helper reads the immediate peer from `r.RemoteAddr`. If that peer lies inside
  `proxies`, the helper consumes the left-most entry of `X-Forwarded-For`, the value of
  `X-Forwarded-Proto`, and the value of `X-Forwarded-Host`. Otherwise it returns the
  immediate peer and the locally observed scheme/host and marks the request as
  untrusted, which the audit writer uses to suppress the `forwarded_for` field.
- The helper ignores `X-Forwarded-For` chains longer than 16 hops and rejects chains with
  entries that are not valid IP addresses. A separate audit event records the rejection.
- The Console BFF uses the helper to populate `http.Request.Host` and the audit event's
  client address before the OIDC callback validation step. The API Server gateway uses
  the helper to populate the request metadata that the authorization interceptor sees.

### Security response headers

A new package `control-plane/auth/transport/headers` exposes:

```go
func SecurityHeaders(secureCookies bool) func(http.Handler) http.Handler
```

When installed, the middleware appends the following headers to every response from the
matched listener:

- `Strict-Transport-Security: max-age=63072000; includeSubDomains` (production only, and
  only when the listener is known to be TLS-terminating).
- `X-Content-Type-Options: nosniff` on every response in any environment.
- `Referrer-Policy: strict-origin-when-cross-origin` on every response in any environment.

The API Server gateway and the Console BFF install this middleware. The middleware runs
after `Strict-Transport-Security` is only added when the request was received over TLS or
the trusted-proxy helper returned `scheme=https`.

### Startup configuration

Each binary adds:

| Variable | Required in production | Purpose |
|---|---|---|
| `TRUSTED_PROXY_CIDRS` | yes | Comma-separated CIDR list of ingress / load-balancer networks that may set `X-Forwarded-*`. Empty list is rejected in production. |
| `CONSOLE_TLS_CERTIFICATE_FILE` | console only, when `APP_ENV=production` | TLS certificate for the Console BFF listener. |
| `CONSOLE_TLS_PRIVATE_KEY_FILE` | console only, when `APP_ENV=production` | TLS private key for the Console BFF listener. |

Existing variables (`TLS_CERTIFICATE_FILE`, `TLS_PRIVATE_KEY_FILE`, `CONSOLE_PUBLIC_URL`,
`CONSOLE_API_TLS_CA_FILE`) keep their current roles.

### Failure semantics

| Condition | Outcome |
|---|---|
| Production startup missing TLS certificate pair | `log.Fatal` with a stable message; no listener starts. |
| Production startup missing `TRUSTED_PROXY_CIDRS` or with empty list | `log.Fatal` with a stable message. |
| Trusted-proxy helper cannot parse `X-Forwarded-For` entry | Falls back to the immediate peer; emits a security audit event `transport.forwarded_rejected`. |
| Listener receives plaintext bearer token in production profile | TLS handshake refuses the connection. The audit trail never sees the request body. |
| Untrusted peer sets `X-Forwarded-Host` | Header is ignored; `r.Host` stays the locally observed value. |

### Rollout

The slice does not introduce a new feature gate. Existing rollout gates stay exactly as
recorded by Slices 18, 19, and 20. Operators who enable enforcement must:

1. Set `TLS_CERTIFICATE_FILE` and `TLS_PRIVATE_KEY_FILE` (API Server).
2. Set `CONSOLE_TLS_CERTIFICATE_FILE` and `CONSOLE_TLS_PRIVATE_KEY_FILE` (Console).
3. Declare `TRUSTED_PROXY_CIDRS` with the exact ingress or load-balancer subnets.
4. Roll out the binary upgrade; both components refuse to start otherwise.

Rollback uses the previous release; the slice preserves backward compatibility with
existing plaintext profiles in `development` and `test`.

## Acceptance Criteria

- `make build` succeeds across the seven Go modules.
- `make check-security` runs the new negative tests and exits 0.
- `make test-go` adds at least 16 new passing tests and remains 100% green.
- `make vet-go` reports no new findings.
- `make catalog-check` is unchanged.
- A staging deployment with `APP_ENV=production` cannot start the API Server without TLS
  files; the same is true for the Console without both `CONSOLE_TLS_*` files and a
  non-empty `TRUSTED_PROXY_CIDRS`.
- An untrusted peer that sends `X-Forwarded-For: 10.0.0.1` is observed by the audit
  pipeline as the untrusted peer; the forwarded address is ignored.
- The Phase 6 README and `docs/adr/README.md` index reference ADR-043 and the Slice 22
  documentation set.