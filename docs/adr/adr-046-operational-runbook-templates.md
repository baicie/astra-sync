# ADR-046: Operational Runbook Templates (Phase 7 Slice 24)

## Status

Accepted. Implements Phase 7 Slice 24 and closes the "Operational runbook
templates under `docs/runbooks/`" entry criterion recorded by ADR-044 §
"Phase 7 entry criteria" §2.

## Context

ADR-044 lists four Phase 7 entry criteria. Slice 23 closed the first
(cross-cluster control-plane mTLS, ADR-045). Slice 24 closes the second:

> Operational runbook templates. The Slice 18 implementation plan tracked
> IdP registration, key rotation, session revocation, audit retention,
> backup, and rollback runbooks as deployment artefacts. Phase 7 supplies
> a template under `docs/runbooks/` that an operator populates per
> environment. The template is the only repository-side contribution; the
> populated versions stay out of source control.

The Phase 6 acceptance document (`docs/phase6/acceptance.md`) called these
items out as deployment-owned, and the Slice 18 implementation plan §6
("Transport, Deployment, and Rollout") put them on the closure checklist
without naming a repository deliverable. ADR-044 captured the boundary
explicitly so that the closeout of Phase 6 could be honest about the
handoff.

This ADR names the six templates, fixes the boundary between the template
and the populated runbook, and explains why the populated runbook must
stay out of source control.

## Decision

### Six templates under `docs/runbooks/`

The repository adds six runbook templates under a new top-level
`docs/runbooks/` directory. Each template is a single Markdown file that
documents a procedure an operator performs in a production environment.
The templates are the only repository-side contribution. Operators copy a
template into their deployment-side store, fill in environment-specific
values, and own the resulting runbook thereafter.

| Template | File | Trigger |
|---|---|---|
| IdP registration | `idp-registration.md` | First-time Slice 18 enablement; new IdP onboarding; IdP issuer migration. |
| Key rotation | `key-rotation.md` | API Server / Console TLS certificates, OIDC JWKS keys, console session key, PG TDE/SSL. |
| Session revocation | `session-revocation.md` | Lost device, suspected credential leak, departing employee, drill. |
| Audit retention | `audit-retention.md` | Monthly partition rollover, partition detach, retention window change. |
| Backup | `backup.md` | Daily / weekly snapshot cadence, restore drill, region failover input. |
| Rollback | `rollback.md` | Bad release, schema regression, accidental destructive admin action. |

The six lines mirror the checklist in ADR-044 §2 and Slice 18
implementation plan §6. Role recovery is folded into IdP registration
because both share the same principal-revocation and re-bootstrap
mechanism.

### Template shape

Every template has the same skeleton so an operator can populate any
template by answering the same set of prompts:

1. **Purpose** — what the runbook achieves and which ADR/PRD it pairs
   with.
2. **Pre-conditions** — environment-specific values the operator must
   supply (control-plane version, OIDC issuer, database role, Vault
   path, etc.).
3. **Procedure** — ordered steps, each step a single operator action
   with an expected output and a rollback hook.
4. **Verification** — how the operator confirms the procedure
   succeeded, with concrete queries and expected counts.
5. **Rollback** — what to do if a step fails or the change has to be
   undone.
6. **Security boundary** — what the procedure must not do (e.g. log
   secrets, share session material, rotate keys without overlap).

Each template is short. The "Procedure" sections list commands and
expected outputs; they do not embed environment-specific URLs, account
names, or credentials. The templates cite the Slice 18 admin runbook
(`docs/phase6/18-auth-rbac-audit/admin-runbook.md`) and other
repository-side artefacts that operators read alongside the runbook.

### Boundary between template and populated runbook

The template is a repository artefact. The populated runbook is a
deployment artefact. The split is enforced by content shape, not by
mechanism:

- The template is parameterised by `<placeholder>` values such as
  `<prod-control-plane-url>` and `<vault-path-prefix>`. An operator
  replaces each placeholder with the environment's value.
- The populated runbook holds non-portable values (URLs, account IDs,
  role names, pager rotation order). The repository must never see
  those values because they are deployment-owned, may carry information
  the operator's change-management process labels as restricted, and
  vary by environment in ways that have no benefit to anyone reading the
  repository.

The repository therefore ships templates only. Operators fork the
template into their deployment-side wiki, ticketing system, or runbook
store, populate it, and version the populated copy outside the
repository.

### Index

`docs/runbooks/README.md` lists the six templates and links to the
Phase 6 artefacts that each template builds on. The index is the
single entry point for an operator who needs to find a template; the
template files themselves are the deliverable.

### Verification

A repository check confirms that every file in `docs/runbooks/` is a
template: it must contain at least one `<placeholder>` and must not
contain the strings `astra-prod`, `prod-control-plane`, or any other
known production hostname pattern. The check is added to the existing
`make check` gate so a stray populated runbook fails CI before it
lands. The check is best-effort — it cannot enumerate every hostname —
so the ADR relies on operators to honour the boundary in code review.

## Consequences

- `docs/runbooks/` is the canonical location for the six templates. A
  Phase 7 operator looking for a template goes there, not to
  `docs/phase6/18-auth-rbac-audit/admin-runbook.md`. The Slice 18 admin
  runbook stays in place because it documents the `astra-auth-admin`
  binary, not a deployment procedure.
- `docs/phase7/24-ops-runbooks/` records the slice decision, design,
  implementation plan, and verification evidence. The slice is a
  documentation-only delivery; there is no Go or Java code in the PR.
- The Slice 18 implementation plan §6 checklist line "Document IdP
  registration, bootstrap, key rotation, role recovery, session
  revocation, audit retention, backup, and rollback runbooks" closes
  via this slice.
- The Phase 7 README updates the Slice 24 row from Design to
  Implementation Complete. Slice 25 (multi-region) and Slice 26
  (observability) are not affected by this ADR.

## Alternatives considered

- **Ship populated runbooks for a reference deployment.** Rejected. A
  reference deployment pins one IdP, one cloud, and one operator team.
  The other operators cannot reuse the populated values; they copy the
  template and rewrite anyway. Shipping populated values also commits
  the repository to a specific IdP and database provider, which ADR-036
  (external OIDC) and ADR-007 (three-storage model) deliberately keep
  open.
- **Build a runbook generator that ingests deployment values.** Rejected.
  The generator becomes a deployment tool, which is the wrong layer for
  the repository. The boundary in ADR-044 is repository-vs-deployment;
  a generator collapses the two.
- **Push the templates into the existing Slice 18 admin-runbook
  document.** Rejected. The admin-runbook documents the
  `astra-auth-admin` binary, not deployment procedures. Mixing the two
  forces an operator to read a binary reference to find the IdP
  registration steps. ADR-044's "deployment-owned artefact" language is
  also cleaner when the templates live in their own directory.