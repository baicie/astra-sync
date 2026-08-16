# Slice 23 Implementation Plan

## Order of work

The slice lands in five commits so that each step is independently
reviewable and revertable. The order follows the dependency chain.

### Commit 1 — `docs(phase7): record slice 23 design and ADR-045`

- Add `docs/phase7/23-control-plane-mtls/README.md`.
- Add `docs/phase7/23-control-plane-mtls/design.md`.
- Add `docs/phase7/23-control-plane-mtls/implementation-plan.md` (this file).
- Add `docs/phase7/23-control-plane-mtls/threat-model-delta.md`.
- Add `docs/phase7/23-control-plane-mtls/verification.md` (empty template).
- Add `docs/adr/adr-045-control-plane-mtls-and-network-boundary.md`.
- Update `docs/adr/README.md` to index ADR-045.

No code changes. The plan file is reviewed before code.

### Commit 2 — `feat(control-plane/auth): add mTLS helpers`

- Add `control-plane/auth/transport/mtls.go` with
  `ServerTLSConfig` and `ClientTLSConfig`.
- Add `control-plane/auth/transport/mtls_test.go` covering:
  - Server rejects handshake without a client certificate when
    `RequireClientCert=true`.
  - Server accepts handshake with a certificate signed by `ClientCAs`.
  - Server rejects handshake with a certificate signed by an unknown CA.
  - Client validates the server certificate against `CAPath` and
    `ServerName`.
  - Client presents a client certificate when one is configured.

The tests generate their own CA, server cert, and client cert at runtime
so that no fixtures leak into the repository.

### Commit 3 — `feat(api-server): require mTLS in production`

- Update `control-plane/api-server/cmd/server/main.go`:
  - Add `mtlsClientCAFile` and `mtlsRequireClientCert` to `config`.
  - Add the production fail-closed checks in `loadConfig`.
  - Replace `credentials.NewServerTLSFromFile` with
    `transport.ServerTLSConfig` when `mtlsClientCAFile` is set.
- Update `control-plane/api-server/cmd/server/main_test.go`:
  - Add `TestLoadConfigRejectsMissingMTLSClientCA`.
  - Add `TestLoadConfigRejectsMTLSRequireClientCertFalseInProduction`.
  - Add an end-to-end mTLS handshake test that wires up the gRPC
    listener with a generated CA and verifies a happy path.

### Commit 4 — `feat(console): require client certificate in production`

- Update `console/cmd/console/main.go`:
  - Add `apiClientCertFile` and `apiClientKeyFile` to `config`.
  - Add the production fail-closed checks in `loadConfig`.
  - Replace the inline `tls.Config` in `apiDialOptions` with
    `transport.ClientTLSConfig`.
- Update `console/cmd/console/main_test.go`:
  - Add `TestLoadConfigRejectsMissingConsoleClientCert`.
  - Extend the existing production gate tests to require the new client
    certificate files.

### Commit 5 — `build(ci): add mTLS verification gate`

- Update `Makefile`:
  - Add the `check-mtls` target.
  - Add `check-mtls` to the `check-security` chain so that `make check`
    runs it.
- Update `.github/workflows/ci.yml`:
  - Add the `Phase 7 mTLS verification` job.
  - Add the job to the required-check set so that PRs without it cannot
    merge.

### Commit 6 — `docs(phase7): record slice 23 verification evidence`

- Fill in `verification.md` with the CI evidence captured during the
  slice.

### Commit 7 — Helm

- Update `deployment/helm/astrasync/values.yaml` with the new
  environment variables.
- Update `deployment/helm/astrasync/templates/api-server/deployment.yaml`
  to inject the variables.
- Update `deployment/helm/astrasync/templates/console/deployment.yaml` to
  inject the variables.
- Update `deployment/helm/astrasync/templates/connection-test-executor/network-policy.yaml`
  to allow the API Server to talk to the Console mTLS client.

The Helm commit is added to the same PR because the slice is not
deployable without it.

## Local checks

Before opening the PR:

```bash
make check
make test-java
make test-go
```

The CI gate runs `make check-mtls` as part of the new job.