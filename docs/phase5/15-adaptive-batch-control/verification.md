# Phase 5 Slice 15 Verification

## Required Local Checks

| Check | Result |
|---|---|
| `mvn -B -ntp -pl engine -am test` | PASS: 50 tests in Engine and 33 tests in Connector API |
| `mvn -B -ntp clean verify -DskipITs` | PASS: 31 Maven modules |
| `mvn -B -ntp spotless:check` | PASS |
| Adaptive controller JMH smoke invocation | PASS: batchDecision and parallelismDecision |
| `git diff --check` | PASS |

## Acceptance Evidence

- Adaptive batch tests must prove fixed compatibility, minimum/maximum clamping, EWMA-driven
  increase/decrease, queue-pressure reduction, cooldown hysteresis, and invalid configuration rejection.
- Adaptive parallelism tests must prove bounded targets, backlog-gated scale-up, slow-task scale-down,
  in-flight task completion, and deterministic failure propagation.
- JobSpec and protocol tests must prove absent settings disable tuning and mismatched Worker policies
  are rejected before execution.
- Checkpoint tests must prove adaptive batch limits do not change checkpoint sequence or ordering.
- JMH smoke must execute every Slice 15 controller benchmark method. It is a runnable-build gate,
  not a throughput SLA.

The clean verification regenerated protobuf sources before compilation. This is required when a
local target directory contains stale generated output from an earlier protocol layout.

## Pull-request Gate

Pull request [#26](https://github.com/baicie/astra-sync/pull/26) passed every required GitHub
Actions job in [CI run 31111025522](https://github.com/baicie/astra-sync/actions/runs/31111025522)
before squash merge. Go and PostgreSQL jobs were scope-skipped because the change did not touch the
control plane.

The pull request was squash merged into `main` as
[`49ee76a08cf6f55c386e754eb77cce76906d03ed`](https://github.com/baicie/astra-sync/commit/49ee76a08cf6f55c386e754eb77cce76906d03ed).
Post-merge [CI run 31111524781](https://github.com/baicie/astra-sync/actions/runs/31111524781)
also passed, including Maven verification, Spotless, Arrow and adaptive-controller benchmark
smokes, protocol validation, repository policy checks, and all five container image builds.
