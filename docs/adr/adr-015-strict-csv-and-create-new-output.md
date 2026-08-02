# ADR-015: Strict CSV and Create-new Output

## Status

Accepted

## Date

2026-08-03

## Context

CSV appears simple but quoting, embedded line breaks, headers, empty values, nulls, malformed rows,
and output replacement all carry data-loss risk. A line-splitting parser cannot implement RFC 4180
correctly. Silently skipping malformed input would make row counts untrustworthy, while publishing
a temporary file from `BatchSink.close` would commit partial output because the current lifecycle
does not distinguish successful completion from abort.

## Decision

The Phase 0 `csv` connector uses Apache Commons CSV and a deliberately narrow contract:

1. Input and output use UTF-8 RFC 4180 with comma, double quote, preserved spaces, and CRLF output.
2. Input requires a non-empty record of unique, non-blank, case-sensitive header names. A leading
   UTF-8 BOM is accepted and removed.
3. Every data record must exactly match the header width. Values are strings unless an exact
   configured `nullValue` token maps them to null.
4. `malformedRowPolicy` defaults to and only accepts `fail`. Invalid headers fail in `open`; lexical
   and width errors fail in `readBatch` with path and record/line evidence. No row is skipped.
5. The Source accumulates no more than the requested batch size and does not read ahead solely to
   identify an exact-boundary final batch.
6. The Sink derives header order from its first Row, requires the same column set thereafter, and
   accepts only strings or explicitly encodable nulls in this slice.
7. Output uses create-new semantics. Existing targets are never truncated, parent directories are
   not created implicitly, and overwrite/append are unsupported.
8. The Sink writes directly and flushes each accepted batch. A failed job can leave a partial file;
   it is reported as failed and never described as atomic or committed.

Header propagation for an empty data set, configurable dialects, typed JDBC encodings, skip/dead
letter handling, and atomic publication require explicit later contracts.

## Consequences

### Positive

- Standard quoting and multiline behavior comes from a maintained parser.
- Malformed data cannot disappear silently.
- Empty strings and nulls are distinct unless the user explicitly chooses a null token.
- Existing files are protected from accidental truncation, including same-path jobs.
- Output behavior matches the runtime's honest at-most-once and partial-write semantics.

### Negative

- Phase 0 cannot ingest headerless or non-RFC dialect files.
- One malformed record stops the job even when other rows could be usable.
- Reruns require a new or manually removed output path.
- A failed job may leave a partial file that operators must inspect and remove.
- A header-only input produces an empty output because the current batch SPI carries rows, not a
  separate schema.

## Alternatives Considered

### Split Physical Lines Manually

Rejected because quoted fields can contain commas, quotes, CRLF, and LF.

### Skip Malformed Rows

Rejected for Phase 0 because lexical errors do not always have a reliable resynchronization point,
and the current result contract has no rejected-row metric or dead-letter evidence.

### Write a Temporary File and Rename in Close

Rejected because `close` runs after both success and failure. Without commit/abort, it cannot know
whether publishing the temporary file is correct.

### Truncate Existing Output

Rejected because it can destroy input or prior results before a job has succeeded.

## Related Decisions

- ADR-004: Sink Writer/Committer Model
- ADR-008: Row and Arrow Formats
- ADR-009: Exactly-once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-013: Descriptor-first Connector Planning
