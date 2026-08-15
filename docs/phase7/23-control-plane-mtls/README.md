# Slice 23: Control-Plane Mutual TLS (Console → API Server)

## Summary

Slice 23 closes the Phase 7 entry criterion that ADR-043 left open. It
upgrades the Console BFF → API Server gRPC channel from server-only TLS to
mutual TLS so that an attacker who can reach `:50051` from inside the
cluster network cannot impersonate a Console. The slice covers the only
control-plane channel that actually crosses a trust boundary today; the
remaining Scheduler and Controller boundaries continue to be enforced by
Kubernetes NetworkPolicy.

## Boundary

This slice:

- Configures the API Server gRPC listener with a client CA pool and
  `tls.RequireAndVerifyClientCert` when `APP_ENV=production`.
- Configures the Console BFF to present a client certificate signed by that
  CA when `APP_ENV=production`.
- Adds fail-closed startup checks for the new environment variables.
- Adds a CI gate (`make check-mtls`) that runs the unit tests with a
  self-signed CA and exercises both the happy path and the negative paths.
- Records the deviation from ADR-043: Scheduler → API mTLS is not built
  because no such channel exists.

This slice does not:

- Add a Scheduler → API Server mTLS channel.
- Add a new authentication mechanism or change tenant identity.
- Implement in-flight certificate rotation.
- Adopt SPIFFE, cert-manager client SDK, or Vault SDK.
- Change the public HTTPS listener or the `grpc-gateway` reverse dial.

## Records

- [Slice 23 Design](design.md)
- [Slice 23 Implementation Plan](implementation-plan.md)
- [Slice 23 Threat-Model Delta](threat-model-delta.md)
- [Slice 23 Verification](verification.md)
- [ADR-045: Control-Plane Mutual TLS and Network Boundary](../../adr/adr-045-control-plane-mtls-and-network-boundary.md)
- [ADR-043: Transport Hardening and Trusted-Proxy Boundary](../../adr/adr-043-transport-hardening-and-trusted-proxy-boundary.md)