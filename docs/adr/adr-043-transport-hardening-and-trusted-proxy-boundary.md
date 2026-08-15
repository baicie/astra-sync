# ADR-043: Transport Hardening and Trusted-Proxy Boundary

## Status

Accepted (Phase 6, Slice 22). Implements the transport-hardening section tracked by
ADR-036 and the Slice 18 implementation plan §6.

## Context

The Slice 18 design and threat model committed AstraSync to a deployment-side boundary
in which bearer credentials travel only over TLS, the API Server and Console never trust
forwarded headers from arbitrary peers, and every listener emits the security response
headers that browsers and CSRF defences rely on. The runtime foundation implemented in
Slice 18 covered OIDC validation, RBAC enforcement, and transactional audit, but
explicitly tracked transport hardening as remaining operator-controlled work.

By the close of Slice 21 the binaries expose the following gaps:

1. The API Server gRPC listener accepts plaintext even in `APP_ENV=production`. Only the
   HTTP gateway requires TLS.
2. The Console BFF listens on plaintext HTTP. The BFF assumes a fronting ingress will
   terminate TLS, but the application never enforces cipher, certificate, or HSTS policy
   itself, and never requires that an ingress is even present.
3. Neither binary validates `X-Forwarded-For`, `X-Forwarded-Proto`, or
   `X-Forwarded-Host`. The OIDC callback validator reads the host from `r.Host`, and the
   audit writer trusts the immediate peer to be the client. An attacker that can speak
   to the listener can spoof both.
4. Neither binary emits `Strict-Transport-Security`, `X-Content-Type-Options`, or
   `Referrer-Policy`. A misconfigured ingress that drops one of these headers silently
   downgrades the security posture of every Console response.

These gaps are independent of authentication, authorization, and audit. They are
deployment-amplifiable: an operator that toggles the Phase 6 rollout gates on without
closing the transport layer exposes bearer tokens and audit-spoofed identities even when
the application code is otherwise correct.

## Decision

We add a new package `control-plane/auth/transport` that exports two reusable
components, and we wire them into both the API Server and the Console BFF.

### Reusable package

`control-plane/auth/transport` contains:

- `ParseCIDRList(string) ([]*net.IPNet, error)` — strict CIDR parser that rejects
  empty lists and entries that do not parse.
- `TrustedProxy(*http.Request, []*net.IPNet) (ClientAddress, Scheme, Host string, Trusted bool)` —
  reads `r.RemoteAddr` and decides whether the peer lies in a declared trusted network.
  When the peer is trusted, the helper consumes the left-most valid IP from
  `X-Forwarded-For`, the value of `X-Forwarded-Proto`, and the value of `X-Forwarded-Host`.
  When the peer is untrusted, the helper returns the immediate peer and the locally
  observed scheme and host.
- `TrustedProxyMiddleware([]*net.IPNet) func(http.Handler) http.Handler` — installs the
  helper, populates request-scoped fields the audit writer reads, and emits a security
  audit event when a forwarded chain is malformed.
- `SecurityHeaders(secureCookiesEnabled bool) func(http.Handler) http.Handler` — emits
  `X-Content-Type-Options: nosniff` and `Referrer-Policy: strict-origin-when-cross-origin`
  on every response. Emits `Strict-Transport-Security: max-age=63072000; includeSubDomains`
  when the request arrived over TLS or the trusted-proxy helper returned `scheme=https`.

### API Server wiring

`control-plane/api-server/cmd/server/main.go` extends `loadConfig` to require:

- `TLS_CERTIFICATE_FILE` and `TLS_PRIVATE_KEY_FILE` together in `APP_ENV=production`,
  regardless of whether `HTTP_LISTEN_ADDRESS` is set.
- A non-empty `TRUSTED_PROXY_CIDRS` in `APP_ENV=production`.

`run` then:

- Loads the credentials into the gRPC server with `credentials.NewServerTLSFromFile`.
- Wraps the HTTP gateway with `transport.SecurityHeaders(true)` followed by
  `transport.TrustedProxyMiddleware(cidrs)`.
- Records the trusted-proxy helper output into the audit writer's request metadata.

### Console wiring

`console/cmd/console/main.go` extends `loadConfig` to add:

- `CONSOLE_TLS_CERTIFICATE_FILE` and `CONSOLE_TLS_PRIVATE_KEY_FILE`. Both are required in
  `APP_ENV=production`. The listener calls `httpServer.ListenAndServeTLS` when they are
  present.
- `TRUSTED_PROXY_CIDRS`, required to be non-empty in `APP_ENV=production`.

`run` installs the same middleware chain on the Console HTTP handler. The OIDC callback
validator reads the scheme from the trusted-proxy helper output rather than from the raw
header, so a spoofed `X-Forwarded-Proto` cannot redirect the IdP callback to a different
origin.

### CI gate

The root `Makefile` adds a `check-security` target that depends on the existing Go checks
and additionally runs the `control-plane/auth/transport` tests and the negative startup
tests in both binaries' `main_test.go`. The `.github/workflows/ci.yml` file adds a
`Repository security checks` job that invokes `make check-security` and marks it as
required. The job fails closed when the negative startup tests do not run, which prevents
operators from accidentally disabling the new gate.

### Threat model and verification

`docs/phase6/22-transport-hardening/threat-model-delta.md` records the threats closed
and the residual risks accepted by the slice. `docs/phase6/22-transport-hardening/verification.md`
records the CI evidence. `docs/phase6/acceptance.md` and `docs/phase6/README.md` index the
slice.

## Consequences

- Production deployments must declare `TLS_CERTIFICATE_FILE`, `TLS_PRIVATE_KEY_FILE`,
  `CONSOLE_TLS_CERTIFICATE_FILE`, `CONSOLE_TLS_PRIVATE_KEY_FILE`, and a non-empty
  `TRUSTED_PROXY_CIDRS`. Operators who omit any of these will see the binary refuse to
  start. This is an intentional fail-closed posture that matches the existing production
  rules for `AUTH_MODE`, `OIDC_ISSUER`, and the compiler-validation mTLS bundle.
- The Console BFF now requires its own TLS termination. Operators who relied on an
  ingress-only TLS termination must either run TLS twice or move cipher policy into the
  binary configuration.
- Cross-cluster mTLS for Console → API and Scheduler → API remains a Phase 7 candidate.
  The slice deliberately scopes itself to public-listener hardening.
- The `auth/transport` package becomes a permanent shared dependency. It must keep a
  tight public surface (no logging of forwarded chains, no bypass flags) and its tests
  must run as part of every PR.

## Alternatives considered

- **Rely on the ingress for everything.** Rejected. We already know deployments vary
  widely and the threat model explicitly assumes the ingress may be misconfigured.
  Belt-and-braces is appropriate for a security boundary.
- **Make the trusted-proxy middleware opt-in.** Rejected. The default would silently
  remain the unsafe behaviour for any deployment that does not read the release notes.
- **Forward every header to the audit writer.** Rejected. The audit metadata allowlist
  documented by ADR-037 does not include raw forwarded chains, and storing them would
  leak internal infrastructure topology.