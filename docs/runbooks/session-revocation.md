# Session Revocation

## Purpose

Revoke one or more active Console sessions for a principal. The
procedure covers three triggers:

- A lost or stolen device.
- A suspected credential leak (for example, an OIDC refresh token
  discovered in a log file or a paste bin).
- A departing employee whose access the operator needs to remove
  before the off-boarding ticket closes.

The procedure does not revoke the IdP account itself; the operator
follows the IdP runbook for that. AstraSync only revokes the
downstream Console session and the audit-visible sign-in events that
the operator needs to record for the security review.

## Pre-conditions

- `<principal-id>` — the UUID of the principal whose sessions the
  operator needs to revoke. The UUID is recoverable from the audit
  table (`audit.subject_principal_id`) or from the IdP's user
  directory.
- `<operator-id>` — the UUID of the platform administrator running
  the revocation. The platform administrator must be authenticated
  through the IdP and must hold the `platform_admin` role in the
  deployment.
- `<revocation-reason>` — a short string the operator records in the
  security ticket (for example `device-lost`, `credential-leak`,
  `off-boarding`). The reason is not stored in AstraSync itself; the
  operator records it in the deployment-side ticket.
- `<vault-path-prefix>` — the Vault path prefix where the operator
  stores the session revocation evidence. The path is referenced by
  the security team but is not required by the revocation commands.
- The operator has a jump host with `astra-auth-admin` and `kubectl`
  installed.

## Procedure

1. **Confirm the principal's identity.** In the IdP admin console,
   verify that the operator has the right principal. A wrong UUID
   revokes an unrelated principal's sessions and may break a
   legitimate user's day.
   Expected output: the operator confirms the principal's email,
   display name, and tenant memberships in the IdP console.
   Rollback: stop. The operator does not run any revocation command.
2. **List the principal's active sessions.** Query the audit table
   for `sign-in` events for the principal in the last
   `<revocation-window-days>` days. The audit table records every
   sign-in with the session ID, the principal ID, the tenant ID, the
   IP address, and the user agent. The list is the evidence the
   operator cites in the security ticket.
   Expected output: a list of session IDs the operator needs to
   revoke.
   Rollback: stop. The list is informational.
3. **Revoke the sessions.** Run `astra-auth-admin revoke-session
   -principal-id <principal-id> -confirm` for each principal. The
   command deletes every Console session row for the principal. The
   next page load redirects to the IdP sign-in screen. Heartbeat
   capabilities are not affected; those belong to the data plane
   and the operator stops them by stopping the affected Jobs.
   Expected output: a confirmation message that lists the number of
   sessions deleted.
   Rollback: stop. The revocation is irreversible; the operator must
   restart the affected Jobs to clear heartbeat capabilities.
4. **Disable the principal (optional).** If the trigger is a
   credential leak or a departing employee, run
   `astra-auth-admin disable-principal -principal-id <principal-id>
   -confirm` after the revocation. The principal keeps the principal
   row but is denied on subsequent Bearer authentication and tenant
   authorization.
   Expected output: a confirmation message that lists the disabled
   memberships.
   Rollback: re-enable the principal by re-running `bootstrap-tenant`
   or `bootstrap-platform-admin`.
5. **Record the revocation.** Open the security ticket and record the
   principal UUID, the session IDs revoked, the trigger, and the
   operator UUID. The ticket is the durable record; AstraSync itself
   only records `revoke-session` and `disable-principal` audit events
   without the security-ticket reference.
   Expected output: a ticket ID the operator can cite in the
   post-incident review.
   Rollback: stop. The ticket is informational.
6. **Verify the revocation.** Sign in to the Console with a
   different principal and confirm the revoked principal's sessions
   no longer appear in the audit explorer's recent activity. The
   explorer filters by principal UUID, so the operator can confirm
   the audit-visible sign-ins match the revoked session list.
   Expected output: no active sessions for the revoked principal.
   Rollback: stop. The verification is informational.

## Verification

- `astra-auth-admin show-tenant -tenant-id <tenant-uuid>` does not
  list the revoked principal's memberships.
- The audit table contains a `revoke-session` event with the
  principal UUID and the operator UUID, plus a `disable-principal`
  event if step 4 was run.
- The Console sign-in page rejects a sign-in attempt with the
  revoked principal's credentials (Bearer token still valid at the
  IdP, denied at the API Server).
- The deployment-side ticket contains the principal UUID, the
  session IDs, the trigger, and the operator UUID.

## Rollback

The revocation is one-way. To re-enable a principal whose sessions
were revoked, the operator follows the IdP registration runbook's
bootstrap procedure with the same principal UUID. To re-enable a
disabled principal, the operator re-runs `bootstrap-tenant` or
`bootstrap-platform-admin` with the original flags; the command is
idempotent.

## Security boundary

- The revocation commands do not connect to the IdP. The operator
  confirms the principal's identity in the IdP before running the
  commands because the CLI accepts the principal UUID verbatim.
- The revocation does not delete the audit events. The audit table is
  append-only and the revocation events are the durable record of
  the action.
- The disable step is optional. A revoke without a disable leaves the
  principal able to sign in again; a disable without a revoke leaves
  the existing sessions valid until they expire. The operator
  chooses based on the trigger.
- The session IDs the operator records in the security ticket come
  from the audit table. The operator must redact the session IDs
  before pasting them into a public ticket system.

<!-- placeholders: principal-id, operator-id, revocation-reason, vault-path-prefix, revocation-window-days, tenant-uuid -->