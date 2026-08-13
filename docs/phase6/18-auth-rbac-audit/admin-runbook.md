# Slice 18.1 Operational Runbook

## Purpose

`astra-auth-admin` is the offline command-line utility that operators use to
perform first-time Slice 18 setup, recover lost membership, suspend tenants,
and inspect authorization revisions. The API Server intentionally does not
expose these flows; the same database is reachable only through the dedicated
deployment role configured for the API Server, the Console BFF, and the
Controller/Scheduler service actors.

This runbook assumes the deployment has already provisioned the dedicated
authentication PostgreSQL database and supplied the operator with a connection
URL. The utility never prompts for credentials and never logs secret material.

## Build

```sh
make build-auth-admin
```

The binary is written to `bin/astra-auth-admin`. Copy it to the operator jump
host that has network access to the authentication database.

## Common flags

| Flag | Required | Meaning |
|---|---|---|
| `-database-url URL` | Recommended | PostgreSQL DSN. Falls back to `$ASTRASYNC_AUTH_DATABASE_URL`. |
| `-confirm` | Required for any mutation | Prevents accidental execution. |

The utility calls `Migrate()` on every invocation so an operator can recover
from a fresh schema by simply running the intended bootstrap command.

## Operations

### bootstrap-tenant

```sh
astra-auth-admin bootstrap-tenant \
  -tenant-id 11111111-1111-4111-8111-111111111111 \
  -namespace team-a \
  -display-name "Team A" \
  -oidc-issuer https://idp.example/ \
  -oidc-subject operations-lead@example.com \
  -confirm
```

The tenant is created ACTIVE, the principal is materialized (creating a row if
it has not authenticated yet) and granted `tenant_admin` in one transaction.
The same command can be re-run for idempotent recovery; the second invocation
matches the existing tenant and returns success without modification.

### bootstrap-platform-admin

```sh
astra-auth-admin bootstrap-platform-admin \
  -oidc-issuer https://idp.example/ \
  -oidc-subject platform-owner@example.com \
  -confirm
```

The principal must have authenticated at least once so that the principal row
exists. Re-running flips a disabled assignment back to ACTIVE.

### disable-principal

```sh
astra-auth-admin disable-principal -principal-id <UUID> -confirm
```

The principal keeps its principal row but is denied on subsequent Bearer
authentication and tenant authorization. Existing Console sessions are not
removed by this command; combine with `revoke-session` to clean them up.

### revoke-session

```sh
astra-auth-admin revoke-session -principal-id <UUID> -confirm
```

Deletes every Console session row for the principal. The next page load
redirects to login. Heartbeat capabilities are not affected; those belong to
the data plane and are managed through the Coordinator and Scheduler.

### show-tenant

```sh
astra-auth-admin show-tenant -tenant-id <UUID>
```

Prints the tenant namespace, display name, status, current `authz_revision`,
and one line per membership (principal UUID, role, status).

### suspend-tenant / reactivate-tenant

```sh
astra-auth-admin suspend-tenant   -tenant-id <UUID> -confirm
astra-auth-admin reactivate-tenant -tenant-id <UUID> -confirm
```

A suspended tenant keeps its rows but every tenant permission returns
`PERMISSION_DENIED` at the API Server. Running Jobs are not stopped by
suspension; that is an explicit administrative operation performed by stopping
the affected Jobs first.

### show-revision

```sh
astra-auth-admin show-revision -tenant-id <UUID>
```

Prints the current `authz_revision`. Membership and platform-role changes
increment this revision. Authorization caches invalidate after this revision
changes.

## Recovery Scenarios

| Scenario | Recovery |
|---|---|
| Lost access to the only tenant administrator | Bootstrap a new tenant administrator principal via `disable-principal`+`revoke-session` of the lost principal (after verifying their status). Then `bootstrap-platform-admin` against the same subject, then `bootstrap-tenant` again with the original tenant ID. |
| IdP key rotation caused false `unknown_kid` denials | Operators rely on the deployment-side stale JWKS window. The CLI does not affect JWKS. Re-run `show-revision` to confirm tenants are still reachable, then re-enable the deployment. |
| Suspended tenant accidentally | `reactivate-tenant`. |
| Stale `authz_revision` in caches | The CLI does not write to caches. Operators may trigger cache invalidation by issuing a no-op `bootstrap-tenant` re-run with `-confirm`. |

## Security Boundary

- All operations run as the same PostgreSQL role used by the API Server and
  Console BFF. The database user therefore inherits their privileges. Do not
  run `astra-auth-admin` from a session that also has DDL or unrestricted
  grant authority.
- Identity claims are never logged. The CLI prints only UUIDs, namespaces,
  roles, statuses, and revisions.
- Mutating operations require `-confirm`. The flag is intentionally a separate
  argument so shell history alone is insufficient.
- The CLI does not connect to the OIDC provider. Identity claims are taken
  verbatim from the flag and assumed to match a subject that has previously
  authenticated through the API Server.

## Verification

`make test-auth-admin` exercises the parameter validation, UUID/issuer
checks, and unknown operation handling without touching PostgreSQL.
PostgreSQL integration is covered by `control-plane/auth/postgres` against
the standard test database URL.
