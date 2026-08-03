# Slice 02 Design

## Flow

1. A connector creates a `SplitEnumerator` or `SplitSource` from its configuration.
2. `enumerate()` returns immutable descriptors and does not create a split reader.
3. `BatchCoordinator.run(enumerator, taskFactory)` rejects null descriptors and duplicate split IDs.
4. The task factory creates a fresh source and sink for the descriptor. The Coordinator verifies that
   the task retains the exact descriptor before scheduling it.
5. Existing Worker scheduling, bounded exchange, failure propagation, and metric aggregation execute
   the materialized tasks.

## JDBC Range Algorithm

`JdbcRangeSplitSource` reads `MIN(splitColumn)` and `MAX(splitColumn)` from the configured table.
An empty or all-null result returns no splits. Integral bounds are divided into `N` ranges, where
`N` is the smaller of `splitCount` and the number of integer values in the inclusive span.

Each non-final split is `[start, end)`; the final split is `[start, +infinity)`. The split ID is
stable for the enumeration order and includes the source ID plus its zero-based index. A materialized
reader emits `SELECT *` with the corresponding predicates and an `ORDER BY splitColumn`.

## Contract Limits

The split column is required to be a non-null integer key suitable for stable range partitioning.
`MIN`/`MAX` cannot represent null rows, composite keys, or fractional keys in this contract. The
runtime remains in-process and at-most-once; retry, checkpoint, resume, and remote transport are
outside this slice.
