# Phase 16: CI / Test Hygiene & Release Tooling

## Status

**Complete.** Phase 16 closes four hygiene gaps left by Phases 13–15:
the CI step regression tests did not cover the Phase 13–15 additions,
the runbook template guard did not scan the Phase 14/15 operator guides,
the CHANGELOG was out of sync, and there was no release dry-run tooling.

## Goals

1. Extend `scripts/test_ci_workflow.py` with regression tests for every CI step
   added in Phases 13, 14, and 15 (production profile, staging profile,
   ArgoCD schema validation, catalog-check with diff fallback).
2. Extend `scripts/check-runbook-templates.py` to scan the Phase 14 ArgoCD
   onboarding README and the Phase 15 catalog authoring guide, with a new
   `template` vs `doc` mode per root.
3. Add `scripts/check-changelog.py` to guard that every `**Complete.**` phase
   is referenced in `## [Unreleased]`.
4. Add `scripts/release-dry-run.py` for dry-run release validation.
5. Add `make check-docs` and `make release-dry-run` Makefile targets.
6. Update `make check` to include `check-docs`.
7. Add the CI `check-docs` job for the `docs` change scope.
8. Produce a Phase 15 completion record.
9. Update `CHANGELOG.md` with Phase 13, 14, and 15 entries.

## Non-goals

- Changing any application code, protocol buffers, or Helm chart behaviour.
- Replacing the existing `make check-runbooks` with a different tool.
- Generating the CHANGELOG automatically from git commits.
- Running the CI workflow locally via `act`.

## Roadmap

| Slice | Description | Status |
|-------|-------------|--------|
| 42.1 | ADR-056: CI / test hygiene & release tooling decision | Done |
| 42.2 | `scripts/test_ci_workflow.py`: +8 regression tests for Phase 13-15 CI steps | Done |
| 42.3 | `scripts/check-runbook-templates.py`: new `--all` mode, template/doc per root | Done |
| 42.4 | `scripts/check-changelog.py`: guard `## [Unreleased]` coverage | Done |
| 42.5 | `scripts/release-dry-run.py`: dry-run release checklist | Done |
| 42.6 | Makefile: `check-docs` + `release-dry-run` targets | Done |
| 42.7 | CI: `check-docs` job in `docs` scope | Done |
| 42.8 | Phase 15 completion record | Done |
| 42.9 | CHANGELOG.md: Phase 13/14/15 entries | Done |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| `test_ci_workflow.py` covers 4 Phase 13-15 CI steps (production, staging, ArgoCD, catalog) | Done (11 tests pass) |
| `check-runbook-templates.py --all` scans ArgoCD README and catalog-authoring.md | Done (26 tests pass) |
| `check-changelog.py` exits 1 with actionable errors for Phase 13/14/15 | Done (verified) |
| `release-dry-run.py` reports Maven version, git SHA, proto inventory | Done |
| `make check` includes `check-docs` | Done |
| CI `check-docs` job runs on `docs` scope changes | Done |
| `CHANGELOG.md` includes Phase 13/14/15 in `## [Unreleased]` | Done |

## Records

- [Design](42-ci-hygiene/README.md)
- [ADR-056](../adr/adr-056-ci-hygiene-and-release-tooling.md)
- [ADR-055](../adr/adr-055-connector-catalog-lifecycle-automation.md) (Phase 15)
- [ADR-054](../adr/adr-054-argocd-gitops-integration.md) (Phase 14)

## Operational Impact

Before Phase 16:
- Phase 13-15 CI step additions were not regression-tested.
- Operator guides and onboarding docs could drift into production hostnames
  without any guard.
- CHANGELOG was out of sync; no tool caught Phase 13/14/15 entries missing.
- Release day required manual verification of version, catalog, proto, CHANGELOG.

After Phase 16:
- Every CI step addition is locked in by a unit test that fires on any
  silent removal or refactor.
- Every operator-facing document is scanned for production hostname patterns
  and placeholder tokens.
- `make check-docs` catches CHANGELOG drift on every commit.
- `make release-dry-run` prints a complete release checklist before any tag.
