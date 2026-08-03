# Slice 03: File Vertical Slice

## Status

Verified and complete on implementation commit `f7c6b3c`.

## Objective

Prove the first complete AstraSync user path: a strict JobSpec is loaded by a runnable CLI,
compiled before connector materialization, and executed through the bounded single-node runtime
from one RFC 4180 CSV file to a newly created CSV file.

## Included

- A generic local runner that compiles before creating Source or Sink instances.
- A concrete in-process `csv` factory with bounded batch Source and batch Sink roles.
- UTF-8 RFC 4180 parsing and deterministic RFC 4180 output.
- Required, unique, case-sensitive header names and exact row-width validation.
- Explicit null-token support without conflating null and empty string by default.
- A fail-fast malformed-row policy with record/line evidence.
- Create-new output semantics and honest partial-file behavior on failure.
- An independent runnable CLI module with stable exit categories and concise terminal output.
- File-to-file unit, integration, and packaged-JAR acceptance tests.

## Excluded

- JSON, Parquet, compressed, directory, object-store, stdin, or stdout connectors.
- Headerless CSV, delimiter/quote/charset customization, schema inference, or type coercion.
- Skip, dead-letter, retry, or error-sampling policies.
- Overwrite, append, temporary-file rename, manifest commit, or atomic publication claims.
- Service discovery, external plugins, remote connectors, and multiple concurrent jobs.
- Checkpoints, retries, cancellation, CDC, distributed execution, and stronger delivery guarantees.

## Acceptance

1. `java -jar ... run <job-spec>` copies quoted commas, quotes, embedded line breaks, empty strings,
   explicit nulls, and Unicode through the public SPI with deterministic output.
2. A source larger than `maxBatchRecords` is processed in bounded batches through the same local
   runner used by the CLI.
3. Missing, unreadable, or non-regular input and invalid connector options fail before the
   corresponding connector owns an external resource.
4. Missing, blank, or duplicate headers and lexical or width-invalid records fail fast with file
   and record/line evidence; no row is silently skipped.
5. An existing output path is never truncated or modified, and using the input path as output is
   therefore rejected by create-new open semantics.
6. Sink rows must retain the first row's exact column set; null requires an explicit null token,
   and unsupported value types fail at `SINK_WRITE`.
7. A runtime failure closes every opened CSV resource and leaves any already written output as an
   explicitly partial at-most-once artifact.
8. CLI usage/read, validation, and runtime failures use documented non-zero exit categories without
   printing stack traces by default.
9. The Engine has no dependency on the concrete file connector; only the CLI composes both.
10. Focused verification, packaged-JAR acceptance, full Reactor tests, formatting, and diff checks
    pass.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-014](../../adr/adr-014-local-runner-and-cli-boundary.md)
- [ADR-015](../../adr/adr-015-strict-csv-and-create-new-output.md)
