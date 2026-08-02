# JobSpec and Connector SPI Design

## Context

Slice 01 proves bounded execution but exposes internal `SyncRecord`, `SyncBatch`, Source, and Sink
types. The repository also contains initial JobSpec and connector sketches that either expose
package-private auxiliary types or live below uncompiled source roots. Treating those sketches as
a stable public API would freeze invalid boundaries and leave capability checks disconnected from
execution.

Slice 02 establishes the smallest coherent Java API needed by both the file and JDBC vertical
slices. Long-term split readers, committers, CDC events, and Arrow batches remain governed by the
architecture ADRs but are not simulated in Phase 0.

## Processing Boundary

```text
YAML / JSON text
       |
       v
strict JobSpec v1 parser -> immutable JobSpec
                                  |
                                  v
connector descriptors -> JobCompiler -> immutable CompiledJobPlan
                                                 |
                                     later materialization only
                                                 |
                                                 v
                                ConnectorFactory -> Source / Sink -> open
```

Parsing and compilation are side-effect free. Factory creation and connector `open` calls occur
only after a plan has passed every structural, role, capability, transform, and delivery check.

## Public Data Contract

### Row

`Row` owns an insertion-ordered `Map<String, Object>`.

- Column names are non-blank and unique by map construction.
- Values may be null.
- Construction and `with` defensively copy the map structure.
- Exposed values are unmodifiable.
- Equality is value based and preserves deterministic iteration order for encoding.

The copy is structural, not a deep clone of arbitrary user objects. File and JDBC connectors must
create owned values for mutable payloads such as byte arrays.

### RowBatch

`RowBatch` owns an immutable list of non-null rows and carries an end-of-input flag.

- `data(rows)` is non-terminal and must be non-empty.
- `last(rows)` may be non-empty or returns the shared terminal empty batch.
- `end()` is the terminal empty batch.
- The runtime validates every returned batch against the requested row limit.

The Sink receives one complete bounded batch. Transform output may allocate one additional batch,
so the local runtime bound is the Source batch plus one transformed batch, never the full data set.

## Connector SPI

### Descriptor

`ConnectorDescriptor` is an immutable value containing:

- canonical connector name;
- implementation version;
- supported roles (`SOURCE`, `SINK`);
- capabilities such as `BATCH_READ`, `BATCH_WRITE`, `REPLAYABLE_OFFSET`, and
  `TRANSACTIONAL_COMMIT`.

Names use lowercase letters, digits, dots, underscores, and hyphens. `BATCH_READ` requires the
Source role and `BATCH_WRITE` requires the Sink role. A role may expose a different mode (for
example, stream-only); the Phase 0 compiler separately requires the matching batch capability.
Descriptor lookup never creates a connector.

### Factory and Runtime Interfaces

```java
interface ConnectorFactory {
    ConnectorDescriptor descriptor();
    BatchSource createSource(ConnectorConfiguration configuration);
    BatchSink createSink(ConnectorConfiguration configuration);
}

interface BatchSource extends AutoCloseable {
    void open();
    RowBatch readBatch(int maxRows);
    void close();
}

interface BatchSink extends AutoCloseable {
    void open();
    void writeBatch(RowBatch batch);
    void close();
}
```

Unsupported factory roles throw only if incorrectly materialized; the compiler prevents that path.
Factory creation must not acquire external resources. Connection, file, transaction, and buffer
ownership starts in `open` and ends in `close`.

`ConnectorConfiguration` is an immutable string map. Typed accessors perform strict boolean and
integer parsing and include the option key in errors. Connector-specific required/unknown option
validation belongs to the concrete connector before resources are opened.

## Registry

The in-memory registry is constructed explicitly for Phase 0.

- Registration keys are descriptor names.
- Duplicate names are errors even if versions match.
- Entries are stored in canonical sorted order.
- Registry snapshots expose descriptors, not mutable factory maps.
- Lookup distinguishes missing connector and unsupported role.

ServiceLoader, remote registries, dependency isolation, and hot replacement remain later work.

## JobSpec v1

The external shape is:

