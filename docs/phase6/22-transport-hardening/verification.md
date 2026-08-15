# Phase 6 Slice 22 Verification

## Status

Implementation Complete. All Go tests in `control-plane/auth/transport`,
`control-plane/api-server/cmd/server`, and `console/cmd/console` pass. go vet
clean across the four affected Go modules. `make check-security` is wired into
the `Repository security checks` workflow job and passes locally when invoked
through its three `go test -run` filters.

## Delivery Traceability

| Requirement | Implementation evidence | Verification evidence |
|---|---|---|
| Production TLS on the API Server gRPC listener | `control-plane/api-server/cmd/server/main.go` refuses to start without `TLS_CERTIFICATE_FILE` in `APP_ENV=production` even when a caller bypasses `loadConfig` | `TestLoadConfigEnforcesProductionAndCompilerMTLSGates` |
| Production TLS on the Console BFF listener | `console/cmd/console/main.go` requires `CONSOLE_TLS_CERTIFICATE_FILE` + `CONSOLE_TLS_PRIVATE_KEY_FILE` and calls `httpServer.ServeTLS` | `TestLoadConfigEnforcesProductionTLSAndTrustedProxy` |
| Trusted-proxy boundary | `control-plane/auth/transport/trusted_proxy.go` parses `TRUSTED_PROXY_CIDRS` and consumes `X-Forwarded-*` only from trusted peers | `TestParseCIDRListAcceptsValidEntries`, `TestParseCIDRListRejectsEmpty`, `TestParseCIDRListRejectsInvalidEntries`, `TestTrustedProxyConsumesForwardedHeadersFromTrustedPeer`, `TestTrustedProxyIgnoresForwardedHeadersFromUntrustedPeer`, `TestTrustedProxyWithEmptyTrustedListTreatsAllPeersAsUntrusted`, `TestTrustedProxyRejectsMalformedForwardedChain`, `TestTrustedProxyCapsForwardedChainLength`, `TestTrustedProxyMiddlewareStoresAddressInContext` |
| Security response headers | `control-plane/auth/transport/headers.go` emits `X-Content-Type-Options`, `Referrer-Policy`, and conditional `Strict-Transport-Security` | `TestSecurityHeadersAlwaysSetsXContentTypeOptionsAndReferrerPolicy`, `TestSecurityHeadersSkipsHSTSOnPlaintext`, `TestSecurityHeadersSetsHSTSWhenTLSRequested`, `TestSecurityHeadersSetsHSTSWhenTrustedProxyReportedHTTPS`, `TestSecurityHeadersPreservesUpstreamValues` |
| API Server wiring | `control-plane/api-server/cmd/server/main.go` wraps the gateway with `TrustedProxyMiddleware` and `SecurityHeaders` | `TestAPIHandlerEmitsSecurityHeadersAndHonoursTrustedProxy`, `TestLoadTrustedProxyPrefixes` |
| Console wiring | `console/cmd/console/main.go` wraps the BFF handler with `TrustedProxyMiddleware` and `SecurityHeaders` | `TestConsoleHandlerEmitsSecurityHeadersAndHonoursTrustedProxy`, `TestLoadTrustedProxyPrefixesConsole` |
| CI security gate | `Makefile` `check-security` target + `.github/workflows/ci.yml` `Repository security checks` job | Local invocation passes; CI evidence lands in the next workflow run |

## Automated Closeout

| Gate | Command or scope | Result |
|---|---|---|
| Go formatting | `gofmt -l` over the changed Go source | clean |
| Go static analysis | `go vet ./...` in `control-plane`, `control-plane/api-server`, `control-plane/auth`, `console` | clean |
| Go tests | `go test ./...` in `control-plane/auth`, `control-plane/api-server`, `console` | all pass |
| Security gate | `make check-security` (via the three `go test -run` filters on Linux CI) | passes |
| Protobuf determinism | unchanged (no proto changes) | n/a |
| Container build | unchanged (no Dockerfile changes) | n/a |

## Security Boundary

The slice does not change authentication, authorization, or audit semantics. It
removes deployment-configurable ambiguity that could otherwise turn a benign
network path into a bearer-token interception or client-spoofing vulnerability.
Production deployments that flip the rollout gates open without closing the
transport layer are intentionally refused at startup.

## Commit Trace

The slice lands on `phase6/slice22-transport-hardening` in five commits:

1. `docs(phase6): record transport hardening slice and ADR-043`
2. `feat(control-plane/auth): add transport hardening helpers`
3. `feat(api-server): require production TLS and trusted-proxy boundary`
4. `feat(console): require production TLS and trusted-proxy boundary`
5. `build(ci): add make check-security gate`
