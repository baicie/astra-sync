# Slice 23 Design: Control-Plane Mutual TLS

## Goals

1. Make the API Server gRPC listener enforce client certificate
   authentication when `APP_ENV=production`.
2. Make the Console BFF present a deployment-owned client certificate
   when `APP_ENV=production`.
3. Fail closed on missing certificates.
4. Stay additive: do not regress Slice 22's HTTPS posture and do not
   disturb the `grpc-gateway` reverse dial.
5. Provide a CI gate that exercises both happy and negative paths.

## Environment variables

### API Server

| Variable | Required (production) | Purpose |
|---|---|---|
| `MTLS_CLIENT_CA_FILE` | yes | CA bundle that signs Console client certificates |
| `MTLS_REQUIRE_CLIENT_CERT` | yes (default `true`) | Whether to reject handshakes without a client certificate. The slice refuses to start if this is `false` while `APP_ENV=production`. |

The existing Slice 22 variables remain unchanged:

| Variable | Required (production) | Purpose |
|---|---|---|
| `TLS_CERTIFICATE_FILE` | yes | Server certificate presented to gRPC clients |
| `TLS_PRIVATE_KEY_FILE` | yes | Matching private key |

### Console

| Variable | Required (production) | Purpose |
|---|---|---|
| `CONSOLE_API_CLIENT_CERT_FILE` | yes | Client certificate presented to API Server |
| `CONSOLE_API_CLIENT_KEY_FILE` | yes | Matching private key |
| `CONSOLE_API_TLS_CA_FILE` | yes | CA bundle that signs the API Server certificate |
| `CONSOLE_API_TLS_SERVER_NAME` | no (default `api-server`) | Server name pinning |

The client and server certificate chains are intentionally separated. A
misconfiguration where the Console presents its server certificate as a
client certificate would fail the `ClientCAs` verification, which is
exactly the failure mode the gate should detect.

## Package layout

```
control-plane/auth/transport/
    headers.go              # existing
    headers_test.go         # existing
    trusted_proxy.go        # existing
    trusted_proxy_test.go   # existing
    mtls.go                 # NEW: ServerTLSConfig, ClientTLSConfig
    mtls_test.go            # NEW: end-to-end mTLS handshake tests
```

`mtls.go` exposes two constructors:

```go
func ServerTLSConfig(input ServerTLSConfigInput) (*tls.Config, error)
func ClientTLSConfig(input ClientTLSConfigInput) (*tls.Config, error)
```

Both functions accept file paths rather than raw PEM material so that the
binary stays in control of file IO errors and can produce clear startup
errors. Both functions apply `MinVersion: tls.VersionTLS12` to match Slice
22.

`ServerTLSConfigInput` carries:

- `CertificateFile` — server certificate
- `PrivateKeyFile` — server private key
- `ClientCAPath` — CA bundle for client verification
- `RequireClientCert` — `true` in production
- `MinVersion` — defaults to TLS 1.2

`ClientTLSConfigInput` carries:

- `CertificateFile` — optional client certificate
- `PrivateKeyFile` — optional client private key
- `CAPath` — CA bundle for server verification
- `ServerName` — server name pinning
- `MinVersion` — defaults to TLS 1.2

## API Server integration

`control-plane/api-server/cmd/server/main.go` gains:

- `mtlsClientCAFile` and `mtlsRequireClientCert` in the `config` struct.
- Production fail-closed checks in `loadConfig`.
- A call to `transport.ServerTLSConfig` before the existing
  `credentials.NewServerTLSFromFile`. The new path replaces the old one
  when `mtlsClientCAFile` is set.

The slice keeps the legacy path so that local development against
`APP_ENV=development` still works without a CA bundle.

## Console integration

`console/cmd/console/main.go` gains:

- `apiClientCertFile` and `apiClientKeyFile` in the `config` struct.
- Production fail-closed checks in `loadConfig`.
- A call to `transport.ClientTLSConfig` in `apiDialOptions` so that the
  client certificate is loaded whenever it is configured.

The existing `insecure` path stays for `APP_ENV=development`.

## Fail-closed behaviour

The slice adds three new negative tests:

1. `TestLoadConfigRejectsMissingMTLSClientCA` — `APP_ENV=production` with
   no `MTLS_CLIENT_CA_FILE` causes `loadConfig` to return a clear error.
2. `TestLoadConfigRejectsMTLSRequireClientCertFalseInProduction` —
   `MTLS_REQUIRE_CLIENT_CERT=false` while `APP_ENV=production` is
   rejected.
3. `TestLoadConfigRejectsMissingConsoleClientCert` — `APP_ENV=production`
   with no `CONSOLE_API_CLIENT_CERT_FILE` / `CONSOLE_API_CLIENT_KEY_FILE`
   causes `loadConfig` to return a clear error.

These tests fail if the binary ever stops enforcing the gate.

## CI gate

`make check-mtls`:

```makefile
.PHONY: check-mtls
check-mtls:
    go test ./control-plane/auth/transport/... -run MTLS -count=1
    go test ./control-plane/api-server/cmd/server/... -run MTLS -count=1
    go test ./console/cmd/console/... -run MTLS -count=1
```

`.github/workflows/ci.yml` gains a `Phase 7 mTLS verification` job that
runs `make check-mtls` and adds the job to the required-check set.

## Threat-model delta

See `threat-model-delta.md`.

## Verification

See `verification.md`.