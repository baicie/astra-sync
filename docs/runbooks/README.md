# Operational Runbook Templates

This directory is the canonical location for the AstraSync operational
runbook templates. The templates are the repository-side contribution
recorded by [ADR-046](../adr/adr-046-operational-runbook-templates.md).
Operators copy a template into their deployment-side wiki or runbook
store, fill in the placeholders, and own the populated copy thereafter.

The repository never holds a populated runbook. Populated runbooks are
deployment artefacts that may carry environment-specific URLs, account
IDs, pager rotation orders, and other values that have no place in a
public repository.

## Templates

| Template | Use it when |
|---|---|
| [idp-registration.md](idp-registration.md) | First-time Slice 18 enablement; new IdP onboarding; IdP issuer migration. |
| [key-rotation.md](key-rotation.md) | API Server / Console TLS certificates, OIDC JWKS keys, console session key, PostgreSQL TDE/SSL. |
| [session-revocation.md](session-revocation.md) | Lost device, suspected credential leak, departing employee, periodic drill. |
| [audit-retention.md](audit-retention.md) | Monthly partition rollover, partition detach, retention window change. |
| [backup.md](backup.md) | Daily / weekly snapshot cadence, restore drill, region failover input. |
| [rollback.md](rollback.md) | Bad release, schema regression, accidental destructive admin action. |

## How to populate

Every template uses the same skeleton: Purpose, Pre-conditions,
Procedure, Verification, Rollback, Security boundary. To populate a
template:

1. Copy the Markdown file to your deployment-side store.
2. Replace every `<placeholder>` value with the environment-specific
   value. The placeholders are wrapped in angle brackets so a global
   find-and-replace can find them, but no automatic tool is required.
3. Remove the `<!-- placeholders: ... -->` comment at the bottom of
   each template once all placeholders are resolved.
4. Version the populated runbook in your deployment-side system, not
   in the AstraSync repository.

## Pairing with repository artefacts

The templates cite the Slice 18 admin runbook and other repository-side
artefacts. Operators read those artefacts alongside the runbook because
the artefact describes a tool or interface and the runbook describes a
procedure.

- [Slice 18 admin runbook](../phase6/18-auth-rbac-audit/admin-runbook.md)
  — the `astra-auth-admin` CLI that several procedures call.
- [Slice 22 design](../phase6/22-transport-hardening/design.md) — the
  transport hardening decisions that key rotation and rollback inherit.
- [Phase 6 acceptance](../phase6/acceptance.md) — the verification
  evidence that drives the rollback criteria.

## CI guard

A repository check rejects any Markdown file in this directory that
does not contain at least one `<placeholder>` token or that contains a
known production hostname pattern. The check is best-effort and is not
a substitute for review. Operators are responsible for honouring the
boundary between template and populated runbook.