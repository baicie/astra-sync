# Slice 24 Design

## Scope

Slice 24 ships six runbook templates and an index. The templates
cover the procedures an operator performs in a production
environment, paired with the repository-side artefacts (admin CLI,
Helm chart, audit explorer) the operator reads alongside them.

The slice is documentation-only. There is no Go or Java code in the
PR. The only non-document artefact is a CI check that rejects
populated runbooks.

## Template selection

ADR-044 §"Phase 7 entry criteria" §2 names six runbook topics. The
Slice 18 implementation plan §6 names seven (the same six plus role
recovery). The slice consolidates the seven into six templates by
folding role recovery into IdP registration; both share the same
principal-revocation and re-bootstrap mechanism, and the IdP
registration template already covers the `disable-principal` and
`bootstrap-platform-admin` commands.

The six templates are:

| Template | File |
|---|---|
| IdP registration | `idp-registration.md` |
| Key rotation | `key-rotation.md` |
| Session revocation | `session-revocation.md` |
| Audit retention | `audit-retention.md` |
| Backup | `backup.md` |
| Rollback | `rollback.md` |

The templates pair with the existing repository-side artefacts:

| Repository artefact | Template that cites it |
|---|---|
| [Slice 18 admin runbook](../../phase6/18-auth-rbac-audit/admin-runbook.md) | IdP registration, session revocation |
| [Slice 21 audit explorer](../../phase6/21-audit-explorer/README.md) | Session revocation, audit retention |
| [Slice 22 design](../../phase6/22-transport-hardening/design.md) | Key rotation, rollback |
| [Phase 6 acceptance](../../phase6/acceptance.md) | Rollback |

## Template shape

Every template uses the same skeleton so an operator can populate any
template by answering the same set of prompts:

1. **Purpose** — what the runbook achieves and which ADR/PRD it
   pairs with.
2. **Pre-conditions** — environment-specific values the operator
   must supply.
3. **Procedure** — ordered steps, each step a single operator
   action with an expected output and a rollback hook.
4. **Verification** — how the operator confirms the procedure
   succeeded.
5. **Rollback** — what to do if a step fails or the change has to
   be undone.
6. **Security boundary** — what the procedure must not do.

The placeholders are wrapped in angle brackets and listed in an HTML
comment at the bottom of each template. The comment is a reminder
for the operator to remove it once all placeholders are resolved.

## Boundary between template and populated runbook

The boundary is enforced by content shape, not by mechanism:

- The template contains `<placeholder>` values. The populated
  runbook contains the operator's environment-specific values.
- The repository never holds a populated runbook. A populated
  runbook may carry environment-specific URLs, account IDs, pager
  rotation orders, and other values that have no place in a public
  repository.

The CI check enforces the boundary at the file level. The check
rejects any Markdown file in `docs/runbooks/` that lacks a
placeholder or that contains a known production hostname pattern
(`astra-prod`, `prod-control-plane`, etc.). The check is
best-effort; it cannot enumerate every hostname. The repository
relies on operator discipline and code review to honour the
boundary.

## CI check

The CI check is a Python script invoked from `make check` (or the
existing `make check-security` gate, if a security-side check is
preferred). The script:

1. Walks every `*.md` file under `docs/runbooks/`.
2. Asserts that each file contains at least one `<placeholder>`
   token. The token matches the regex `<\w[\w-]*>`.
3. Asserts that each file does not contain any of the known
   production hostname patterns. The patterns are listed in
   `scripts/check-runbook-templates.py`.
4. Fails the gate if any assertion fails. The failure message
   names the file and the failing assertion.

The check is a single Python file under `scripts/`. The script is
unit-tested by `make test-scripts` to confirm that:

- A template with placeholders and no hostnames passes.
- A template without placeholders fails with a clear message.
- A template with a known production hostname fails with a clear
  message.

## Index

`docs/runbooks/README.md` is the single entry point. The index
lists the six templates, the procedure for populating them, the
repository artefacts the templates pair with, and a description of
the CI guard.

## Verification

The slice is verified by:

- Reading the templates against the Slice 18 implementation plan
  §6 checklist. The checklist line "Document IdP registration,
  bootstrap, key rotation, role recovery, session revocation,
  audit retention, backup, and rollback runbooks" closes via this
  slice.
- Running the CI check locally and confirming it passes on the
  shipped templates.
- Running the CI check with a deliberately populated runbook and
  confirming it fails with a clear message.
- Reviewing each template against the security boundary section to
  confirm it documents what the procedure must not do.

## Future work

- Slice 25 (multi-region) will inherit the backup template's
  region-failover input procedure. The slice records this in
  ADR-046's Consequences section.
- Slice 26 (observability) will add a runbook template for
  dashboard-on-call rotation. The template is out of scope for
  Slice 24.