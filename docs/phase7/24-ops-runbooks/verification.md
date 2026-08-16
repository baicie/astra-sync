# Slice 24 Verification

## Test plan

The slice is verified by the following checks.

### Template integrity

- `make check-runbooks` walks every `*.md` file under
  `docs/runbooks/` and asserts that:
  - Each file contains at least one `<placeholder>` token.
  - Each file does not contain a known production hostname pattern.
- The check fails the gate if any assertion fails.

### Script unit tests

- `make test-scripts` runs
  `scripts/test_check_runbook_templates.py`, which exercises:
  - A template with placeholders and no hostnames (passes).
  - A template without placeholders (fails with a clear message).
  - A template with a known production hostname (fails with a clear
    message).
  - An empty directory (passes vacuously).

### Local checks

- `make check` runs the existing Go / Java / format checks plus the
  new `check-runbooks` gate.
- `make test-scripts` runs the script unit tests.

### CI checks

- The CI workflow `.github/workflows/ci.yml` runs `make check-runbooks`
  on every PR that touches `docs/runbooks/`. The workflow is gated by
  the existing `Repository policy checks` job so a CI failure blocks
  the merge.

## Evidence

The slice is a documentation delivery. The verification evidence is
the local `make check` and `make test-scripts` runs plus the CI run
on the PR.

| Check | Local | CI |
|---|---|---|
| `make check` | passes | passes |
| `make check-runbooks` | passes | passes |
| `make test-scripts` | passes | passes |

## Acceptance

The slice is accepted when:

1. `make check` passes locally and on the PR's CI run.
2. `make test-scripts` passes locally and on the PR's CI run.
3. The Phase 7 README marks Slice 24 as Implementation Complete.
4. The Phase 6 acceptance document closes the Slice 18 §6 checklist
   line via this slice (no further action required; the slice
   records the closure in `docs/phase7/24-ops-runbooks/README.md`).

## Out of scope

The slice does not verify:

- Operator-side runbook execution. The templates are deployment-owned
  artefacts; the operator's populated runbooks are not in the
  repository.
- Cross-environment runbook compatibility. The templates assume the
  Slice 18 admin CLI is deployed and the production gates from
  Slice 22 are closed. A deployment that has not enabled Slice 18
  or Slice 22 should not use these templates.

## References

- [ADR-046: Operational Runbook Templates](../../adr/adr-046-operational-runbook-templates.md)
- [ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria](../../adr/044-phase6-closeout-and-phase7-entry-criteria.md)
- [Slice 18 implementation plan §6](../../phase6/18-auth-rbac-audit/implementation-plan.md)