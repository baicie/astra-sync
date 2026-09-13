# ADR-101: Data-Plane Worker Identity in Logs

## Status

Accepted — followed by ADR-102 for request identity.

## Context

ADR-100 added scoped tenant, job, epoch, stage, and outcome fields to Java
data-plane logs. When one Job runs across multiple Workers, those fields do
not identify which Worker produced an event.

The metrics catalog already uses a bounded `worker_id` label. Logs need the
same correlation key without changing the Worker protocol or adding another
identity source.

## Decision

Add an optional `worker_id` field to `DataPlaneLogContext`:

1. Keep existing context constructors unchanged.
2. Add a worker-scoped overload that records a non-blank Worker identifier.
3. Populate it in `InProcessBatchWorker` task and stage contexts.
4. Populate it in `RemoteBatchWorker` dispatch and checkpoint contexts.
5. Do not add `worker_id` to process-wide Coordinator execution logs because
   no single Worker owns those events.
6. Use the same bounded Worker identifier already validated by the deployment
   and Worker configuration.

The field is additive to the stable log contract. It never contains connector
options, credentials, or other user input.

## Consequences

- Multi-Worker jobs can correlate logs and metrics using `worker_id`.
- Nested queue/thread contexts retain and restore the field with all other MDC
  values.
- Coordinator-side dispatch and Worker-side execution use the same Worker
  identifier.
- No protobuf, metric family, deployment, or dependency change is required for
  `worker_id`. ADR-102 later appends a separate `request_id` protocol field.

## Rollback

Remove the worker-scoped context overload and stop populating `worker_id`.
Existing tenant, job, epoch, stage, and outcome fields remain unchanged.
