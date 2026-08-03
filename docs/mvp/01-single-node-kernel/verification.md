# Slice 01 Verification

## Status

Verified and complete.

## Environment

- OS: Windows 11
- Java: Eclipse Temurin 21.0.11
- Maven: 3.9.16
- Branch: `mvp/01-single-node-kernel`
- Base: `8b237b7`

## Baseline Evidence

Command:

```powershell
mvn -pl engine -am test -DskipITs
```

Result before implementation: failed in `connector-api` compilation. The first reported defect
is the pseudo-parameterized enum syntax in `metadata/Schema.java`; engine tests were skipped.

The first formatting run also showed that the parent POM used the Gradle Spotless version
`6.25.0` as a Maven plugin version. That artifact does not exist in Maven Central; slice 01
corrects the build coordinate before the formatting gate is rerun.

The initial Engine POM also declared future Arrow, network, state, compression, connector, and
protocol dependencies although slice 01 uses none of them. Some coordinates were unresolved and
prevented even focused tests. Slice 01 removes those speculative Engine dependencies; later
slices add dependencies only when executable code owns them.

The first successful focused test run reported that JaCoCo had no execution data. Surefire's
hard-coded JVM arguments replaced JaCoCo's injected agent argument. Slice 01 preserves the
late-evaluated JaCoCo `argLine` so coverage evidence is produced on the verification rerun.

The first full-reactor run stopped in the data protocol because the Protobuf plugin referenced
`${os.detected.classifier}` without loading the OS detection build extension. Slice 01 adds the
portable detector instead of hard-coding a Windows classifier.

Once protoc ran, the connector protocol exposed package-scoped enum-value collisions between
`ConfigType` and `FieldType`. Because the protocol had no successfully generated baseline,
slice 01 prefixes the enum symbols while preserving their numeric wire values. It also removes
an unused import reported by the data protocol compiler.

The Reactor then reached `parquet-format` and found that `zstd-jni` used a non-existent bare
upstream version. Slice 01 uses the published Maven coordinate `1.5.6-3` and removes the same
unused Protobuf import from the connector protocol.

The build next reached the CDC connector scaffolds and found Debezium's release suffix omitted.
Slice 01 corrects `2.5.4` to the published `2.5.4.Final` coordinate; CDC behavior remains outside
this slice.

## Acceptance Evidence

### Focused Kernel Gate

```powershell
mvn -pl engine clean verify -DskipITs
```

Result: passed. Surefire ran 16 tests with zero failures, errors, or skips. The JaCoCo
verification gate passed.

Coverage from `engine/target/site/jacoco/jacoco.csv`:

- Lines: 144 covered, 61 missed (70.2%).
- Branches: 32 covered, 16 missed (66.7%).

The tests prove:

- multiple requested batches stay within the configured limit and preserve transform order;
- the Source is not polled until the current batch has reached the Sink;
- an oversized Source batch fails before any record in that batch is written;
- Source-open/read, Sink-open/write, transform, and close failures retain their stage and partial
  counters;
- opened resources close in reverse order, and secondary close failures are suppressed;
- records and batches defensively copy their containers, reject null keys/records, and preserve
  null values.

### Reactor Gate

```powershell
mvn test -DskipITs
```

Result: passed for all 22 Reactor modules. The Engine's 16 tests passed in the full build; modules
without tests compiled successfully.

### Formatting and Diff Gates

```powershell
mvn spotless:check
git diff --check
```

Result: both passed. The first Spotless run after adding the last failure-boundary tests detected
one standard line-wrap difference. `mvn -pl engine spotless:apply` corrected it, after which the
full 22-module Spotless check passed.

## Known Limitations

- This slice exposes only the internal `engine.kernel` contracts. JobSpec parsing, connector
  discovery, and the durable public row/batch SPI remain owned by slice 02.
- The minimal baseline repairs make the initial sketches compile, but some Engine sketch
  interfaces still reference package-private auxiliary types. Slice 02 replaces the relevant
  JobSpec and connector surface instead of treating these sketches as a stable API.
- Sink calls are per record; the public SPI adds bounded batch writes in slice 02.
- There is no checkpoint, retry, cancellation API, prefetch, asynchronous execution, or
  exactly-once claim.
- Maven emits a harmless warning that the forked compiler cannot autodetect the `javac` path and
  uses the executable from the environment.

## Resulting Commit

Implementation: `e4ed490` (`feat(engine): implement bounded single-node kernel`).
