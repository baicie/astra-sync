# Phase 6 Slice 22 Threat-Model Delta

## New residual risks accepted by the slice

- An operator that forgets to populate `TRUSTED_PROXY_CIDRS` in production will see the
  binary refuse to start. The slice prefers false negatives over a permissive
  default; documentation calls this out in the rollout section.
- TLS termination now happens inside the binary as well as at the ingress. Operators
  who rely on the ingress for cipher policy must either run TLS twice (with mutual
  trust on the inner hop) or migrate cipher policy into the binary configuration. The
  slice does not change cipher suites from the existing `tls.VersionTLS12` baseline.

## Threats closed

| Threat | Before | After |
|---|---|---|
| Plaintext bearer token interception at the API Server gRPC port | Possible in production when only the HTTP gateway had TLS. | Refused: the gRPC listener also requires TLS in production. |
| Plaintext Console BFF traffic | Possible; the listener never used `ListenAndServeTLS`. | Refused in production. |
| Spoofed client IP via `X-Forwarded-For` from an untrusted peer | Always accepted. | Rejected; the immediate peer wins. |
| Spoofed scheme via `X-Forwarded-Proto` from an untrusted peer | Always accepted. | Rejected; the locally observed scheme wins. |
| HSTS stripping on a downgradeable response | Not set by the application. | Set whenever the listener terminates TLS or the trusted proxy declared `https`. |
| MIME-sniffing on JSON or HTML responses | Possible. | Disabled by `X-Content-Type-Options: nosniff`. |
| Cross-origin referrer leakage | Possible. | Disabled by `Referrer-Policy: strict-origin-when-cross-origin`. |

## Threats unchanged

- All threats listed in `docs/phase6/18-auth-rbac-audit/threat-model.md` outside the
  transport boundary.
- IdP registration, key rotation, and session-revocation operational risk. Those
  remain in the Phase 6 closeout backlog.

## Verification artefacts

- `verification.md` collects the new CI gate evidence and the local
  `control-plane/auth/transport` test output.
- The Phase 6 acceptance document links the verification record.