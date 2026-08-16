# Slice 24 Implementation Plan

## Work breakdown

The slice is delivered as five commits. Each commit is reviewable
in isolation and passes the local check.

### Commit 1 — ADR

- Add `docs/adr/adr-046-operational-runbook-templates.md`.
- Update `docs/adr/README.md` to index ADR-046.

### Commit 2 — Runbook templates

- Add `docs/runbooks/README.md`.
- Add `docs/runbooks/idp-registration.md`.
- Add `docs/runbooks/key-rotation.md`.
- Add `docs/runbooks/session-revocation.md`.
- Add `docs/runbooks/audit-retention.md`.
- Add `docs/runbooks/backup.md`.
- Add `docs/runbooks/rollback.md`.

### Commit 3 — CI guard

- Add `scripts/check-runbook-templates.py`.
- Add `scripts/test_check_runbook_templates.py`.
- Update `Makefile` to add `make check-runbooks` and to chain it
  into `make check`.
- Update `.github/workflows/ci.yml` to run `make check-runbooks`
  on every PR that touches `docs/runbooks/`.

### Commit 4 — Slice records

- Add `docs/phase7/24-ops-runbooks/README.md`.
- Add `docs/phase7/24-ops-runbooks/design.md`.
- Add `docs/phase7/24-ops-runbooks/implementation-plan.md`.
- Add `docs/phase7/24-ops-runbooks/verification.md`.

### Commit 5 — Phase 7 README + index

- Update `docs/phase7/README.md` to mark Slice 24 as Implementation
  Complete and to point at the slice's records.

## Verification steps

After the last commit, the operator runs:

```sh
make check
make test-scripts
```

The first command runs the existing Go/Java/format checks plus the
new `check-runbooks` gate. The second command runs the
`test_check_runbook_templates.py` unit tests.

The slice is ready to merge when:

- `make check` passes locally.
- `make test-scripts` passes locally.
- The CI run on the PR passes.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| A populated runbook slips into a future PR. | The CI check rejects populated runbooks at the file level. The check is best-effort but catches the common cases. |
| An operator cannot find the templates. | The Phase 7 README points at `docs/runbooks/README.md`; the README is the single entry point. |
| The templates drift from the underlying tools. | The templates cite the Slice 18 admin runbook, the Slice 22 design, and the Phase 6 acceptance. A change to those artefacts requires a corresponding change to the templates. The PR description for any future Slice 18 / 22 / 21 / 24 follow-up must call out the runbook impact. |
| The CI check has a false positive. | The check is unit-tested. A false positive is fixable in minutes. |

## Rollout

The slice does not require a deployment rollout. The templates are
documentation; the CI check is local. The slice ships when the PR
merges.

## Open questions

None. The slice scope is documented in ADR-046 and the design.