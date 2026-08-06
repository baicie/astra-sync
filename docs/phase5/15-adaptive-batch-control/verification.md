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

Every required GitHub Actions job must pass before squash merge. The final CI run and merge commit
will be recorded after the implementation is pushed.
