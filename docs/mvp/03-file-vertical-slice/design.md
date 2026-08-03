# File Vertical Slice Design

## Context

Slices 01 and 02 establish bounded execution, public row/batch contracts, strict JobSpec parsing,
descriptor-only compilation, and explicit connector factories. They intentionally stop before
materialization. The repository's existing Engine main class prints a banner but cannot run a job,
and the file connector module contains only a speculative Parquet dependency.

Slice 03 must connect those boundaries without making the core Engine depend on a concrete
connector or claiming transactional file publication that the batch Sink lifecycle cannot supply.

## End-to-end Flow

```text
CLI run <job.yaml>
        |
        v
read UTF-8 document -> JobSpecParser -> JobCompiler -> CompiledJobPlan
                                               |
                                  no factory create/open before this point
                                               |
                                               v
                             LocalJobRunner materializes factories
                                               |
                                               v
                              SingleNodeSyncJob (bounded pull)
                                               |
                                  CsvBatchSource -> CsvBatchSink
                                               |
                                               v
                                   terminal result and CLI exit
```

The CLI owns process concerns. The Engine owns generic local execution. The file connector owns
CSV configuration and I/O. Dependency direction is:

```text
cli -> engine -> connector-api
  \-> connector-file -> connector-api
```

The Engine does not import `connector-file`.

## Local Materialization Boundary

`LocalJobRunner` receives an explicit `ConnectorRegistry` and an immutable JobSpec. It:

1. Compiles the entire job through `JobCompiler`.
2. Resolves factories pinned to the descriptor name and version in the compiled plan.
3. Creates resource-free Source and Sink instances from normalized connector options.
4. Builds and runs `SingleNodeSyncJob` with the compiled batch limit.
5. Returns the compiled plan and terminal `SyncResult` as one immutable result.

Compilation failures invoke neither factory creation nor connector lifecycle methods. Connector
option validation occurs during resource-free factory creation; both connector `open` methods are
still deferred to the kernel. A factory returning null is a materialization error.

The current runtime has no executable transform registry, so the compiler continues to reject all
non-empty transform lists.

## CSV Connector Descriptor

One explicit factory is registered under the canonical name `csv`, version `1.0.0`:

- roles: `SOURCE`, `SINK`;
- capabilities: `BATCH_READ`, `BATCH_WRITE`;
- no replayable-offset or transactional-commit claim.

The factory has no ambient configuration and creates a new connector instance per role and job.
Apache Commons CSV 1.11 parses and prints the established CSV grammar instead of introducing a
custom parser.

## CSV Configuration

### Source

| Option | Required | Default | Rule |
|---|---|---|---|
| `path` | yes | none | Non-blank local path, resolved from the process working directory |
| `nullValue` | no | none | Exact field token decoded as Java null |
| `malformedRowPolicy` | no | `fail` | Only `fail` is supported in Phase 0 |

### Sink

| Option | Required | Default | Rule |
|---|---|---|---|
| `path` | yes | none | Non-blank local path whose parent already exists |
| `nullValue` | no | none | Exact token used to encode Java null |

Unknown options are rejected. CSV syntax is deliberately fixed to UTF-8 RFC 4180 with comma,
double quote, CRLF output, no trimming, and no comments. A single leading UTF-8 BOM is accepted on
input and never emitted on output. Fixed syntax keeps the first vertical slice reproducible; a
dialect option is a future versioned contract rather than an unchecked pass-through.

An absent `nullValue` means every parsed field is a non-null string, including an empty field. A
Sink receiving null without a configured token fails rather than silently converting it to an
empty string. Choosing a token introduces the documented ambiguity that the same literal field is
decoded as null.

Configuration and plan `toString` methods continue to expose option keys, never path or null-token
values.

## Header and Row Contract

The first CSV record is the schema header.

- At least one header is required, including for a file with no data rows.
- Header names are case-sensitive, non-blank, and unique.
- Spaces are data and are not trimmed.
- Every data record must have exactly the header width.
- Source rows preserve header order in their insertion-ordered map.
- Quoted commas, doubled quotes, and quoted embedded CRLF/LF values follow RFC 4180.

