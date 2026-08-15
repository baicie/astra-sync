# Phase 6 Slice 22: Transport Hardening

## Status

Design draft. Implementation not yet started.

Slice 22 closes the transport-hardening gap explicitly tracked by the Slice 18 implementation
plan (§6) and the Phase 6 README ("transport hardening and production identity rollout are
not yet delivered"). It enforces production TLS on every public control-plane listener and
adopts an explicit trusted-proxy boundary for forwarded-header interpretation.

The slice does not change authentication, authorization, or audit semantics. It removes
deployment-configurable ambiguity that could otherwise turn a benign network path into a
bearer-token interception or client-spoofing vulnerability.

## Intended Outcomes

- Production TLS termination required on the API Server gRPC listener, the API Server HTTP
  gateway, and the Console BFF listener.
- Strict, deployment-declared trusted-proxy boundary that governs when
  `X-Forwarded-For`, `X-Forwarded-Proto`, and `X-Forwarded-Host` may be trusted, and that
  forces the original peer otherwise.
- Security response headers (`Strict-Transport-Security`,
  `X-Content-Type-Options`, `Referrer-Policy`) returned by both the API Server gateway and
  the Console BFF in production profiles.
- CI `make check-security` target that runs startup-config negative tests and trusted-proxy
  boundary tests against both binaries' `main_test.go`.
- Phase 6 README and acceptance index updated so the slice is auditable as the closing of
  the deployment-boundary gap.

## Non-goals

- IdP registration, key rotation, or session-revocation runbooks. These are operations-side
  artefacts that the repository may template but that an operator must own in each
  deployment; they remain in the Phase 6 closeout backlog.
- Browser end-to-end suites that exercise authenticated Console traffic. Slice 22 is a
  transport boundary slice; the slice-level implementation plan adds the CI gate but does
  not claim a full browser suite.
- PostgreSQL encryption-at-rest, secret references inside compiled images, or supply-chain
  hardening of the published binaries. These are deployment-side controls owned by the
  Helm chart and the registry, not by `cmd/server/main.go`.
- Cross-cluster control-plane mTLS. The repository already requires mTLS to the
  Compiler Validation service in production (`control-plane/api-server/cmd/server/main.go`
  `COMPILER_VALIDATION_TLS_*`). Extending mTLS to Console → API or Scheduler → API is a
  Phase 7 candidate.
- New authentication or authorization features.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [Threat-model delta](threat-model-delta.md)
- [ADR-043: Transport Hardening and Trusted-Proxy Boundary](../../adr/adr-043-transport-hardening-and-trusted-proxy-boundary.md)

## Boundary

This slice does not modify authorization policy, role mappings, audit content, or the
existing OIDC validator. It does not introduce a new public RPC. It does not relax any
production failure mode currently enforced by `loadConfig` in either binary. Existing
development and test profiles keep their current plaintext listeners.