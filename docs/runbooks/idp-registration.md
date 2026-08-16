# IdP Registration

## Purpose

Register AstraSync as an OAuth/OIDC client of the production identity
provider, then bootstrap the first tenant and platform administrator.
The procedure covers both the first-time Slice 18 enablement recorded
in the Phase 6 acceptance document and the IdP issuer migration that an
operator performs when the IdP itself changes (for example, an Okta to
Entra ID migration, or a tenant-to-tenant rename inside the same IdP).

This runbook pairs with the Slice 18 admin runbook, which documents
the `astra-auth-admin` CLI used by the bootstrap steps.

## Pre-conditions

- `<control-plane-version>` — the deployed AstraSync control plane
  release (for example `v0.42.0`). The IdP client registration must
  match the version's expected `redirect_uri` and scope values; both
  have changed across releases.
- `<idp-issuer-url>` — the production IdP issuer URL. The IdP must
  expose a JWKS endpoint and must support RS256-signed JWTs.
- `<control-plane-public-url>` — the external URL of the Console BFF
  (for example `https://astrasync.example.com`). The Console uses this
  URL as the OIDC `redirect_uri`.
- `<api-server-public-url>` — the external URL of the API Server (for
  example `https://api.astrasync.example.com`). The API Server uses
  this URL as the OIDC audience.
- `<vault-path-prefix>` — the Vault path prefix where the operator
  stores OIDC client secrets and the console session key. The path
  must be readable by the API Server and Console service identities.
- The operator has a jump host with `astra-auth-admin` and `kubectl`
  installed and credentials for the production IdP tenant.

## Procedure

1. **Create the OIDC client.** In the IdP admin console, register a
   new confidential client. Use `<control-plane-public-url>/auth/callback`
   as the redirect URI and `openid`, `profile`, `email` as scopes.
   Generate a client secret and store it under
   `<vault-path-prefix>/idp/client-secret`.
   Expected output: a client ID and a client secret written to the
   operator's secret store.
   Rollback: delete the client. There is no durable state in
   AstraSync yet.
2. **Configure the API Server.** Set
   `OIDC_ISSUER=<idp-issuer-url>`, `OIDC_AUDIENCE=<api-server-public-url>`,
   and point `AUTH_MODE=oidc` in the API Server deployment. The
   `OIDC_ISSUER` value must match the issuer URL the IdP publishes in
   its discovery document byte-for-byte.
   Expected output: the API Server starts without the production OIDC
   gate tripping. The startup log prints `OIDC issuer validated`
   when the issuer matches.
   Rollback: revert the deployment to the previous release.
3. **Configure the Console.** Set `CONSOLE_OIDC_ISSUER=<idp-issuer-url>`
   and the equivalent client credentials in the Console deployment.
   Expected output: the Console sign-in page redirects to the IdP
   login screen on first load.
   Rollback: revert the Console deployment.
4. **Bootstrap the platform administrator.** Run the
   `bootstrap-platform-admin` command documented in the Slice 18 admin
   runbook. The principal must have authenticated at least once so the
   principal row exists.
   Expected output: a single row in the membership table with role
   `platform_admin` and status ACTIVE.
   Rollback: `disable-principal` and `revoke-session` against the
   principal.
5. **Bootstrap the first tenant.** Run `bootstrap-tenant` with the
   desired tenant UUID, namespace, display name, OIDC issuer, and OIDC
   subject. The command is idempotent; re-running it does not
   duplicate rows.
   Expected output: a tenant row in ACTIVE state, a principal row, and
   a `tenant_admin` membership.
   Rollback: `suspend-tenant`.
6. **Smoke test the sign-in flow.** Open the Console in a private
   browser window. Sign in with the bootstrapped platform
   administrator's IdP account. Confirm the tenant list renders and
   the audit explorer (Slice 21) records the sign-in event.
   Expected output: a `sign-in` event in the audit table with the
   principal UUID and the tenant UUID.
   Rollback: `revoke-session` to invalidate the session.

## Verification

- `astra-auth-admin show-tenant -tenant-id <tenant-uuid>` prints the
  tenant row.
- `astra-auth-admin show-revision -tenant-id <tenant-uuid>` returns
  the current `authz_revision`. The revision must be greater than 0
  after the bootstrap.
- The IdP's audit log shows one sign-in event for the platform
  administrator and one sign-in event for the tenant administrator.
- The PostgreSQL audit table contains the `create-tenant`,
  `grant-tenant-admin`, and `sign-in` events in chronological order.

## Rollback

If any step fails before the bootstrap, revert the deployment
configuration and delete the IdP client. If the bootstrap succeeds and
the operator needs to undo it, run `suspend-tenant` followed by
`revoke-session` for every principal the bootstrap granted. The
tenant row stays so the operator can re-activate it later.

## Security boundary

- Client secrets live in Vault. They never enter the operator's shell
  history, the deployment manifests, or the repository.
- The OIDC issuer URL must match the IdP discovery document
  byte-for-byte. A typo causes the API Server to reject every token
  with `unknown_issuer` and to deny every sign-in.
- The bootstrap commands accept the OIDC subject verbatim. The
  operator must verify the subject by signing in with the IdP
  account first; the CLI does not connect to the IdP.
- The platform administrator principal is the highest-privilege
  principal in the deployment. The operator must record the principal
  UUID in the deployment-side vault and rotate it according to the
  key rotation runbook.

<!-- placeholders: control-plane-version, idp-issuer-url, control-plane-public-url, api-server-public-url, vault-path-prefix, tenant-uuid -->