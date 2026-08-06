# Phase 5 Slice 14 Verification

## Required Local Checks

| Check | Result |
|---|---|
| `mvn -B -ntp -pl formats/arrow-format -am test` | PASS: 13 Arrow tests, 0 failures |
| `mvn -B -ntp verify -DskipITs` | PASS: 31 modules succeeded |
| `mvn -B -ntp spotless:check` | PASS |
| Arrow JMH smoke invocation | PASS: all 4 `ArrowBatchBenchmark` methods ran with `rowCount=128` |
| `git diff --check` | PASS |

## Acceptance Evidence

- Scalar, null, decimal, binary, date, time, local timestamp, and zoned timestamp tests must prove
  deterministic Row-to-Arrow-to-Row values.
- Explicit-schema tests must cover all-null columns and terminal empty batches, while inference
  rejects data that cannot produce one stable schema.
- Ownership tests must prove idempotent close, closed access rejection, parent allocator survival,
  zero retained child allocation, and enforcement of a per-batch memory limit.
- IPC tests must prove end-of-input preservation and deterministic rejection of malformed,
  unsupported, truncated, and oversized frames.
- JMH smoke must run all Slice 14 benchmark methods. Throughput numbers from a shared runner are not
  an acceptance threshold.

## Commands and Evidence

The benchmark smoke was run with:

```text
java --add-opens=java.base/java.nio=ALL-UNNAMED -jar tests/benchmark/target/astrasync-benchmarks.jar ArrowBatchBenchmark -wi 0 -i 1 -f 1 -r 100ms -p rowCount=128
```

The PowerShell runner was also exercised with the same arguments. The Linux shell runner is wired
into the repository and is validated by the Linux CI job.

## Pull-request Gate

Every required GitHub Actions job must pass before squash merge. The final CI run and merge commit
are recorded after the implementation is pushed.
