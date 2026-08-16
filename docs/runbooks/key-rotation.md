# Key Rotation

## Purpose

Rotate the cryptographic material AstraSync depends on without causing
an outage. The four key families are:

- API Server / Console TLS certificates (`TLS_CERTIFICATE_FILE`,
  `TLS_PRIVATE_KEY_FILE`, `CONSOLE_TLS_CERTIFICATE_FILE`,
  `CONSOLE_TLS_PRIVATE_KEY_FILE`).
- Slice 23 control-plane mTLS material (`MTLS_CLIENT_CA_FILE`,
  `CONSOLE_API_CLIENT_CERT_FILE`, `CONSOLE_API_CLIENT_KEY_FILE`).
- Console session key (`CONSOLE_SESSION_KEY`).
- IdP JWKS keys (`OIDC_ISSUER` advertised keys).

The procedure keeps the old and new material live during the rotation
window so the API Server, Console, and IdP can switch over without
breaking active sessions or in-flight gRPC streams.

## Pre-conditions

- `<control-plane-version>` — the deployed AstraSync control plane
  release. The rotation cadence differs by version; the release notes
  document the supported overlap window.
- `<vault-path-prefix>` — the Vault path prefix where the operator
  stores TLS private keys, client certificates, and the console
  session key.
- `<idp-issuer-url>` — the production IdP issuer URL.
- `<control-plane-public-url>` and `<api-server-public-url>` — the
  external URLs the certificates must SAN-match.
- The operator has a jump host with `openssl`, `kubectl`, and Vault
  access installed.

## Procedure

1. **Issue the new material.** Generate a new server certificate,
   client certificate, and private key for each family. The new
   certificate must SAN-match the same external URLs the current
   certificate SAN-matches. Store the new private key under
   `<vault-path-prefix>/rotation/<rotation-id>/<family>`.
   Expected output: a new key pair written to Vault, the old key pair
   still present at the original Vault path.
   Rollback: stop. The new material never reaches the deployment.
2. **Deploy the new material alongside the old material.** Update the
   API Server and Console deployments to mount both the new and the
   old certificates. The API Server TLS listener serves the new
   certificate while continuing to verify the old client CA bundle.
   The Console mTLS client presents the new certificate while
   continuing to trust the old CA bundle.
   Expected output: a rolling restart that completes without the
   production TLS gate tripping.
   Rollback: revert the deployment to the previous release.
3. **Switch the IdP JWKS.** Publish the new JWKS alongside the old
   JWKS at `<idp-issuer-url>/.well-known/jwks.json`. The IdP must keep
   both keys advertised during the rotation window. The API Server
   cache invalidates after the configured TTL; the operator can
   shorten the TTL before the rotation to accelerate the switchover.
   Expected output: the JWKS document now contains the new `kid` value
   alongside the old `kid`.
   Rollback: remove the new JWKS from the IdP. The API Server
   continues to verify tokens with the old key.
4. **Roll the console session key.** Replace the value of
   `CONSOLE_SESSION_KEY` in the Console deployment. Existing sessions
   become unreadable because the key changed; the operator must
   communicate the sign-out to the user base.
   Expected output: every active Console session returns an
   `invalid_session` error on the next heartbeat.
   Rollback: restore the old `CONSOLE_SESSION_KEY`. Sessions that
   were already invalidated stay invalidated; sessions that had not
   yet heartbeated remain valid.
5. **Remove the old material.** After the rotation window expires
   (typically 24 hours, configured per environment), remove the old
   certificate, client CA bundle, and JWKS key.
   Expected output: the Vault path no longer contains the old key
   pair; the IdP JWKS no longer advertises the old `kid`.
   Rollback: re-publish the old key. The rotation reverses.

## Verification

- `openssl s_client -connect <api-server-public-url>:443 -servername <api-server-public-url>`
  shows the new certificate. Repeat for the Console URL.
- `openssl x509 -in <new-server-cert.pem> -noout -text` shows a
  `notAfter` value later than the old certificate.
- The API Server logs contain no `unknown_kid` denials during the
  rotation window.
- `astra-auth-admin show-tenant -tenant-id <tenant-uuid>` still
  succeeds after the console session key rotation; this confirms the
  API Server's session cache is unaffected.
- The IdP's JWKS endpoint returns the new key alongside the old key
  until the rotation window closes.

## Rollback

Every step has an in-place rollback hook. The overall rollback is to
revert the deployment to the previous release and restore the old
Vault path. The IdP JWKS rollback is to remove the new key from the
JWKS document; the API Server falls back to the old key
immediately.

## Security boundary

- Private keys never enter the operator's shell history. The operator
  copies them into Vault using the Vault CLI, which does not echo the
  key material.
- The rotation window is operator-defined per environment. A window
  shorter than the API Server's JWKS cache TTL causes a denial
  cascade; the operator must check the TTL before shortening the
  window.
- The console session key rotation invalidates every active session.
  Operators schedule the rotation for low-traffic windows.
- The IdP JWKS rotation is the highest-risk step because a misstep
  denies every token. The operator keeps both keys advertised for the
  duration of the window and removes the old key last.

<!-- placeholders: control-plane-version, vault-path-prefix, idp-issuer-url, control-plane-public-url, api-server-public-url, rotation-id, tenant-uuid -->