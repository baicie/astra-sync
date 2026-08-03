# Slice 02 Verification

## Status

Verified and complete.

## Environment

- OS: Windows 11
- Java: Eclipse Temurin 21.0.11
- Maven: 3.9.16
- Branch: `mvp/02-job-spec-connector-spi`
- Parent slice: `mvp/01-single-node-kernel@ec1e7d2`
- Implementation: `769a455`

## Acceptance Evidence

### Focused JobSpec, SPI, Compiler, and Kernel Gate

```powershell
mvn -pl connector-api,engine -am clean verify -DskipITs
```

Result: passed. Connector API ran 28 tests and Engine ran 27 tests, with zero failures,
errors, or skips. Both JaCoCo verification gates passed.

Coverage from the final `jacoco.csv` reports:

| Module | Lines | Line coverage | Branches | Branch coverage |
|---|---:|---:|---:|---:|
| `connector-api` | 146 / 202 | 72.3% | 45 / 55 | 81.8% |
| `engine` | 431 / 532 | 81.0% | 127 / 176 | 72.2% |

The focused tests prove:

- YAML and JSON v1 inputs produce equal immutable JobSpec values with deterministic defaults and
  option ordering;
- duplicate, unknown, missing, wrong-type, unsupported-version, malformed-name, and unsupported
  delivery inputs fail at documented paths;
- connector names and DNS-label job names use explicit bounded syntax;
- rows retain nulls and structurally copy their maps, while batches copy their row lists and reject
  invalid empty or null-row values;
- descriptor construction enforces capability-to-role consistency and the registry rejects
  duplicate canonical names;
- compilation resolves both descriptors before role and batch capability checks, rejects
  transforms and stronger delivery guarantees, and returns equal value-only plans for equivalent
  input;
- rejected plans never invoke Source/Sink factory creation or lifecycle methods, including when
  future replay and transaction capabilities are advertised;
- the migrated kernel preserves bounded pulls, no prefetch, lifecycle ownership, reverse close,
  failure stages, partial counters, and suppressed close failures on the public row/batch SPI.

### Reactor Gate

```powershell
mvn test -DskipITs
```

Result: passed for all 22 Reactor modules. Nine Surefire suites ran 55 tests with zero failures,
errors, or skips; modules without tests compiled successfully.

### Formatting, Diff, Reference, and Dependency Gates

```powershell
mvn spotless:check
git diff --check
rg -n "\b(RecordBatch|SyncBatch|SyncRecord|RecordSource|RecordSink|ConnectorCapabilities|ConnectorConfig|SourceConfig|SinkConfig|Configurable|engine\.api\.JobSpec)\b" connector-api engine --glob '!**/target/**'
jdeps --multi-release base --ignore-missing-deps -summary connector-api/target/connector-api-0.1.0-SNAPSHOT.jar
```

Result:

- the full 22-module Spotless check passed;
- the implementation diff passed whitespace validation;
- no superseded API reference remained in `connector-api` or `engine`;
- the compiled Connector API JAR depends only on `java.base`.

The first formatting check after the final name/order regression tests found five files with
formatter or mixed-line-ending differences. `mvn -pl engine spotless:apply` normalized only those
files, and the full check then passed.

## Acceptance Matrix

| Slice acceptance item | Evidence |
|---|---|
| YAML/JSON parity | `JobSpecParserTest` equality and ordering assertions |
| Strict path-aware parsing | Parser negative cases for fields, types, versions, names, and documents |
| Immutable row and batch values | `RowTest` and `RowBatchTest` |
| Descriptor and registry consistency | `ConnectorDescriptorTest` and `ConnectorRegistryTest` |
| Compile-time role/capability/feature rejection | `JobCompilerTest` error-code assertions |
| Honest delivery semantics | At-least-once and exactly-once rejection with future capabilities present |
| Zero rejected-plan materialization | Probe factory create/open counters remain zero |
| Deterministic plans | Equal plan assertion across registry order and sorted options |
| Kernel behavior retained | Migrated `SingleNodeSyncJobTest` suite |
| Focused and Reactor builds | Both commands above passed |

## Known Limitations

- Concrete file and JDBC connectors, CLI materialization, service discovery, and live connectivity
  checks remain owned by later slices.
- Phase 0 compiles only `at-most-once`; transforms, at-least-once, and exactly-once are rejected.
- Connector options are string values with no secret resolution. Value-bearing `toString` methods
  redact option values, but production credential catalogs remain outside this slice.
- `Row` performs structural container copying, not deep cloning of arbitrary mutable values.
  Connector implementations must own mutable values such as byte arrays.
- Descriptor claims are static and trusted until connector compatibility tests are added.
- Maven emits non-failing warnings for forked `javac` path detection and shaded module metadata.

## Resulting Commit

Implementation: `769a455` (`feat(engine): add job spec and connector spi`).
