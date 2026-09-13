# ADR-102: Data-Plane Request ID Propagation

## Status

Accepted

## Context

ADR-100 added scoped `tenant_id`, `job_id`, `epoch`, `stage`, and `outcome`
fields to Java data-plane logs, but explicitly deferred `request_id` because
the Worker task contract did not carry it. Coordinator and Worker records from
one execution therefore could not be joined by request identity.

The observability handbook already reserves `request_id` as a stable field and
future OpenMetrics exemplars need a request identity that can be transported
with the execution. The Worker protocol must provide that carrier without
breaking old Coordinators or Workers.

## Decision

1. Append `request_id = 10` to `ExecuteTaskRequest` and
   `request_id = 15` to `ExecuteCheckpointTaskRequest`.
2. Keep both fields optional and retain Worker protocol versions `1` and `2`.
   Older Coordinators that omit the fields remain compatible.
3. Generate one canonical UUID per `CoordinatorApplication.run` invocation and
   reuse it for execution start, completion, remote dispatch, and checkpoint
   requests from that run.
4. Extend `BatchTask` with a bounded request identity. Preserve the existing
   constructors and two-argument `withIdentity` overload; missing values
   normalize to `_unknown`.
5. Carry the request identity through `RemoteTaskFactory` and
   `WorkerProtocolMapper` for both normal and checkpoint requests.
6. Rebind the request identity onto the materialized Worker task before
   execution.
7. Add `request_id` to Coordinator, remote dispatch, in-process Worker task,
   stage, outcome, and checkpoint log contexts.
8. Omit blank or `_unknown` request identities from logs so old requests do not
   emit a misleading correlation value.

The request identity is observability metadata. It does not grant
authorization and does not replace Worker, task, epoch, or split validation.
This ADR does not emit OpenMetrics exemplars or add a metric label.

## Consequences

- Coordinator and Worker logs for one execution can be joined on
  `request_id`.
- The Worker protocol now carries the request identity required for a future
  exemplar implementation without another contract change.
- Existing Coordinator and Worker binaries remain wire-compatible because the
  appended protobuf fields default to an empty string and are omitted from
  logs.
- Request identity does not increase metric label cardinality and does not
  change deployment configuration or storage backends.
- Tests must cover normal dispatch, checkpoint dispatch, old requests, and
  log-context cleanup.

## Rollback

Remove the appended protobuf fields and the `request_id` log-context usage.
Restore `BatchTask` and `RemoteTaskFactory` to their identity-only forms.
Existing tenant, job, Worker, stage, and outcome behavior remains otherwise
unchanged.
