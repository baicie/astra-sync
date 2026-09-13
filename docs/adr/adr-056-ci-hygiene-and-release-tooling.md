# ADR-056: CI / Test Hygiene & Release Tooling

## Status

Accepted

## Context

Phase 13 (production hardening) added a Helm CI step. Phase 14 (ArgoCD GitOps)
added two more CI steps (staging profile validation and ArgoCD CR schema
validation). Phase 15 (catalog lifecycle automation) added a third category
of behaviour: a `catalog-check` step that, on failure, invokes
`scripts/diff-catalog.py` to print categorised diagnostics. None of these
additions are covered by `scripts/test_ci_workflow.py`, which still asserts
only the original Phase 12 contract (third-party actions pinned, scope
outputs declared, multi-region job teardown on failure).

Three concrete problems limit the hygiene story today:

1. **CI step additions are not regression-tested.** Phase 13-15 added four
   CI steps (`Lint and render production profile`, `Lint and render staging
   profile`, `Validate ArgoCD CR schema`, `Check deployment connector
   inventory` with `diff-catalog.py` fallback). If a future refactor
   silently removes one of these steps or short-circuits the catalog
   fallback, `test_ci_workflow.py` will not notice.

2. **The runbook template guard does not cover Phase 14/15 documentation
   surfaces.** `scripts/check-runbook-templates.py` scans `docs/runbooks`,
   `docs/observability`, and `deployment/helm/astrasync/templates/multi-region`.
   Phase 14 added `deployment/argocd/README.md` (operator onboarding
   template) and Phase 15 added `docs/catalog-authoring.md` (connector
   authoring guide). Neither is currently scanned for the placeholder /
   production-hostname rules recorded by ADR-046.

3. **The repository has no release dry-run tooling.** The release flow is:
   bump the Maven `<version>` in `pom.xml`, regenerate
   `deployment/catalog/connector-inventory.pb` against the new commit,
   regenerate `.proto` bindings, push a tag. None of this is exercised by a
   script before the actual release. A release that forgot one of these
   steps would not be caught until the deployment side complains.

In addition, the `CHANGELOG.md` `## [Unreleased]` section has been empty
since v0.2.0 was tagged, even though Phase 13-15 added five commits since.
The repository governance rule recorded in the AstraSync process guideline
requires that every PR be identifiable in a `CHANGELOG` section. We have
no tool to enforce this.

## Decision

### 1. Extend `scripts/test_ci_workflow.py`

Add three new test cases that lock in the Phase 14/15 CI step additions:

- `test_production_profile_ci_step_exists` — verifies the
  `Lint and render production profile` step still asserts HPA/PDB/NetworkPolicy/
  mtls/`apiServer.replicas=3` invariants in the rendered output.
- `test_staging_profile_ci_step_exists` — verifies the
  `Lint and render staging profile` step still asserts 3 PDBs, 0 HPAs,
  0 NetworkPolicies, environment=staging, DEBUG log level.
- `test_argocd_schema_ci_step_exists` — verifies the
  `Validate ArgoCD CR schema` step still parses the three ArgoCD manifests
  and asserts no-secrets Role rules.
- `test_catalog_check_falls_back_to_diff` — verifies the
  `Check deployment connector inventory` step still invokes
  `scripts/diff-catalog.py` on failure and prints diagnostics.

The tests parse `ci.yml` as text and assert that the named step bodies
contain the required assertions. This mirrors the existing approach in
`test_multi_region_job_runs_and_cleans_up_on_failure` and stays robust
to comment additions.

### 2. Extend `scripts/check-runbook-templates.py`

Add two new default roots:

- `deployment/argocd/README.md` — Phase 14 operator onboarding template.
- `docs/catalog-authoring.md` — Phase 15 connector authoring guide.

Both contain `<placeholder>` patterns and must not contain production
hostname patterns. Add a `--root` invocation for each in
`Makefile check-runbooks`. Update the existing test suite to cover the
new roots.

