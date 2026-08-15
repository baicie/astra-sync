# Slice 23 Verification Evidence

The CI gate described in `implementation-plan.md` is the source of truth for
verification. This file records the evidence captured during the slice.

## Tests

- [x] `go test ./control-plane/auth/transport/... -run MTLS` passes
- [x] `go test ./control-plane/auth/transport/...` passes (full transport package)
- [x] `go test ./control-plane/api-server/cmd/server/... -run MTLS` passes
- [x] `go test ./control-plane/api-server/cmd/server/...` passes (full cmd/server)
- [x] `go test ./console/cmd/console/... -run MTLS` passes
- [x] `go test ./console/cmd/console/...` passes (full console cmd)
- [x] `make check-mtls` passes
- [x] `make check-security` passes (depends on `check-mtls`)
- [x] `go test ./...` in control-plane and console passes (no regression)

## Negative tests

- [x] `TestLoadConfigRejectsMissingMTLSClientCA` enforces the API Server gate
- [x] `TestLoadConfigRejectsMTLSRequireClientCertFalse` enforces the API Server gate
- [x] `TestLoadConfigAcceptsMissingMTLSClientCAOutsideProduction` confirms the development posture
- [x] `TestLoadConfigEnforcesProductionAndCompilerMTLSGates` enforces both production gates
- [x] Console test `TestLoadConfigEnforcesProductionOIDCAndAPITLS` enforces the Console client certificate gate
- [x] Console test `TestLoadConfigEnforcesProductionTLSAndTrustedProxy` enforces the Console client certificate gate
- [x] `TestServerTLSConfigRejectsEmptyPaths` enforces the transport helper gate
- [x] `TestServerTLSConfigRequiresClientCAPath` enforces the transport helper gate
- [x] `TestServerTLSConfigRejectsMissingFiles` enforces the IO error gate
- [x] `TestClientTLSConfigRequiresCAAndServerName` enforces the client helper gate
- [x] `TestClientTLSConfigRequiresPairedClientCertificate` enforces the client helper gate
- [x] `TestMTLSEndToEnd` exercises handshake rejection without client cert and with a foreign-CA client cert
- [x] `TestMTLSClientRejectsUnknownServerCA` exercises the inverse case

## End-to-end handshake

- [x] Generated CA signs a server cert and a client cert (`writeTestCertificatePair` in `control-plane/auth/transport/mtls_test.go`)
- [x] gRPC server accepts a client dial from a client that presents the client cert
- [x] gRPC server rejects a client dial that does not present the client cert
- [x] gRPC server rejects a client dial that presents a cert signed by a different CA
- [x] Client rejects a server certificate signed by an unknown CA

## CI gate

- [x] `Repository security checks` job runs `make check-security` which depends on `check-mtls`
- [x] The job fails if the gate is disabled in `Makefile` (the gate is the only entry point for `check-security`)
- [x] `make check-security` outputs an error if `vet-go` fails before reaching `check-mtls`

## Helm gate

- [x] `helm template deployment/helm/astrasync --set apiServer.environment=production --set apiServer.auth.mode=oidc --set console.environment=production --set console.auth.mode=oidc --set console.publicUrl=https://c` fails because `console.api.tls.enabled=false` and `console.api.clientCert.enabled=false` (production gates)
- [x] `helm template deployment/helm/astrasync --set apiServer.environment=production --set apiServer.auth.mode=oidc` fails because `apiServer.mtls.enabled=false` (production gate)
- [x] `helm template deployment/helm/astrasync --set apiServer.environment=development` renders successfully