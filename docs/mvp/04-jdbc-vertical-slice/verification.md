# Slice 04 Verification

## Status

Verified and complete.

## Environment

- OS: Windows 11
- Java: Eclipse Temurin 21.0.11
- Maven: 3.9.16
- Branch: `mvp/04-jdbc-vertical-slice`
- Parent slice: `mvp/03-file-vertical-slice@f7cfbc1`
- Design baseline: `b80d217`, scalar encoding amendment: `595e7e9`
- Implementation: `9fde26f`

## Acceptance Evidence

### Focused JDBC, File, Engine, and CLI Gate

```powershell
mvn -pl cli -am clean verify -DskipITs
mvn -pl cli -am verify -DskipITs
```

Result: clean and non-clean focused builds passed. Connector API ran 28 tests, Engine ran 32,
JDBC Connector ran 11, File Connector ran 21, and CLI ran 9. There were zero failures, errors,
or skips and all JaCoCo verification gates passed.

Coverage from the final JaCoCo CSV reports:

| Module | Lines | Line coverage | Branches | Branch coverage |
|---|---:|---:|---:|---:|
| `connector-api` | 145 / 201 | 72.1% | 45 / 55 | 81.8% |
| `engine` | 475 / 570 | 83.3% | 128 / 178 | 71.9% |
| `connector-jdbc` | 307 / 404 | 76.0% | 121 / 208 | 58.2% |
| `connector-file` | 222 / 237 | 93.7% | 94 / 106 | 88.7% |
| `cli` | 43 / 52 | 82.7% | 2 / 4 | 50.0% |

The focused tests prove:

- JDBC factory and options reject unknown, malformed, non-positive, and unsafe identifiers
  before `DriverManager` access, while `toString()` omits all option values.
- JDBC Source preserves metadata order and the documented null, Unicode, decimal, date/time,
  timestamp, binary, and large-text values through bounded pulls.
- Blank/duplicate labels and unsupported JDBC types fail with column/type evidence; Source close
  rolls back its read-only transaction and releases ResultSet, Statement, and Connection.
- JDBC Sink quotes identifiers, derives one parameterized insert, commits each successful batch,
  rolls back a rejected batch, rejects schema drift/unsupported values, and never commits in close.
- CSV Sink encodes JDBC scalar values using ADR-018, including deterministic Base64 for binary
  values; existing CSV strictness and null-token behavior remain covered.
- CLI executes JDBC-to-file and file-to-JDBC through the same `LocalJobRunner` with stable result
  output and no credential values in diagnostics.

### Full Reactor Gate

```powershell
mvn test -DskipITs
```

Result: all 23 Reactor modules passed. Modules with tests ran 98 tests in total across the
Connector API, Engine, JDBC, File, and CLI suites with zero failures, errors, or skips; empty
scaffold modules compiled successfully.

### Packaged Artifact and Driver Services

```powershell
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run examples/phase0/csv/job.yaml
jar tf cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar
```

Result: the packaged CSV job exited 0 with `recordsRead=2`, `recordsWritten=2`, `batches=2`,
and a 100-byte output that was removed after verification. The shaded artifact contains both
`com/mysql/cj/jdbc/Driver.class` and `org/postgresql/Driver.class`; its merged
`META-INF/services/java.sql.Driver` contains `com.mysql.cj.jdbc.Driver` and `org.postgresql.Driver`.
Repeated non-clean packaging produced no nested shaded artifact.

### Formatting, Diff, Dependency, and Boundary Gates

```powershell
mvn.cmd spotless:check
git diff --check
jdeps --multi-release base --ignore-missing-deps -summary engine/target/astrasync-engine-0.1.0-SNAPSHOT.jar
rg -n "connector-file|connector-jdbc|io\\.astrasync\\.connector\\.(file|jdbc)" engine --glob '!**/target/**'
mvn.cmd -pl cli -am dependency:tree "-Dscope=runtime" "-Dincludes=com.h2database:h2" -DskipTests
```

Result:

- the full 23-module Spotless check passed and the implementation diff passed whitespace checks;
- Engine's compiled dependency summary contains `java.base` and no concrete connector, and no
  concrete file/JDBC references exist in Engine sources;
- H2 is absent from runtime dependency output and remains test-scoped only.

## Acceptance Matrix

| Slice acceptance item | Evidence |
|---|---|
| Strict JDBC factory/options and bounded capabilities | `JdbcConnectorFactoryTest` |
| Bounded typed Source reads | `JdbcBatchSourceTest` |
| Null/Unicode/decimal/temporal/binary/LOB mapping | H2 Source/Sink tests |
| Metadata and unsupported-type failures | Source metadata/type tests |
| Parameterized Sink and identifier quoting | Sink insert and schema tests |
| Per-batch commit and rollback | Sink duplicate-key transaction test |
| JDBC-to-file and file-to-JDBC | `JdbcCliIntegrationTest` |
| CSV scalar compatibility | `CsvBatchSinkTest.encodesJdbcScalarsDeterministically` |
| Engine independence and driver packaging | `jdeps`, source search, shaded service entry |
| Focused, full, formatting, and diff gates | Commands and results above |

## Known Limitations

- Table identifiers are one or two unquoted segments and destination schemas must already exist;
  DDL, schema evolution, upsert, delete, and arbitrary SQL fragments are excluded.
- Source queries have no prepared parameters and use the database's configured isolation level;
  the single read-only transaction is not a repeatable-read or replay guarantee.
- Only the ADR-016 scalar matrix is supported. Vendor JSON, arrays, spatial, interval, and custom
  objects fail fast; CSV represents binary values as Base64 text and does not restore types.
- Sink commits per runtime batch. A commit outcome can be unknown after a client-side failure,
  and already committed rows are not removed or replayed; exactly-once remains rejected.
- MySQL/PostgreSQL drivers are packaged in the CLI, while H2 is test-only and no external database
  integration is exercised in this slice.
- Maven still emits non-failing forked-`javac` and shade module-metadata overlap warnings.

## Resulting Commit

Implementation: `9fde26f` (`feat(jdbc): add typed jdbc vertical slice`).