### 3. Add `scripts/check-changelog.py`

A new guard that enforces:

- The `## [Unreleased]` section exists.
- The `## [Unreleased]` section references at least one phase whose README
  is marked `**Complete**`. The guard parses each `docs/phase<N>/README.md`,
  finds the status line, and verifies the corresponding phase appears in
  `## [Unreleased]` either by name (e.g. `Phase 13`) or by ADR range
  (e.g. `ADR-053..ADR-055`).

Exit code is 0 on compliance, 1 on missing phase references, 2 on missing
`## [Unreleased]` section.

### 4. Add `scripts/release-dry-run.py`

A new tool that, without mutating any file, prints what a release would
do:

1. The current `git rev-parse --short HEAD` as the build version.
2. The Maven project version from `pom.xml` (read-only).
3. The catalog SHA that would be embedded if `make catalog-export` ran
   now (computed by re-running the same deterministic export in-process
   via the CLI jar).
4. A list of phase READMEs marked `**Complete**` that have not yet been
   added to `CHANGELOG.md`.
5. A list of all `.proto` files (the wire contract) and whether
   `make proto-generate` would re-emit code (a deterministic
   `git status`-style report).

Exit code is 0 if everything is consistent, 1 if there are unsynced
phases or version mismatches.

### 5. Makefile additions

Two new targets:

- `check-docs` — runs `scripts/check-runbook-templates.py` for all roots
  (including the two new ones) and `scripts/check-changelog.py`.
- `release-dry-run` — runs `scripts/release-dry-run.py`.

`make check` is extended to depend on `check-docs` so that the docs gate
fires on every commit.

### 6. CI addition

A new `check-docs` job in `.github/workflows/ci.yml`, gated on the `docs`
change scope. The job runs `make check-docs`. Failure prints the specific
template or phase reference that is missing.

### 7. Phase 15 completion record

Phase 13 and Phase 14 each have a `completion.md` in their slice
directory. Phase 15 ships without one. This slice also produces a
`docs/phase15/41-catalog-lifecycle/completion.md` recording the verified
CI behaviour, the new Python scripts, and the author guide.

## Consequences

### Positive

- Phase 13/14/15 CI step additions become regression-tested.
- The runbook template guard now covers all operator-facing documentation.
- The release dry-run script catches the four most common release-day
  failures (version drift, catalog drift, unsynced phase CHANGELOG
  entries, proto drift) before any tag is pushed.
- The CHANGELOG guard enforces the governance rule that every phase
  completion must reach `CHANGELOG.md`.

### Negative

- `scripts/check-changelog.py` is a structural guard that knows about the
  Keep-a-Changelog section names. If the team adopts a different format,
  the guard must be updated. This is acceptable because the governance
  rule explicitly cites Keep-a-Changelog in `AGENTS.md` §5.
- `scripts/release-dry-run.py` runs the CLI jar against the current
  checkout, which means it must find `cli/target/astrasync-cli-*-all.jar`.
  The script surfaces a clear error if the jar is missing.

## Alternatives Considered

### Hard-code the CI step expectations into `Makefile` instead of a Python
test

Hard-coding into `Makefile` would couple the test to shell semantics and
make future edits fragile. The Python test is more readable and benefits
from `unittest`'s reporting.

### Use a tool like `act` to run the CI workflow locally

`act` would let us run the actual GitHub Actions workflow, but it
requires Docker and the workflow references GitHub-only services. A
text-level test is cheaper and exercises the same invariants.

### Generate the CHANGELOG automatically from git commits

`git-cliff` and similar tools can generate CHANGELOGs from conventional
commits. We have not adopted `git-cliff` because the existing
`CHANGELOG.md` is hand-curated by the maintainer at release time and
includes cross-cutting entries that don't fit a commit-derived format.
The `check-changelog.py` guard is sufficient for the governance rule
without changing the existing release workflow.
