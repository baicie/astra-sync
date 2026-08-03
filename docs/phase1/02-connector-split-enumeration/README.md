# Phase 1 Slice 02: Connector Split Enumeration

This slice separates connector split discovery from split reader materialization and connects the
result to the existing in-process Coordinator/Worker runtime.

## Delivered

- Immutable `SourceSplit` and `SplitPosition` contracts in `connector-api`.
- `SplitEnumerator` and `SplitSource` contracts for descriptor discovery and reader materialization.
- `BatchTask` ownership of a `SourceSplit` and a `BatchTaskFactory` Coordinator boundary.
- JDBC numeric range enumeration using `MIN`/`MAX` and left-closed/right-open ranges.
- Validation for duplicate IDs, source ownership, malformed boundaries, empty tables, and non-integer
  range values.

## JDBC Configuration

Required options are `url`, `table`, and `splitColumn`. `splitCount` is optional and defaults to `1`.
`user`, `password`, `fetchSize`, and `queryTimeoutSeconds` are optional and have the same meaning as
the generic JDBC connector.

The table and split column are unquoted SQL identifiers. The split column must be a stable,
non-null integer column. A primary key or unique, indexed column is recommended so each row belongs
to one deterministic range.

## Boundary Example

For values from `1` through `10` and `splitCount=3`, the enumerator produces:

```text
[1, 4)
[4, 7)
[7, +infinity)
```

The final range is unbounded above so it includes the observed maximum and values inserted after
enumeration are still subject to the source's normal snapshot/transaction behavior. This slice
does not claim a resumable or exactly-once snapshot.