The Sink derives its output header from the first row it receives. Every later row must have the
same column-name set; values are written in first-row header order even if a later Row was built in
a different insertion order. Empty Rows are rejected.

Slice 03 writes only string and null values. JDBC scalar, temporal, decimal, binary, and LOB CSV
encodings are decided with the Slice 04 type matrix instead of relying on locale-sensitive
`toString` behavior.

A header-only Source produces no data batch, so a Sink with no row from which to derive schema
creates an empty output file. Preserving a header for an empty data set requires future schema
propagation or an explicit Sink schema option and is recorded as a limitation, not inferred.

## Bounded Source Behavior

`readBatch(maxRows)` rejects a non-positive request even though the Engine already provides a
positive compiled value. It accumulates at most `maxRows` valid rows and never loads the complete
file. If exactly `maxRows` rows are returned, the Source does not look ahead merely to mark the
batch terminal; the next pull observes EOF and returns the terminal empty batch. This preserves
the kernel's one-pull-at-a-time invariant.

Reader, parser, iterator, and header state are created in `open` and released once in `close`.
Calls before open, after close, or after terminal EOF fail deterministically.

## Malformed Input Policy

Phase 0 supports fail-fast only:

- a missing, empty, blank, or duplicate header fails during `SOURCE_OPEN`;
- an unterminated quote or other lexical CSV error fails during `SOURCE_READ`;
- an inconsistent row width fails during `SOURCE_READ` before that row reaches the Sink;
- errors name the input path and the best available record and physical line number;
- no record is skipped and no partial recovery is attempted.

CSV quoting permits embedded physical newlines, so a general line-by-line skip implementation
cannot safely resynchronize. `skip`, dead-letter, and retry policies require rejected-record
metrics plus a parser recovery contract and remain out of scope.

## Sink Publication and Failure Semantics

The Sink opens with create-new semantics. It rejects an existing target and does not create parent
directories. This prevents accidental truncation, including when input and output paths are equal.

After the first batch establishes the header, each `writeBatch` writes complete records and flushes
the CSV printer before returning. The runtime counts a batch as written only after that return.
Flush provides prompt I/O failure classification but is not a disk durability or transactional
commit guarantee.

The output is written directly because `BatchSink.close` has no success/abort distinction. A
temporary-file rename in `close` would publish partial data even when Source or transform execution
failed. If a later batch fails, the create-new output remains as an explicitly partial artifact;
the CLI reports failure and never claims an atomic result. Overwrite, append, cleanup-on-failure,
and atomic publication require a commit/abort lifecycle.

## CLI Contract

The runnable artifact is the shaded JAR produced by a new `cli` module. The core Engine returns to
a normal library JAR; its placeholder banner main and Engine-owned shade configuration are removed.

```text
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run <job-spec-path>
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar --help
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar --version
```

Picocli supplies deterministic argument parsing and help. The CLI reads the JobSpec itself as
UTF-8, constructs an explicit built-in registry containing `CsvConnectorFactory`, and delegates
all planning/materialization/execution to the Engine.

Exit categories are stable for Phase 0:

| Code | Category | Examples |
|---:|---|---|
| 0 | success | Job completed |
| 2 | invocation/input | Bad CLI syntax, missing or unreadable JobSpec file |
| 3 | validation | JobSpec parse, compile, or connector-option error before `open` |
| 4 | runtime | Source/Sink open, read, write, close, or other execution failure |

Success prints one concise result line with job name and bounded-runtime counters. Failure prints a
single categorized message to stderr without a stack trace by default. Secret-bearing connector
option values are not printed. Structured metrics and verbose diagnostics remain Slice 05 work.

## Test Strategy

- Connector option validation and resource-free factory construction.
- BOM, header, null/empty, Unicode, quoting, multiline, exact-width, and lexical-error fixtures.
- Multi-batch Source tests that inspect requested limits and terminal behavior.
- Create-new, parent, schema consistency, unsupported value, flush, and close tests for the Sink.
- Generic local-runner probes for compile-before-create and create-before-open order.
- Direct CLI tests for help, exit categories, redacted failures, and success summaries.
- Packaged-JAR file-to-file execution against temporary input, JobSpec, and output paths.
- Full Reactor, formatting, diff, dependency-direction, and worktree gates.
