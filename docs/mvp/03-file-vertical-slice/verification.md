# Slice 03 Verification

## Status

Verified and complete.

## Environment

- OS: Windows 11
- Java: Eclipse Temurin 21.0.11
- Maven: 3.9.16
- Branch: `mvp/03-file-vertical-slice`
- Parent slice: `mvp/02-job-spec-connector-spi@0776547`
- Implementation: `f7c6b3c`

## Acceptance Evidence

### Focused File, Engine, and CLI Gate

```powershell
mvn -pl cli -am clean verify -DskipITs
mvn -pl cli -am verify -DskipITs
```

Result: both clean and non-clean focused builds passed. Connector API ran 28 tests, Engine ran
32 tests, File Connector ran 20 tests, and CLI ran 7 tests. There were zero failures, errors, or
skips, and every JaCoCo verification gate passed. The non-clean rerun produced the attached
`astrasync-cli-0.1.0-SNAPSHOT-all.jar` without nesting the previous shaded artifact.

Coverage from the final JaCoCo CSV reports:

| Module | Lines | Line coverage | Branches | Branch coverage |
|---|---:|---:|---:|---:|
| `connector-api` | 145 / 201 | 72.1% | 45 / 55 | 81.8% |
| `engine` | 475 / 570 | 83.3% | 128 / 178 | 71.9% |
| `connector-file` | 218 / 233 | 93.6% | 73 / 80 | 91.2% |
| `cli` | 43 / 52 | 82.7% | 2 / 4 | 50.0% |

The focused tests prove:

- JobSpec compilation completes before either factory is created; connector creation completes
  before the kernel opens either resource.
- CSV source input accepts one leading UTF-8 BOM and RFC 4180 quotes, commas, embedded CRLF/LF,
  Unicode, empty strings, and an explicit null token while enforcing headers and exact row width.
- CSV lexical, header, and width failures are fail-fast and include path plus record/line context.
- CSV sink validates the existing parent, uses `CREATE_NEW`, derives and preserves first-row header
  order, rejects schema drift and unsupported values, flushes each batch, and leaves explicit
  partial output after a later failure.
- CLI help, version, input/validation/runtime exit categories, redacted failures, and bounded
  success counters are stable.

### Full Reactor Gate

```powershell
mvn test -DskipITs
```

Result: all 23 Reactor modules passed. The four executable suites above ran 87 tests with zero
failures, errors, or skips; modules without tests compiled successfully.

### Packaged-JAR Acceptance

```powershell
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run examples/phase0/csv/job.yaml
```

Result: exit code 0, `recordsRead=2`, `recordsWritten=2`, `batches=2`, and a 100-byte output file
with deterministic CRLF output. The generated ignored output was removed after verification.

### Formatting, Diff, and Dependency-Direction Gates

```powershell
mvn.cmd spotless:check
git diff --check
rg -n "connector-file|io\\.astrasync\\.connector\\.file" engine cli connectors/connector-file pom.xml --glob '!**/target/**'
jdeps --multi-release base --ignore-missing-deps -summary engine/target/astrasync-engine-0.1.0-SNAPSHOT.jar
```

Result:

- the full 23-module Spotless check passed;
- the implementation diff passed whitespace validation;
- concrete file references exist in the CLI and file connector only, not in Engine sources or
  Engine POM;
- Engine's compiled dependency summary contains `java.base` and no `connector-file` module.

## Acceptance Matrix

| Slice acceptance item | Evidence |
|---|---|
| File-to-file JobSpec path | CLI unit test and packaged-JAR run |
| Bounded batches | Engine probe and CLI success reports `maxBatchRecords` |
| Compile/create/open ordering | `LocalJobRunnerTest` event trace |
| Strict CSV grammar and null/empty behavior | Source/Sink tests and round-trip CLI test |
| Header and exact-width validation | Source malformed-input tests |
| Create-new and same-path protection | Sink and CLI existing-output tests |
| Partial output semantics | Sink failure and malformed-source CLI tests |
| Stable CLI exit categories | `AstraSyncCliTest` |
| Engine independence from file connector | source/POM search and `jdeps` |
| Focused, Reactor, formatting, and diff gates | Commands and results above |

## Known Limitations

- Only UTF-8 RFC 4180 CSV with a header is supported; dialect customization, headerless input,
  JSON, Parquet, directories, compression, object storage, stdin, and stdout remain future work.
- A configured null token intentionally makes the same literal field ambiguous with null.
- Header-only input creates an empty sink file because the current batch SPI has no separate schema
  propagation path.
- The sink writes directly and may leave a partial file; overwrite, cleanup-on-failure, atomic
  publication, retries, and stronger delivery guarantees remain out of scope.
- CSV values are limited to strings and explicitly encoded nulls; JDBC type mappings are Slice 04.
- Connector registration is explicit in the CLI; ServiceLoader and external plugin isolation are
  deferred.

## Resulting Commit

Implementation: `f7c6b3c` (`feat(file): add csv vertical slice and cli`).
