# Slice 05 Implementation Plan

## Preconditions

- [x] Start from verified Slice 04 documentation commit `bc4e180` (implementation `9fde26f`).
- [x] Re-read ADR-011/014/017/019/020 and the Phase 0 acceptance matrix.
- [x] Inventory the current kernel stages, LocalJobRunner overload boundary, CLI text report, and
  existing resource/partial-result tests.
- [x] Commit ADR-019 and ADR-020 before changing the Engine cancellation or CLI report contract.

## Steps

### 1. Add Cooperative Cancellation

- Add `CancellationToken`, a `CANCELLED` stage, builder support, and a LocalJobRunner overload.
- Check cancellation at open/pull/write boundaries and preserve reverse close/suppressed failures.
- Gate: Engine tests prove no materialization before cancellation, partial counters, and closure.

### 2. Add Machine-readable CLI Metrics

- Add `--metrics text|json` with text as the compatibility default.
- Serialize success, input/validation, runtime, and cancellation reports through Jackson without
  connector option values or causes.
- Add exit code 5 for cancellation and retain 0/2/3/4 behavior.
- Gate: CLI tests parse exactly one JSON object and assert forbidden-value redaction.

### 3. Harden Resources and Examples

- Add cancellation/close-failure tests across the Engine and connector lifecycle boundaries.
- Add `examples/phase0/README.md` and JDBC prerequisites/JobSpec documentation without adding a
  production database dependency.
- Gate: bounded slow-Sink and partial-write tests remain deterministic.

### 4. Verify and Archive

- Run focused clean/non-clean verify, full Reactor tests, Spotless, diff, dependency, `jdeps`,
  shaded artifact, JSON parser, and clean-worktree checks.
- Record exact test counts, coverage, report samples, limitations, and resulting implementation
  commit in `verification.md`.
- Mark Phase 0 and Slice 05 complete only after every acceptance item passes.

### 5. Close Review Findings

- Amend ADR-016/019/020 before implementation to define unsupported zoned-time input,
  cancellation callback failures, and parse-time metrics output.
- Route Picocli parameter failures through sanitized text/JSON reports, complete the generic
  runtime schema, and make root/subcommand version output consistent.
- Preserve token callback failures, partial metrics, suppressed close failures, and serialized
  `SyncJobException` state.
- Reject `TIME_WITH_TIMEZONE` through the standard unsupported-type path and add H2 evidence.
- Gate: focused regression suites, full Reactor tests, formatting, repository policy, packaged CLI,
  and clean-worktree checks pass before the review-fix commit is pushed.
- Upgrade Java CI from model validation to full verification, scope planned Go/Helm checks to their
  owned paths, repair Helm helper syntax, and give secret scanning complete Git history.

## Change Control

Do not add asynchronous execution, OS signal semantics, persistent metrics, retries, checkpoints,
or stronger delivery claims in this slice without revising the design and ADRs first.
