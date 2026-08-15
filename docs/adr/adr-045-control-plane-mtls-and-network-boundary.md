# ADR-045: Control-Plane Mutual TLS and Network Boundary

## Status

Accepted. Implements Phase 7 Slice 23, closing the entry criterion that
ADR-043 §Consequences left open ("Cross-cluster mTLS for Console → API and
Scheduler → API remains a Phase 7 candidate").

## Context

Slice 22 (ADR-043) introduced public-listener TLS, `TRUSTED_PROXY_CIDRS`,
and security headers on the API Server and Console HTTP listeners. The
slice explicitly scoped itself to public listeners and listed cross-cluster
control-plane mTLS as Phase 7 work.

The Phase 7 entry criteria in ADR-044 require that cross-cluster control-plane
mTLS land before any multi-region change. Phase 7 Slice 23 is that work.

### Current architecture

- The Console BFF (`console/cmd/console/main.go` line 233) connects to the
  API Server over gRPC with `credentials.NewTLS(&tls.Config{...})`. The client
  verifies the server certificate against `CONSOLE_API_TLS_CA_FILE` and
  pins `CONSOLE_API_TLS_SERVER_NAME`. It does **not** present a client
  certificate.
- The API Server gRPC listener
  (`control-plane/api-server/cmd/server/main.go` line 360) is built with
  `credentials.NewServerTLSFromFile(...)`. It serves a server certificate
  and stops there; no `ClientCAs` is configured and `ClientAuth` is not
  enforced.
- The Scheduler and Controller do not call the API Server gRPC API. They
  read desired state from PostgreSQL and reconcile Kubernetes resources via
  the in-cluster Kubernetes API. ADR-043 called this out implicitly: the
  Scheduler's egress is the Kubernetes API server, the database, and the
  connection-test executor; it does not dial `:50051`.

The result is that the only control-plane channel that currently crosses a
trust boundary is the Console → API Server gRPC dial. ADR-043's reference to
"Scheduler → API" was forward-looking and does not reflect today's call
graph. This ADR records the deviation and explains how Scheduler trust is
preserved without an mTLS channel.

## Decision

### Slice 23 ships mTLS for Console → API Server

The Console and API Server gain mutual TLS using the same `crypto/tls`
machinery Slice 22 already standardises. The new requirements are:

1. The API Server gRPC listener requires a client certificate signed by a
   deployment-owned CA when `APP_ENV=production`. The CA bundle is loaded
   from `MTLS_CLIENT_CA_FILE`. `MTLS_REQUIRE_CLIENT_CERT` is allowed to be
   `false` only when `APP_ENV` is `development` or `test`.
2. The Console BFF presents a client certificate loaded from
   `CONSOLE_API_CLIENT_CERT_FILE` and `CONSOLE_API_CLIENT_KEY_FILE` when
   `APP_ENV=production`. The existing `CONSOLE_API_TLS_CA_FILE` continues
   to authenticate the server side.
3. Both sides fail closed. Missing or unreadable cert / key / CA files in
   `production` cause the binary to refuse to start with a clear error
   message. The error message points at the missing environment variable
   but does not echo the file contents.
4. The minimum TLS version stays at TLS 1.2 to match Slice 22.

### Boundary preservation for Scheduler and Controller

The Scheduler and Controller trust boundaries are:

- The Scheduler talks to PostgreSQL using the `DATABASE_URL` it already
  owns. PostgreSQL is reachable only on the cluster network; no public
  listener is exposed.
- The Scheduler talks to the Kubernetes API server via the in-cluster
  service account token mounted by the deployment. Token theft is
  mitigated by Kubernetes RBAC and by the existing NetworkPolicy
  (`deployment/helm/astrasync/templates/connection-test-executor/network-policy.yaml`).
- The Controller talks to etcd via the Kubernetes API server using the
  same pattern.

The slice does **not** add a Scheduler → API mTLS channel because the
channel does not exist. Trust is preserved by Kubernetes NetworkPolicy
isolation, which is a deployment-side control. The Helm chart must
declare a NetworkPolicy that limits the API Server's egress to its
database and to the Console BFF's mTLS client.

### No new dependencies

The slice uses only `crypto/tls`, `crypto/x509`, and
`google.golang.org/grpc/credentials`. No SPIFFE, no cert-manager client
SDK, no HashiCorp Vault SDK. Certificate provisioning stays a deployment
concern.

### No in-flight rotation

The slice does not implement dynamic certificate reload. Operators rotate
certificates by restarting the pods. A later slice can lift that constraint
when operational evidence warrants the complexity.

## Boundary

- The slice does not change the API Server's REST gateway
  (`grpc-gateway`). The gateway continues to use loopback `insecure` to
  dial the local gRPC listener; the public HTTPS listener still enforces
  TLS 1.2 from Slice 22.
- The slice does not introduce client certificate-based tenant
  authentication. Tenant identity still comes from the external OIDC
  provider per ADR-036. Client certificates only prove that the dialer is
  an authorised Console BFF.
- The slice does not add a new authorisation model. RBAC and tenant
  scoping are unchanged from Slices 18 and 19.
- The slice does not cover cross-cluster replication. Multi-region
  replication is a Phase 7 Slice 25 topic and depends on this slice.

## Consequences

- Production deployments of the API Server must declare
  `MTLS_CLIENT_CA_FILE`. Production deployments of the Console BFF must
  declare `CONSOLE_API_CLIENT_CERT_FILE` and `CONSOLE_API_CLIENT_KEY_FILE`.
  Operators who omit any of these see the binary refuse to start, matching
  the Slice 22 fail-closed posture.
- The API Server gRPC listener now performs TLS client authentication.
  Operators who previously used `grpcurl` against `:50051` for debugging
  must present a client certificate signed by the same CA.
- The Console BFF dials the API Server with a client certificate. The
  dial code path goes from "server-only TLS" to "mutual TLS".
- The Scheduler continues to be isolated by Kubernetes NetworkPolicy. The
  Helm chart gains an explicit NetworkPolicy entry for the API Server's
  egress, recorded as a deployment-side control.

## Alternatives considered

- **Wire mTLS between Scheduler and API Server.** Rejected. The Scheduler
  does not call the API Server today. Adding an unused channel would
  create a surface that operators must keep configured without providing
  any actual trust guarantee.
- **Adopt SPIFFE / cert-manager client SDK.** Rejected. The slice's
  threat model only needs client identity for the Console BFF, which is a
  single deployment-side issuer. SPIFFE would add a runtime dependency
  and a new identity provider without addressing any concrete gap.
- **Reuse `TLS_CERTIFICATE_FILE` for both server and client auth.**
  Rejected. Mixing server and client material in one PEM file is
  error-prone and breaks standard tooling. A separate CA bundle is
  cleaner.
- **Defer to deployment-only certificates.** Rejected. Without an enforced
  check the gate becomes advisory, and ADR-043 explicitly called out that
  belt-and-braces is appropriate for a security boundary.