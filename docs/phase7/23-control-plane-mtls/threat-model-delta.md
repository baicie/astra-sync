# Slice 23 Threat-Model Delta

## Threats closed by this slice

| Threat | Pre-slice posture | Post-slice posture |
|---|---|---|
| An attacker who reaches `:50051` from inside the cluster network but cannot reach the Console BFF can still dial the API Server gRPC API. | The server only enforced Slice 22's `TLS_CERTIFICATE_FILE`. Any client could complete the handshake. | The server now enforces `MTLS_CLIENT_CA_FILE`. Only clients presenting a certificate signed by the deployment-owned CA complete the handshake. |
| An attacker who captures the Console's outbound TLS traffic can reuse the session. | The server could not distinguish the Console from any other client. | The server now refuses handshakes without the Console's certificate. |

## Threats explicitly accepted

- A privileged attacker who steals the Console's client certificate and
  private key from the Console pod can still impersonate the Console.
  The slice does not introduce a Hardware Security Module, a secrets
  manager, or short-lived tokens. Operators must protect the Console
  pod's file system.
- An attacker who controls the deployment's Certificate Authority can
  mint a client certificate and impersonate the Console. The slice does
  not introduce a separate root of trust; the same operations team that
  provisions the API Server certificate provisions the client CA.

## Threats deferred to later slices

- Cross-cluster replication traffic. Phase 7 Slice 25 will lift ADR-010's
  single-region constraint and must decide how to keep the same CA trust
  across regions. The slice does not pre-empt that decision.
- In-flight certificate rotation. The slice restarts pods to rotate.
  Short-lived certificates or dynamic reload can come later when the
  operational cadence justifies the complexity.

## Trust boundaries affected

- The Console BFF → API Server gRPC channel now performs mutual TLS. The
  channel terminates at the API Server gRPC listener on `:50051`.
- The `grpc-gateway` reverse dial from the API Server HTTP listener to
  its own gRPC listener continues to use loopback `insecure`. The
  gateway is bound to `127.0.0.1` by the listener config, so the
  boundary is loopback-only and outside the trust model.

## Compliance mapping

| Control | Where |
|---|---|
| mTLS for control-plane traffic | `control-plane/auth/transport/mtls.go` |
| Production fail-closed gate | `control-plane/api-server/cmd/server/main.go` and `console/cmd/console/main.go` |
| CI verification | `.github/workflows/ci.yml` Phase 7 mTLS verification job |
| Documentation | `docs/phase7/23-control-plane-mtls/*` and `docs/adr/adr-045-*` |