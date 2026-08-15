# Phase 6 Slice 22 Implementation Plan

## 1. Shared transport package

- [ ] Create `control-plane/auth/transport/trusted_proxy.go` exporting
      `TrustedProxy(*http.Request, []*net.IPNet) (ClientAddress, bool)` and
      `ParseCIDRList(string) ([]*net.IPNet, error)`.
- [ ] Create `control-plane/auth/transport/headers.go` exporting
      `SecurityHeaders(secureCookies bool) func(http.Handler) http.Handler`.
- [ ] Add `control-plane/auth/transport/trusted_proxy_test.go` covering: trusted peer
      with single-hop and multi-hop `X-Forwarded-For`; untrusted peer; malformed chain;
      loopback IPv4 and IPv6; non-IP entries.
- [ ] Add `control-plane/auth/transport/headers_test.go` covering: HSTS only on TLS or
      forwarded `https`; `nosniff` and `Referrer-Policy` always present; header
      idempotence when the upstream already set HSTS.

## 2. API Server wiring

- [ ] Extend `control-plane/api-server/cmd/server/main.go` to require TLS files in
      `APP_ENV=production` even when the binary is configured for `gRPC_LISTEN_ADDRESS`.
      Both the gRPC listener and the HTTP listener MUST load the credentials.
- [ ] Add `TRUSTED_PROXY_CIDRS` parsing and pass the CIDR list to the new HTTP middleware.
- [ ] Wrap the gateway with `transport.SecurityHeaders(true)` then
      `transport.TrustedProxyMiddleware(cidrs)` in the production and test paths.
- [ ] Update `loadConfig` so that an empty or missing `TRUSTED_PROXY_CIDRS` in production
      fails startup with a stable message.
- [ ] Extend `control-plane/api-server/cmd/server/main_test.go` with:
      - production missing TLS pair → `log.Fatal`-equivalent error;
      - production empty `TRUSTED_PROXY_CIDRS` → startup error;
      - development profile accepts plaintext and an empty `TRUSTED_PROXY_CIDRS`.

## 3. Console wiring

- [ ] Extend `console/cmd/console/main.go` with `CONSOLE_TLS_CERTIFICATE_FILE` and
      `CONSOLE_TLS_PRIVATE_KEY_FILE` and call `httpServer.ListenAndServeTLS` when both
      are configured.
- [ ] Require both in `APP_ENV=production`. The current `CONSOLE_API_TLS_CA_FILE`
      production requirement stays.
- [ ] Add `TRUSTED_PROXY_CIDRS` parsing and install the trusted-proxy middleware on the
      Console BFF handler chain.
- [ ] Wrap the Console HTTP handler with `transport.SecurityHeaders(true)`.
- [ ] Extend `console/cmd/console/main_test.go` with the same negative coverage as the
      API Server: missing TLS files, missing CIDRs, untrusted peer forwarded headers.

## 4. CI gate

- [ ] Add `make check-security` to the root `Makefile`. The target must depend on the
      existing Go checks and additionally run `go test -count=1 ./...` for
      `control-plane/auth/transport` and both binaries' `cmd/.../main_test.go`.
- [ ] Add a `Repository security checks` workflow job in `.github/workflows/ci.yml`
      that invokes `make check-security` after the existing Go checks complete. Mark it
      as required.

## 5. Documentation and index updates

- [ ] Update `docs/phase6/README.md` to add Slice 22 to the roadmap table.
- [ ] Update `docs/phase6/acceptance.md` to add the Slice 22 verification record to the
      per-slice evidence index.
- [ ] Add Slice 22 documents to the Phase 6 records block.
- [ ] Add ADR-043 to `docs/adr/README.md`.

## 6. Closeout

- [ ] Run `make check`, `make test-java`, `make test-go`, `make check-security`,
      `make catalog-check`.
- [ ] Record verification evidence in `verification.md` and link the new CI workflow run.
- [ ] Send a PR titled `feat(phase6): close transport hardening gap (slice 22)`.