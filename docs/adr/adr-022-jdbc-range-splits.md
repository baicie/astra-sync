# ADR-022: Connector Split Enumeration and Numeric JDBC Ranges

## Status

Accepted

## Context

The Phase 1 runtime can schedule independent work, but a connector must be able to describe
independent full-load ranges before a Worker opens a reader. A task-specific `BatchSource` cannot
serve as that descriptor because it owns resources and hides the stable boundary needed for
assignment, validation, and later remote transport.

JDBC tables are a useful first implementation because an ordered, non-null numeric column can
provide deterministic ranges without requiring a connector-specific protocol. The implementation
must avoid silently losing rows from empty tables, null split columns, fractional values, or
overlapping boundaries.

## Decision

1. The connector API exposes immutable `SourceSplit` descriptors with a split ID, source ID, and
   left-closed/right-open `SplitPosition` boundaries. An empty position map means unbounded.
2. `SplitEnumerator` returns descriptors without opening a split reader. `SplitSource` adds
   materialization of one bounded `BatchSource` for a validated descriptor.
3. The Coordinator validates unique split IDs, then uses `BatchTaskFactory` to create one task per
   split. A factory may not replace the split descriptor while materializing its resources.
4. `JdbcRangeSplitSource` accepts `url`, `table`, `splitColumn`, and positive `splitCount`, with
   the existing JDBC credentials and read options as optional settings. It queries `MIN` and `MAX`,
   requires integral values, divides the inclusive numeric span into non-overlapping ranges, and
   gives the final range an unbounded upper boundary.
5. The JDBC split column must be a stable, non-null integer column, preferably a primary key or
   unique index. Null rows are outside the supported contract. Identifiers are limited to one or
   two unquoted SQL identifier segments.

## Consequences

Split descriptors are serializable at the contract level and can be assigned independently of
connector resources. Empty tables produce no work, and a requested split count larger than the
integer span does not create empty ranges. JDBC range enumeration is intentionally narrower than a
general query planner: it does not support composite keys, fractional keys, null-inclusive ranges,
dynamic discovery, retries, checkpoints, resume, or exactly-once delivery.
