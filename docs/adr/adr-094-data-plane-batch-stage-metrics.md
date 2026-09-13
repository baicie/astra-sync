# ADR-094: Data-Plane Batch Stage Metrics

## Status

Accepted

## Context

ADR-051 reserves `coordinator_batch_duration_seconds` with a bounded `stage`
label and initially records only the Coordinator round-trip value as
`stage="read"`. The delayed alternative in ADR-051 notes that producing and
consuming threads must be instrumented separately to expose finer stage
timings.

The distributed `BatchTask` contract has source and sink boundaries but no
separate transform stage. Any transformation owned by the source or sink is
already included in that connector operation.

## Decision

Instrument the existing Worker execution boundaries:

- `InProcessBatchWorker.produce` records `stage="read"` for the duration of
  `BatchSource.readBatch`.
- `InProcessBatchWorker.consume` records `stage="write"` for the duration of
  `BatchSink.writeBatch`.
- The checkpoint execution path records the same read/write boundaries with
  its trusted `job_id`.
- Non-checkpoint execution uses the bounded `_unknown` job identifier.
- `DataPlaneMetrics` allows only `read` and `write`; an unknown stage collapses
  to `unknown`.

`transform` is intentionally not emitted. There is no standalone transform
boundary in the distributed task contract, so claiming a transform sample
would misrepresent connector-internal work as a separate stage.

## Consequences

- Operators can distinguish source wait/read time from sink write time on
  Worker and Coordinator endpoints.
- The existing `stage="read"` series remains present; the schema change is
  additive through the already-reserved `stage` label.
- Timer cardinality remains bounded to `read`, `write`, and `unknown`.
- Transform timing remains included in the owning connector's read or write
  duration until a dedicated transform execution boundary is introduced.

## Rollback

Remove the Worker stage recordings and restore the single-stage allowlist.
The metric descriptor and existing read series remain compatible.