```yaml
apiVersion: sync.astrasync.io/v1
kind: SyncJob
metadata:
  name: local-copy
spec:
  source:
    connector: csv
    options:
      path: input.csv
  transforms: []
  sink:
    connector: csv
    options:
      path: output.csv
  delivery:
    guarantee: at-most-once
  runtime:
    maxBatchRecords: 1024
```

Rules:

- `apiVersion`, `kind`, metadata, Source, Sink, and delivery are required.
- `apiVersion` is exactly `sync.astrasync.io/v1`; `kind` is exactly `SyncJob`.
- Unknown fields and duplicate object keys fail.
- Connector and transform options are string-to-string maps.
- `transforms` defaults to empty; a non-empty list parses but compilation rejects it in Phase 0.
- `runtime.maxBatchRecords` defaults to 1024 and must be positive.
- Delivery syntax is one of `at-most-once`, `at-least-once`, or `exactly-once`.
- Job names use lowercase DNS-label syntax and are at most 63 characters.

Parsing uses Jackson's YAML tree parser with duplicate detection, then explicit allowed-field and
type validation. This avoids lenient bean coercion and produces stable paths such as
`$.spec.source.options.path`.

## Compilation

`JobCompiler` performs these steps in fixed order:

1. Validate the immutable JobSpec invariants.
2. Resolve Source and Sink descriptors by exact connector name.
3. Validate required roles and bounded batch capabilities.
4. Reject executable transforms not supplied by Phase 0.
5. Negotiate delivery against runtime and connector capabilities.
6. Normalize option maps into sorted immutable maps.
7. Return a value-only `CompiledJobPlan` containing descriptor versions and actual guarantee.

The compiler never invokes `createSource`, `createSink`, or `open`. Equal specifications and equal
descriptor values therefore produce equal plans regardless of registration order.

## Delivery Semantics

The Phase 0 runtime has no replay loop, checkpoint, pending commit, or transaction coordinator. Its
only honest guarantee is `AT_MOST_ONCE`: each row/batch is attempted once, and a Sink may have
partially persisted a batch before throwing.

- `at-most-once` compiles when Source and Sink provide bounded batch roles.
- `at-least-once` is rejected because the runtime has no replay/retry boundary.
- `exactly-once` is rejected even when a Source advertises replayable offsets and a Sink advertises
  transactional commit. Connector capability cannot replace missing runtime coordination.

This is an application of ADR-009, not a downgrade policy. The compiler reports requested and
available guarantees in the error.

## Kernel Integration

The slice-01 kernel migrates to public `Row`, `RowBatch`, `BatchSource`, and `BatchSink` types. It
retains:

- one Source pull at a time;
- no prefetch queue;
- ordered transforms;
- runtime enforcement of the requested maximum;
- Source-before-Sink open and reverse close;
- stage-classified errors, partial counters, and suppressed close failures.

Sink metrics count a batch only after `writeBatch` returns. A failing Sink batch is reported as
potentially partially written because the runtime cannot inspect connector-internal effects.

## Compatibility and Cleanup

The initial `engine.api.JobSpec` sketch and the uncompiled connector `source-api`, `sink-api`, and
`catalog-api` trees are superseded. Slice 02 removes them after their supported Phase 0 concepts
are represented in compiled public sources. Long-term enumerator and committer APIs return in
their owning phases under ADR-003 and ADR-004.

Arrow is not part of the Phase 0 public batch. ADR-008 remains accepted; Arrow is introduced when
an executable columnar path and buffer ownership tests exist.

## Test Strategy

- YAML/JSON parity, defaults, immutability, and deterministic normalization.
- Unknown, duplicate, missing, wrong-type, and unsupported-version parsing failures with paths.
- Row and RowBatch defensive-copy/null/terminal tests.
- Descriptor, configuration, and duplicate registry tests.
- Missing connector, wrong role, missing capability, transform, and delivery rejection tests.
- Probe factories proving rejected compilation performs no create/open operation.
- Equal-plan tests across input syntax and registry registration order.
- Existing bounded execution and lifecycle/failure tests migrated to the public SPI.
